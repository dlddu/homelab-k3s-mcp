"""Deployed-server e2e for opensearch-document-put/AC4 (docstore 접근 — 클러스터 내부 HTTP, 자격증명 없음).

검증 시나리오: test-opensearch-document-put.md#시나리오 4
실행 대상: primary
추가 인자: trace
병렬 레인: aws

도구 응답이 아니라 ``tests/k8s/kind/http-trace.yaml`` 프록시의 기록을 읽는다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, fetch_trace, open_session, trace_url, wait_for_healthz
from _opensearch import assert_unsigned_docstore_access, index_for, put_doc, token_for


async def test_opensearch_document_put_ac4_unsigned_docstore_access(session, trace) -> None:
    """AC: opensearch-document-put/AC4 — the write reaches docstore as plain HTTP with no credential.

    Same reasoning as opensearch-search/AC3: the ``created`` result cannot tell
    the access path apart, so the assertion comes from the recording of the
    write itself (``PUT /<index>/_doc/<id>``).
    """
    index = index_for("putac4")
    doc_id = "access-put"
    await put_doc(
        session, index, {"title": f"access probe {token_for('putac4')}"}, doc_id=doc_id
    )

    record = assert_unsigned_docstore_access(
        fetch_trace(trace), method="PUT", path=f"/{index}/_doc/{doc_id}"
    )
    print("opensearch_document_put unsigned docstore access ok ->", f"PUT {record['path']}")


async def run() -> None:
    url = base_url()
    trace = trace_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- opensearch-document-put/AC4 ---")
        await test_opensearch_document_put_ac4_unsigned_docstore_access(session, trace)
        print("ok: opensearch-document-put/AC4")


if __name__ == "__main__":
    asyncio.run(run())
