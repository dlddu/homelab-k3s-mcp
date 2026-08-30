"""Deployed-server e2e for opensearch-document-delete/AC1 (단일 문서 삭제).

검증 AC: opensearch-document-delete/AC1
실행 대상: primary

같은 인덱스에 남는 문서를 함께 두어 "지목한 하나만" 사라졌음을 관측한다.

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _opensearch import delete_doc, hit_ids, index_for, put_doc, search_until, token_for


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


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- opensearch-document-delete/AC1 ---")
        await test_opensearch_document_delete_ac1_single_document(session)
        print("ok: opensearch-document-delete/AC1")


if __name__ == "__main__":
    asyncio.run(run())
