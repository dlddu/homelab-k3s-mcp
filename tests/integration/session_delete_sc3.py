"""Deployed-server e2e for session-delete/AC3 (파괴적 작업 표기).

검증 시나리오: test-session-delete.md#시나리오 3
실행 대상: primary
병렬 레인: session
"""

from __future__ import annotations

import asyncio

from _helpers import assert_destructive_annotation, base_url, open_session, wait_for_healthz


async def test_session_delete_sc3_destructive_hint(session) -> None:
    await assert_destructive_annotation(session, "session_delete")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- session-delete/시나리오 3 ---")
        await test_session_delete_sc3_destructive_hint(session)
        print("ok: test-session-delete.md#시나리오 3")


if __name__ == "__main__":
    asyncio.run(run())
