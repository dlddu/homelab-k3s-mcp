"""opensearch_search: OPENSEARCH_ENDPOINT 미설정 시 graceful 거부 (e2e).

검증 AC: opensearch-search/AC4
실행 대상: auth-variant
"""

from __future__ import annotations

import asyncio

from _auth_variant import (
    API_KEY,
    OPENSEARCH_REFUSAL,
    assert_unavailable_refusal,
)
from _helpers import base_url, open_session, wait_for_healthz


async def test_opensearch_search_ac4_unconfigured_refusal(
    session: ClientSession,
) -> None:
    """AC: opensearch-search/AC4

    With OPENSEARCH_ENDPOINT unset, opensearch_search returns the unavailable
    error. The query argument is valid, so the refusal is the configuration
    check and not the required-argument check.
    """
    await assert_unavailable_refusal(
        session, "opensearch_search", {"query": "anything"}, OPENSEARCH_REFUSAL
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        print("--- unconfigured graceful refusal (AC: opensearch-search/AC4) ---")
        await test_opensearch_search_ac4_unconfigured_refusal(session)
        print("refusal ok: opensearch-search/AC4")


if __name__ == "__main__":
    asyncio.run(run())
