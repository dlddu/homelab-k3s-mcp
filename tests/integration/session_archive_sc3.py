"""Deployed-server e2e for session-archive/AC3 (거부 응답의 구분 전달).

검증 시나리오: test-session-archive.md#시나리오 3
실행 대상: primary
병렬 레인: session

kind 의 제어면은 아카이브를 켜지 않고 뜨므로(`tests/k8s/kind/session-platform.yaml`) 살아 있는 shell
세션의 동결은 실물 503 이다 — 그래서 unavailable 은 여기서 진짜 이유로 관측된다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _session_platform import live_shell_session, sessions_from

ABSENT_SESSION_ID = "e2e-archive-sc3-absent"


async def test_session_archive_sc3_absent_session_is_not_found(session) -> None:
    result = await session.call_tool("session_archive", {"id": ABSENT_SESSION_ID})
    assert result.isError is True, result
    text = result.content[0].text
    assert "not found" in text, text
    assert ABSENT_SESSION_ID in text, text
    assert "retry" not in text, f"a missing session must not invite a retry: {text}"
    assert "unavailable" not in text, text
    print("not found ok:", text)


async def test_session_archive_sc3_disabled_archive_is_unavailable(
    session, live: dict
) -> None:
    result = await session.call_tool("session_archive", {"id": live["id"]})
    assert result.isError is True, result
    text = result.content[0].text
    assert "unavailable" in text, text
    assert "not found" not in text, text

    listed = {s["id"]: s for s in sessions_from(await session.call_tool("session_list", {}))}
    assert listed.get(live["id"], {}).get("state") == "active", (
        f"a refused archive must leave the session active: {listed.get(live['id'])}"
    )

    survivor = await session.call_tool("ping", {})
    assert survivor.isError is False and survivor.content[0].text == "pong", survivor
    print("unavailable ok:", text)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    with live_shell_session("e2e archive sc3") as (_, live):
        async with open_session(url) as session:
            print("--- session-archive/시나리오 3 ---")
            await test_session_archive_sc3_absent_session_is_not_found(session)
            await test_session_archive_sc3_disabled_archive_is_unavailable(session, live)
            print("ok: test-session-archive.md#시나리오 3")


if __name__ == "__main__":
    asyncio.run(run())
