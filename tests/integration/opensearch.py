"""End-to-end checks for the opensearch_* tools against the OpenSearch fixture.

검증 AC: opensearch-document-delete/AC1, opensearch-document-delete/AC2, opensearch-document-delete/AC3, opensearch-document-delete/AC4, opensearch-document-put/AC1, opensearch-document-put/AC2, opensearch-document-put/AC3, opensearch-document-put/AC4, opensearch-search/AC1, opensearch-search/AC2, opensearch-search/AC3
실행 대상: primary
추가 인자: trace

**분할 대기(11 AC 겸용)** — 모델 `tbm_homelab-k3s-mcp-ac-e2e`의 파일 단위 규칙 2는
파일 하나가 AC 하나만 주검증할 것을 요구하므로, 이 파일은 AC별 전용 파일로 쪼개질 대상이다.
위 선언은 현재 겸용 상태를 있는 그대로 신고하는 것이고,
`tests/integration/check_ac_mapping.py`가 이를 규칙 2 위반으로 계수해 `docs/doc-tracker.md`와 대조한다.

The server assumes OPENSEARCH_ROLE_ARN against MinIO's STS endpoint and sends
SigV4-signed (service "aoss") requests to the single-node OpenSearch fixture
(tests/k8s/kind/opensearch.yaml, security plugin disabled).

Per-AC case names + docstrings declare the AC they verify (registry rule 3);
``docs/doc-tracker.md`` is the AC<->case mapping SSOT. ``run()`` is only a
dispatcher: the cases are order independent because each one owns its documents
end to end — it writes into its own ``ci-<case>-<RUN_ID>`` index (a put
auto-creates it) and matches on its own unique query token, so no case can
observe another's writes even on the unscoped searches.

The AssumeRole -> SigV4 access path (opensearch-search/AC3,
opensearch-document-put/AC4, opensearch-document-delete/AC4) is not observable
from the tool responses. This fixture runs with the security plugin disabled,
so it accepts signed and unsigned requests alike, and MinIO accepts the base
credentials in the CI secret as readily as assumed-role ones — an
outcome-based assertion would pass against a server that never assumed the
role. ``tests/k8s/kind/http-trace.yaml`` supplies the missing observation
point: a recording proxy in front of both the STS endpoint and OpenSearch, so
the ``*_assume_role_sigv4`` cases below can assert which credential signed each
data-plane request.

Documents become searchable only after a refresh (~1s on the fixture), so
search assertions poll until the expected state appears.
"""

from __future__ import annotations

import asyncio
import json
import time
import uuid

from _helpers import (
    assert_assumed_role_access,
    assert_destructive_annotation,
    base_url,
    fetch_trace,
    open_session,
    trace_url,
    wait_for_healthz,
)

# Must match the opensearch secret in .github/workflows/ci.yml.
ROLE_ARN = "arn:aws:iam::000000000000:role/ci-opensearch"
REGION = "us-east-1"
# OpenSearch Serverless signs with the "aoss" service name
# (internal/opensearch/opensearch.go).
SIGNING_SERVICE = "aoss"

# Fresh per run so re-runs against a warm fixture never see stale documents.
RUN_ID = uuid.uuid4().hex[:8]

SEARCH_DEADLINE_SECONDS = 60.0


def index_for(case: str) -> str:
    """Index owned by one case: never written to by any other case or run."""
    return f"ci-{case}-{RUN_ID}"


def token_for(case: str) -> str:
    """Query token owned by one case.

    Alphanumeric on purpose: the standard analyzer splits on everything else, so
    a token like ``searchac1a1b2c3d4`` stays a single term that no other case's
    documents contain. That is what makes the unscoped (collection-wide)
    searches deterministic while every case seeds documents concurrently.
    """
    return f"{case}{RUN_ID}"


def structured(result):
    assert result.isError is False, result
    assert result.structuredContent is not None, result
    return result.structuredContent


async def put_doc(session, index, document, doc_id=None):
    args = {"index": index, "document": document}
    if doc_id is not None:
        args["id"] = doc_id
    return structured(await session.call_tool("opensearch_document_put", args))


async def delete_doc(session, index, doc_id):
    return structured(
        await session.call_tool(
            "opensearch_document_delete", {"index": index, "id": doc_id}
        )
    )


