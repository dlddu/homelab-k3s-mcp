"""End-to-end check for the opensearch_* tools against the OpenSearch fixture.

The server assumes OPENSEARCH_ROLE_ARN against MinIO's STS endpoint and sends
SigV4-signed (service "aoss") requests to the single-node OpenSearch fixture
(tests/k8s/kind/opensearch.yaml, security plugin disabled). This exercises the
full assume-role -> sign -> data-plane path for put, search, and delete.

Scenario (mirrors docs/test-opensearch-*.md):
  put id=doc-1 (created) -> re-put same id (updated) -> put twice without id
  (distinct auto ids) -> search finds the matching docs only, index param
  scopes the search -> size=51 is rejected as a tool error -> delete doc-1
  (deleted) -> doc-1 gone from search, sibling doc remains -> delete doc-1
  again (not_found, idempotent).

Documents become searchable only after a refresh (~1s on the fixture), so
search assertions poll until the expected state appears.
"""

from __future__ import annotations

import asyncio
import json
import time
import uuid

from _helpers import (
    assert_destructive_annotation,
    base_url,
    open_session,
    wait_for_healthz,
)

# Fresh per run so re-runs against a warm fixture never see stale documents.
RUN_ID = uuid.uuid4().hex[:8]
RUNBOOKS_INDEX = f"ci-runbooks-{RUN_ID}"
NOTES_INDEX = f"ci-notes-{RUN_ID}"

SEARCH_DEADLINE_SECONDS = 60.0


def structured(result):
    assert result.isError is False, result
    assert result.structuredContent is not None, result
    return result.structuredContent


async def put_doc(session, index, document, doc_id=None):
    args = {"index": index, "document": document}
    if doc_id is not None:
        args["id"] = doc_id
    return structured(await session.call_tool("opensearch_document_put", args))


async def search(session, query, index=None, size=None):
    args = {"query": query}
    if index is not None:
        args["index"] = index
    if size is not None:
        args["size"] = size
    return structured(await session.call_tool("opensearch_search", args))


async def search_until(session, query, predicate, description, index=None):
    """Poll search until predicate(hits) holds (documents surface on refresh)."""
    deadline = time.monotonic() + SEARCH_DEADLINE_SECONDS
    last = None
    while time.monotonic() < deadline:
        last = await search(session, query, index=index)
        if predicate(last["hits"]):
            return last
        await asyncio.sleep(1)
    raise AssertionError(f"search never converged: {description} (last: {last})")


def hit_ids(hits):
    return {hit["id"] for hit in hits}


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
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- opensearch_document_put: upsert with explicit id ---")
        created = await put_doc(
            session,
            RUNBOOKS_INDEX,
            {"title": "etcd backup runbook", "body": "how to back up etcd"},
            doc_id="doc-1",
        )
        assert created["index"] == RUNBOOKS_INDEX, created
        assert created["id"] == "doc-1", created
        assert created["result"] == "created", created

        updated = await put_doc(
            session,
            RUNBOOKS_INDEX,
            {"title": "etcd backup runbook", "body": "how to back up etcd, v2"},
            doc_id="doc-1",
        )
        assert updated["result"] == "updated", updated

        print("--- opensearch_document_put: auto-generated ids ---")
        auto_one = await put_doc(
            session, RUNBOOKS_INDEX, {"title": "etcd backup checklist"}
        )
        auto_two = await put_doc(
            session, RUNBOOKS_INDEX, {"title": "etcd backup checklist"}
        )
        assert auto_one["result"] == "created", auto_one
        assert auto_two["result"] == "created", auto_two
        assert auto_one["id"] and auto_two["id"], (auto_one, auto_two)
        assert auto_one["id"] != auto_two["id"], (auto_one, auto_two)

        # Unrelated document in a second index: proves matching stays scoped to
        # the query, and that the index parameter narrows the search. The index
        # did not exist before this put (auto-creation).
        unrelated = await put_doc(
            session, NOTES_INDEX, {"title": "grafana dashboard notes"}, doc_id="note-1"
        )
        assert unrelated["result"] == "created", unrelated

        print("--- opensearch_search: query matching and index scoping ---")
        expected_ids = {"doc-1", auto_one["id"], auto_two["id"]}
        result = await search_until(
            session,
            "etcd backup",
            lambda hits: hit_ids(hits) == expected_ids,
            f"expected exactly {expected_ids}",
        )
        assert result["total"] == 3, result
        for hit in result["hits"]:
            assert hit["index"] == RUNBOOKS_INDEX, hit
            assert hit["score"] is not None, hit
            assert "etcd backup" in json.dumps(hit["source"]), hit
        doc_one = next(h for h in result["hits"] if h["id"] == "doc-1")
        assert doc_one["source"]["body"] == "how to back up etcd, v2", doc_one

        # The unrelated document is searchable in its own index but never
        # matches the etcd query.
        await search_until(
            session,
            "grafana dashboard",
            lambda hits: hit_ids(hits) == {"note-1"},
            "expected exactly {note-1}",
        )
        scoped = await search(session, "etcd backup", index=NOTES_INDEX)
        assert scoped["hits"] == [], scoped

        print("--- opensearch_search: size cap is rejected, not clamped ---")
        capped = await search(session, "etcd backup", size=50)
        assert len(capped["hits"]) == 3, capped
        oversize = await session.call_tool(
            "opensearch_search", {"query": "etcd backup", "size": 51}
        )
        assert oversize.isError is True, oversize
        assert "size must be <= 50" in oversize.content[0].text, oversize

        print("--- opensearch_document_delete: single-document delete ---")
        deleted = structured(
            await session.call_tool(
                "opensearch_document_delete",
                {"index": RUNBOOKS_INDEX, "id": "doc-1"},
            )
        )
        assert deleted["index"] == RUNBOOKS_INDEX, deleted
        assert deleted["id"] == "doc-1", deleted
        assert deleted["result"] == "deleted", deleted

        survivors = {auto_one["id"], auto_two["id"]}
        await search_until(
            session,
            "etcd backup",
            lambda hits: hit_ids(hits) == survivors,
            f"doc-1 gone, {survivors} kept",
        )

        print("--- opensearch_document_delete: missing doc -> not_found ---")
        for _ in range(2):
            not_found = structured(
                await session.call_tool(
                    "opensearch_document_delete",
                    {"index": RUNBOOKS_INDEX, "id": "doc-1"},
                )
            )
            assert not_found["result"] == "not_found", not_found

        print("--- opensearch_document_put destructiveHint (AC: opensearch-document-put/AC3) ---")
        await test_opensearch_document_put_ac3_destructive_hint(session)
        print("opensearch_document_put destructiveHint ok")

        print("--- opensearch_document_delete destructiveHint (AC: opensearch-document-delete/AC3) ---")
        await test_opensearch_document_delete_ac3_destructive_hint(session)
        print("opensearch_document_delete destructiveHint ok")

        print("opensearch tools ok ->", RUNBOOKS_INDEX, "/", NOTES_INDEX)


if __name__ == "__main__":
    asyncio.run(run())
