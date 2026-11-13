"""Deployed-server e2e for session-restore/AC3 (거부 응답의 구분 전달).

검증 시나리오: test-session-restore.md#시나리오 3
실행 대상: primary
병렬 레인: session

kind 의 제어면은 아카이브를 켜지 않고 뜨므로 동결된 세션을 만들 수 없다. 여기서 관측하는 것은
없는 id 의 not found 와, 이미 살아 있는 세션의 복원이 그 세션을 그대로 돌려준다는 것이다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _session_platform import live_shell_session, sessions_from

ABSENT_SESSION_ID = "e2e-restore-sc3-absent"


async def test_session_restore_sc3_absent_session_is_not_found(session) -> None:
    result = await session.call_tool("session_restore", {"id": ABSENT_SESSION_ID})
    assert result.isError is True, result
    text = result.content[0].text
    assert "not found" in text, text
    assert ABSENT_SESSION_ID in text, text
    assert "retry" not in text, f"a missing session must not invite a retry: {text}"
    assert "unavailable" not in text, text
    print("not found ok:", text)


async def test_session_restore_sc3_live_session_comes_back_unchanged(
    session, live: dict
) -> None:
    result = await session.call_tool("session_restore", {"id": live["id"]})
    assert result.isError is False, result
    restored = result.structuredContent["session"]
    assert restored["id"] == live["id"], restored
    assert restored["state"] == "active", restored
    assert restored.get("pod") == live.get("pod"), (
        f"restoring a live session must not move it to another pod: {restored} vs {live}"
    )

    listed = {s["id"]: s for s in sessions_from(await session.call_tool("session_list", {}))}
    assert listed.get(live["id"], {}).get("state") == "active", listed.get(live["id"])
    print("live session unchanged ok:", restored)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    with live_shell_session("e2e restore sc3") as (_, live):
        async with open_session(url) as session:
            print("--- session-restore/시나리오 3 ---")
            await test_session_restore_sc3_absent_session_is_not_found(session)
            await test_session_restore_sc3_live_session_comes_back_unchanged(session, live)
            print("ok: test-session-restore.md#시나리오 3")


if __name__ == "__main__":
    asyncio.run(run())