async def search(session, query, index=None, size=None):
    args = {"query": query}
    if index is not None:
        args["index"] = index
    if size is not None:
        args["size"] = size
    return structured(await session.call_tool("opensearch_search", args))


async def search_until(session, query, predicate, description, index=None, size=None):
    """Poll search until predicate(hits) holds (documents surface on refresh)."""
    deadline = time.monotonic() + SEARCH_DEADLINE_SECONDS
    last = None
    while time.monotonic() < deadline:
        last = await search(session, query, index=index, size=size)
        if predicate(last["hits"]):
            return last
        await asyncio.sleep(1)
    raise AssertionError(f"search never converged: {description} (last: {last})")


def hit_ids(hits):
    return {hit["id"] for hit in hits}


async def test_opensearch_document_put_ac1_upsert_semantics(session) -> None:
    """AC: opensearch-document-put/AC1 — index a document, and re-putting an id upserts it.

    Walks the AC's verification method clause by clause: the first write of an
    explicit id reports ``created``; re-putting that id reports ``updated`` and
    the *new* body is what a later search returns (so the second write replaced
    the document instead of adding a second one); two id-less writes of the same
    body come back ``created`` with two different auto-generated ids and leave
    three documents behind, not two.
    """
    index = index_for("put-ac1")
    token = token_for("putac1")

    created = await put_doc(
        session,
        index,
        {"title": f"etcd backup runbook {token}", "body": "how to back up etcd"},
        doc_id="doc-1",
    )
    assert created == {"index": index, "id": "doc-1", "result": "created"}, created

    updated = await put_doc(
        session,
        index,
        {"title": f"etcd backup runbook {token}", "body": "how to back up etcd, v2"},
        doc_id="doc-1",
    )
    assert updated == {"index": index, "id": "doc-1", "result": "updated"}, updated

    def upserted(hits):
        return hit_ids(hits) == {"doc-1"} and hits[0]["source"]["body"] == (
            "how to back up etcd, v2"
        )

    await search_until(
        session,
        token,
        upserted,
        "doc-1 is a single document carrying the second body",
        index=index,
    )

    auto_one = await put_doc(session, index, {"title": f"etcd backup checklist {token}"})
    auto_two = await put_doc(session, index, {"title": f"etcd backup checklist {token}"})
    assert auto_one["result"] == "created", auto_one
    assert auto_two["result"] == "created", auto_two
    assert auto_one["id"] and auto_two["id"], (auto_one, auto_two)
    assert auto_one["id"] != auto_two["id"], (auto_one, auto_two)
    assert "doc-1" not in {auto_one["id"], auto_two["id"]}, (auto_one, auto_two)

    final = await search_until(
        session,
        token,
        lambda hits: hit_ids(hits) == {"doc-1", auto_one["id"], auto_two["id"]},
        "the upsert plus both auto-id writes leave exactly three documents",
        index=index,
    )
    assert final["total"] == 3, final
    print("opensearch_document_put upsert semantics ok ->", index)


async def test_opensearch_document_put_ac2_index_auto_creation(session) -> None:
    """AC: opensearch-document-put/AC2 — writing to a missing index creates it.

    The pre-state is asserted, not assumed: searching the index *before* the
    write comes back as a tool error carrying OpenSearch's 404
    ``index_not_found_exception``. Without that step a passing search afterwards
    would be equally consistent with the index having existed all along, and the
    case would demonstrate nothing about auto-creation.
    """
    index = index_for("put-ac2")
    token = token_for("putac2")

    missing = await session.call_tool(
        "opensearch_search", {"query": token, "index": index}
    )
    assert missing.isError is True, missing
    refusal = missing.content[0].text
    assert "status 404" in refusal, refusal
    assert "index_not_found_exception" in refusal, refusal

    created = await put_doc(
        session, index, {"title": f"auto-created index {token}"}, doc_id="doc-1"
    )
    assert created == {"index": index, "id": "doc-1", "result": "created"}, created

    found = await search_until(
        session,
        token,
        lambda hits: hit_ids(hits) == {"doc-1"},
        f"{index} exists and holds the document after the write",
        index=index,
    )
    assert found["hits"][0]["index"] == index, found
    print("opensearch_document_put index auto-creation ok ->", index)


