"""Deployed-server e2e for opensearch-document-delete/AC4 (docstore 접근 — 클러스터 내부 HTTP, 자격증명 없음).

검증 시나리오: test-opensearch-document-delete.md#시나리오 4
실행 대상: primary
추가 인자: trace
병렬 레인: aws

도구 응답이 아니라 http-trace 프록시의 기록을 읽는다(opensearch-document-put/AC4와
같은 이유 — 응답은 접근 경로를 구분하지 못한다).
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, fetch_trace, open_session, trace_url, wait_for_healthz
from _opensearch import (
    assert_unsigned_docstore_access, delete_doc, index_for, put_doc, token_for,
)


async def test_opensearch_document_delete_ac4_unsigned_docstore_access(session, trace) -> None:
    """AC: opensearch-document-delete/AC4 — the delete reaches docstore as plain HTTP with no credential.

    The document is seeded first so the recorded ``DELETE /<index>/_doc/<id>``
    is a real deletion (2xx) rather than a not_found, keeping the observed
    request the one the AC is about.
    """
    index = index_for("deleteac4")
    doc_id = "access-delete"
    await put_doc(
        session,
        index,
        {"title": f"access probe {token_for('deleteac4')}"},
        doc_id=doc_id,
    )
    deleted = await delete_doc(session, index, doc_id)
    assert deleted["result"] == "deleted", deleted

    record = assert_unsigned_docstore_access(
        fetch_trace(trace), method="DELETE", path=f"/{index}/_doc/{doc_id}"
    )
    print(
        "opensearch_document_delete unsigned docstore access ok ->",
        f"DELETE {record['path']}",
    )


async def run() -> None:
    url = base_url()
    trace = trace_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- opensearch-document-delete/AC4 ---")
        await test_opensearch_document_delete_ac4_unsigned_docstore_access(session, trace)
        print("ok: opensearch-document-delete/AC4")


if __name__ == "__main__":
    asyncio.run(run())
