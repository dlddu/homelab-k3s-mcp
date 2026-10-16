"""Deployed-server e2e for session-delete/AC2 (거부 응답의 구분 전달).

검증 시나리오: test-session-delete.md#시나리오 2
실행 대상: primary
병렬 레인: session
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz

ABSENT_SESSION_ID = "e2e-delete-sc2-absent"


async def test_session_delete_sc2_absent_session_is_not_found(session) -> None:
    result = await session.call_tool("session_delete", {"id": ABSENT_SESSION_ID})
    assert result.isError is True, result
    text = result.content[0].text
    assert "not found" in text, text
    assert ABSENT_SESSION_ID in text, text
    assert "retry" not in text, f"a missing session must not invite a retry: {text}"
    assert "unavailable" not in text, text

    survivor = await session.call_tool("ping", {})
    assert survivor.isError is False and survivor.content[0].text == "pong", survivor
    print("not found ok:", text)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- session-delete/시나리오 2 ---")
        await test_session_delete_sc2_absent_session_is_not_found(session)
        print("ok: test-session-delete.md#시나리오 2")


if __name__ == "__main__":
    asyncio.run(run())