async def test_opensearch_search_ac1_query_matching(session) -> None:
    """AC: opensearch-search/AC1 — matching documents only, with index/id/score/source.

    Seeds two indexes so both halves of the AC are observable: the unscoped call
    spans the collection (both indexes answer) while ``index`` narrows it to one,
    and a non-matching document seeded alongside the matches is what proves the
    query — not merely the index scope — is doing the filtering.
    """
    index_a = index_for("search-ac1-a")
    index_b = index_for("search-ac1-b")
    token = token_for("searchac1")
    other_token = token_for("searchac1other")

    await put_doc(
        session,
        index_a,
        {"title": f"apiserver certificate rotation {token}", "body": "rotate certs"},
        doc_id="match-a",
    )
    await put_doc(
        session,
        index_a,
        {"title": f"unrelated grafana note {other_token}"},
        doc_id="other-a",
    )
    await put_doc(
        session,
        index_b,
        {"title": f"etcd certificate rotation {token}"},
        doc_id="match-b",
    )

    across = await search_until(
        session,
        token,
        lambda hits: hit_ids(hits) == {"match-a", "match-b"},
        "both indexes answer the collection-wide query",
    )
    assert across["total"] == 2, across
    assert across["index"] is None, across
    for hit in across["hits"]:
        assert hit["index"] in {index_a, index_b}, hit
        assert hit["id"], hit
        assert hit["score"] is not None, hit
        assert token in json.dumps(hit["source"]), hit

    scoped = await search(session, token, index=index_a)
    assert hit_ids(scoped["hits"]) == {"match-a"}, scoped
    assert scoped["index"] == index_a, scoped
    assert scoped["hits"][0]["index"] == index_a, scoped

    # The non-matching document is in index_a and searchable, so its absence
    # from the queries above is the query filtering, not a missing document.
    other = await search_until(
        session,
        other_token,
        lambda hits: hit_ids(hits) == {"other-a"},
        "the non-matching document is searchable under its own token",
        index=index_a,
    )
    assert other["hits"][0]["index"] == index_a, other
    print("opensearch_search query matching ok ->", index_a, "/", index_b)


async def test_opensearch_search_ac2_size_default_and_cap(session) -> None:
    """AC: opensearch-search/AC2 — size defaults to 10, allows 50, rejects 51.

    Seeds 12 matching documents because the default is otherwise invisible: with
    ten or fewer documents a size-less search returns everything and "defaults to
    10" cannot be told apart from "no limit at all". ``total`` staying 12 while
    only 10 hits come back is what shows the default caps the returned page
    rather than the match set.
    """
    index = index_for("search-ac2")
    token = token_for("searchac2")

    seeded = set()
    for n in range(12):
        put = await put_doc(
            session, index, {"title": f"scaling note {n} {token}"}, doc_id=f"doc-{n}"
        )
        assert put["result"] == "created", put
        seeded.add(put["id"])
    assert len(seeded) == 12, seeded

    full = await search_until(
        session,
        token,
        lambda hits: hit_ids(hits) == seeded,
        "all 12 seeded documents are searchable",
        index=index,
        size=50,
    )
    assert full["total"] == 12, full

    defaulted = await search(session, token, index=index)
    assert len(defaulted["hits"]) == 10, defaulted
    assert defaulted["total"] == 12, defaulted
    assert hit_ids(defaulted["hits"]) <= seeded, defaulted

    oversize = await session.call_tool(
        "opensearch_search", {"query": token, "index": index, "size": 51}
    )
    assert oversize.isError is True, oversize
    assert "size must be <= 50" in oversize.content[0].text, oversize
    print("opensearch_search size default/cap ok ->", index)


async def test_opensearch_document_delete_ac1_single_document(session) -> None:
    """AC: opensearch-document-delete/AC1 — only the named document is removed.

    Two documents share the index and the query token, so the surviving one is
    the control: the post-delete search has to keep returning it while the
    deleted id disappears.
    """
    index = index_for("delete-ac1")
    token = token_for("deleteac1")

    await put_doc(session, index, {"title": f"stale runbook {token}"}, doc_id="doomed")
    await put_doc(session, index, {"title": f"current runbook {token}"}, doc_id="kept")
    await search_until(
        session,
        token,
        lambda hits: hit_ids(hits) == {"doomed", "kept"},
        "both documents are searchable before the delete",
        index=index,
    )

    deleted = await delete_doc(session, index, "doomed")
    assert deleted == {"index": index, "id": "doomed", "result": "deleted"}, deleted

    survivors = await search_until(
        session,
        token,
        lambda hits: hit_ids(hits) == {"kept"},
        "only the deleted document leaves the index",
        index=index,
    )
    assert survivors["total"] == 1, survivors
    print("opensearch_document_delete single document ok ->", index)


