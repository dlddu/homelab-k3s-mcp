"""drop_token_issue: DROP_URL 미설정 시 graceful 거부 (e2e).

검증 시나리오: test-drop-token-issue.md#시나리오 5
실행 대상: auth-variant
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _auth_variant import API_KEY, DROP_REFUSAL, assert_unavailable_refusal
from _helpers import base_url, open_session, wait_for_healthz


async def test_drop_token_issue_sc5_unconfigured_refusal(session: ClientSession) -> None:
    await assert_unavailable_refusal(session, "drop_token_issue", {}, DROP_REFUSAL)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url, headers={"Authorization": f"Bearer {API_KEY}"}) as session:
        print("--- unconfigured graceful refusal (AC: drop-token-issue/AC6) ---")
        await test_drop_token_issue_sc5_unconfigured_refusal(session)
        print("refusal ok: drop-token-issue/AC6")


if __name__ == "__main__":
    asyncio.run(run())
