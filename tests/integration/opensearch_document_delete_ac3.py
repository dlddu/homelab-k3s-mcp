"""Deployed-server e2e for opensearch-document-delete/AC3 (파괴적 작업 표기).

검증 AC: opensearch-document-delete/AC3
실행 대상: primary

``tools/list`` 메타데이터만 읽는다 — 파괴 동작을 실행하지 않는다.
"""

from __future__ import annotations

import asyncio

from _helpers import (
    assert_destructive_annotation, base_url, open_session, wait_for_healthz,
)


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
        print("--- opensearch-document-delete/AC3 ---")
        await test_opensearch_document_delete_ac3_destructive_hint(session)
        print("ok: opensearch-document-delete/AC3")


if __name__ == "__main__":
    asyncio.run(run())