async def test_opensearch_document_delete_ac2_missing_document_not_found(
    session,
) -> None:
    """AC: opensearch-document-delete/AC2 — a missing document answers not_found.

    Covers both flavours of "missing": an id in an index that was never created,
    and an id that existed until this case deleted it (the repeat the tool
    advertises as idempotent). The ``ping`` at the end is what turns the AC's
    second half — no effect on the server or the other tools — into an assertion
    instead of an assumption.
    """
    index = index_for("delete-ac2")
    token = token_for("deleteac2")

    never_written = await delete_doc(session, index, "never-written")
    assert never_written == {
        "index": index,
        "id": "never-written",
        "result": "not_found",
    }, never_written

    await put_doc(session, index, {"title": f"short-lived note {token}"}, doc_id="doc-1")
    first = await delete_doc(session, index, "doc-1")
    assert first == {"index": index, "id": "doc-1", "result": "deleted"}, first
    repeated = await delete_doc(session, index, "doc-1")
    assert repeated == {
        "index": index,
        "id": "doc-1",
        "result": "not_found",
    }, repeated

    pong = await session.call_tool("ping", {})
    assert pong.isError is False, pong
    assert pong.content[0].text == "pong", pong
    print("opensearch_document_delete not_found handling ok ->", index)


async def test_opensearch_search_ac3_assume_role_sigv4(session, trace) -> None:
    """AC: opensearch-search/AC3 — the search request is SigV4-signed with assumed-role credentials.

    Reads the http-trace recording rather than the tool response. The fixture
    runs with the security plugin disabled, so it would answer an unsigned
    search exactly the same way — the response cannot distinguish the access
    path, only the recording can.

    Asserts the whole chain: an AssumeRole for the configured role ARN was
    issued (signed with the base credential), and the ``_search`` that followed
    carried an ``AWS4-HMAC-SHA256`` header scoped to ``aoss`` in this region,
    signed with a key STS handed back — never the static base key — and with a
    session token attached.
    """
    index = index_for("searchac3")
    await put_doc(session, index, {"title": f"sigv4 probe {token_for('searchac3')}"})
    await search(session, token_for("searchac3"), index=index)

    record = assert_assumed_role_access(
        fetch_trace(trace),
        role_arn=ROLE_ARN,
        upstream="opensearch",
        method="POST",
        path=f"/{index}/_search",
        service=SIGNING_SERVICE,
        region=REGION,
    )
    # The payload hash has to be signed too, or the body could be swapped in
    # flight without invalidating the signature.
    assert "x-amz-content-sha256" in record["sigv4"]["signedHeaders"], record
    print(
        "opensearch_search assume-role SigV4 ok ->",
        f"POST {record['path']} signed by {record['sigv4']['accessKeyId']}"
        f" ({record['sigv4']['service']}/{record['sigv4']['region']})",
    )


async def test_opensearch_document_put_ac4_assume_role_sigv4(session, trace) -> None:
    """AC: opensearch-document-put/AC4 — the index write is SigV4-signed with assumed-role credentials.

    Same reasoning as opensearch-search/AC3: the security-disabled fixture
    accepts an unsigned write just as happily, so the assertion has to come
    from the http-trace recording of the write itself (``PUT /<index>/_doc/<id>``)
    rather than from the ``created`` result.
    """
    index = index_for("putac4")
    doc_id = "sigv4-put"
    await put_doc(
        session, index, {"title": f"sigv4 probe {token_for('putac4')}"}, doc_id=doc_id
    )

    record = assert_assumed_role_access(
        fetch_trace(trace),
        role_arn=ROLE_ARN,
        upstream="opensearch",
        method="PUT",
        path=f"/{index}/_doc/{doc_id}",
        service=SIGNING_SERVICE,
        region=REGION,
    )
    assert "x-amz-content-sha256" in record["sigv4"]["signedHeaders"], record
    print(
        "opensearch_document_put assume-role SigV4 ok ->",
        f"PUT {record['path']} signed by {record['sigv4']['accessKeyId']}"
        f" ({record['sigv4']['service']}/{record['sigv4']['region']})",
    )


