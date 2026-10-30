"""Deployed-server e2e for opensearch-search/AC3 (docstore 접근 — 클러스터 내부 HTTP, 자격증명 없음).

검증 시나리오: test-opensearch-search.md#시나리오 3
실행 대상: primary
추가 인자: trace
병렬 레인: aws
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, fetch_trace, open_session, trace_url, wait_for_healthz
from _opensearch import (
    assert_unsigned_docstore_access, index_for, put_doc, search, token_for,
)


async def test_opensearch_search_ac3_unsigned_docstore_access(session, trace) -> None:
    """AC: opensearch-search/AC3 — the search reaches docstore as plain HTTP with no credential.

    Reads the http-trace recording rather than the tool response: docstore would
    answer a signed search exactly the same way, so only the recording can show
    that no ``Authorization`` header or session token travelled with it.
    """
    index = index_for("searchac3")
    await put_doc(session, index, {"title": f"access probe {token_for('searchac3')}"})
    await search(session, token_for("searchac3"), index=index)

    record = assert_unsigned_docstore_access(
        fetch_trace(trace), method="POST", path=f"/{index}/_search"
    )
    print("opensearch_search unsigned docstore access ok ->", f"POST {record['path']}")


async def run() -> None:
    url = base_url()
    trace = trace_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- opensearch-search/AC3 ---")
        await test_opensearch_search_ac3_unsigned_docstore_access(session, trace)
        print("ok: opensearch-search/AC3")


if __name__ == "__main__":
    asyncio.run(run())
