"""Deployed-server e2e for opensearch-document-put/AC1 (문서 색인·업서트).

검증 AC: opensearch-document-put/AC1
실행 대상: primary

이 케이스는 자기 인덱스 ``ci-put-ac1-<RUN_ID>`` 와 자기 질의 토큰만 쓰므로 다른
파일이 무엇을 색인하든 관측하지 않는다.

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _opensearch import hit_ids, index_for, put_doc, search_until, token_for


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


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- opensearch-document-put/AC1 ---")
        await test_opensearch_document_put_ac1_upsert_semantics(session)
        print("ok: opensearch-document-put/AC1")


if __name__ == "__main__":
    asyncio.run(run())