async def test_opensearch_document_delete_ac4_assume_role_sigv4(session, trace) -> None:
    """AC: opensearch-document-delete/AC4 — the delete is SigV4-signed with assumed-role credentials.

    Same reasoning as opensearch-search/AC3. The document is seeded first so
    the recorded ``DELETE /<index>/_doc/<id>`` is a real deletion rather than a
    not_found, keeping the observed request the one the AC is about.
    """
    index = index_for("deleteac4")
    doc_id = "sigv4-delete"
    await put_doc(
        session,
        index,
        {"title": f"sigv4 probe {token_for('deleteac4')}"},
        doc_id=doc_id,
    )
    deleted = await delete_doc(session, index, doc_id)
    assert deleted["result"] == "deleted", deleted

    record = assert_assumed_role_access(
        fetch_trace(trace),
        role_arn=ROLE_ARN,
        upstream="opensearch",
        method="DELETE",
        path=f"/{index}/_doc/{doc_id}",
        service=SIGNING_SERVICE,
        region=REGION,
    )
    print(
        "opensearch_document_delete assume-role SigV4 ok ->",
        f"DELETE {record['path']} signed by {record['sigv4']['accessKeyId']}"
        f" ({record['sigv4']['service']}/{record['sigv4']['region']})",
    )


async def test_opensearch_document_put_ac3_destructive_hint(session) -> None:
    """AC: opensearch-document-put/AC3 — opensearch_document_put advertises destructiveHint=true.

    Verifies the destructive-operation marking via tools/list metadata only; no
    document is written.
    """
    await assert_destructive_annotation(session, "opensearch_document_put")


async def test_opensearch_document_delete_ac3_destructive_hint(session) -> None:
    """AC: opensearch-document-delete/AC3 — opensearch_document_delete advertises destructiveHint=true.

    Verifies the destructive-operation marking via tools/list metadata only; no
    document is deleted.
    """
    await assert_destructive_annotation(session, "opensearch_document_delete")


async def run() -> None:
    url = base_url()
    trace = trace_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        # Order is free: every case owns its index and query token, so none of
        # them can see another's documents.
        print("--- opensearch_document_put upsert (AC: opensearch-document-put/AC1) ---")
        await test_opensearch_document_put_ac1_upsert_semantics(session)

        print(
            "--- opensearch_document_put index auto-creation "
            "(AC: opensearch-document-put/AC2) ---"
        )
        await test_opensearch_document_put_ac2_index_auto_creation(session)

        print("--- opensearch_search query matching (AC: opensearch-search/AC1) ---")
        await test_opensearch_search_ac1_query_matching(session)

        print("--- opensearch_search size default/cap (AC: opensearch-search/AC2) ---")
        await test_opensearch_search_ac2_size_default_and_cap(session)

        print(
            "--- opensearch_document_delete single document "
            "(AC: opensearch-document-delete/AC1) ---"
        )
        await test_opensearch_document_delete_ac1_single_document(session)

        print(
            "--- opensearch_document_delete not_found "
            "(AC: opensearch-document-delete/AC2) ---"
        )
        await test_opensearch_document_delete_ac2_missing_document_not_found(session)

        print(
            "--- opensearch_search assume-role SigV4 "
            "(AC: opensearch-search/AC3) ---"
        )
        await test_opensearch_search_ac3_assume_role_sigv4(session, trace)

        print(
            "--- opensearch_document_put assume-role SigV4 "
            "(AC: opensearch-document-put/AC4) ---"
        )
        await test_opensearch_document_put_ac4_assume_role_sigv4(session, trace)

        print(
            "--- opensearch_document_delete assume-role SigV4 "
            "(AC: opensearch-document-delete/AC4) ---"
        )
        await test_opensearch_document_delete_ac4_assume_role_sigv4(session, trace)

        print(
            "--- opensearch_document_put destructiveHint "
            "(AC: opensearch-document-put/AC3) ---"
        )
        await test_opensearch_document_put_ac3_destructive_hint(session)
        print("opensearch_document_put destructiveHint ok")

        print(
            "--- opensearch_document_delete destructiveHint "
            "(AC: opensearch-document-delete/AC3) ---"
        )
        await test_opensearch_document_delete_ac3_destructive_hint(session)
        print("opensearch_document_delete destructiveHint ok")

        print("opensearch tools ok -> run", RUN_ID)


if __name__ == "__main__":
    asyncio.run(run())
