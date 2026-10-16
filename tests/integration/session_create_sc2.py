"""Deployed-server e2e for session-create/AC2 (잘못된 요청은 아무것도 만들지 않는다).

검증 시나리오: test-session-create.md#시나리오 2
실행 대상: primary
병렬 레인: session

파드 집합을 실 제어면 앞에서 재므로 「파드를 만들지 않았다」가 vacuous 하지 않다 — 거부가
프로비저닝 뒤에 났다면 파드가 생기거나 회수 중으로 남는다.
"""

from __future__ import annotations

import asyncio

from mcp.shared.exceptions import McpError

from _helpers import base_url, open_session, wait_for_healthz
from _session_platform import pod_names, sessions_from

PROTOCOL_REFUSALS = (
    ({"name": "   "}, "name is required"),
    ({"name": "e2e create sc2", "workloadType": "bash"}, "workloadType must be one of"),
)
CONTROL_PLANE_REFUSAL = {"name": "e2e create sc2", "workloadType": "shell", "model": "sonnet"}


async def _snapshot(session) -> tuple[set[str], set[str]]:
    listed = sessions_from(await session.call_tool("session_list", {}))
    return {s["id"] for s in listed}, pod_names()


async def test_session_create_sc2_refusals_create_nothing(session) -> None:
    before = await _snapshot(session)

    for arguments, reason in PROTOCOL_REFUSALS:
        try:
            await session.call_tool("session_create", arguments)
        except McpError as exc:
            assert reason in str(exc), f"{arguments}: {exc}"
        else:
            raise AssertionError(f"{arguments} was not refused as invalid params")
        assert await _snapshot(session) == before, f"{arguments} changed the platform"
        print("protocol refusal ok:", reason)

    result = await session.call_tool("session_create", CONTROL_PLANE_REFUSAL)
    assert result.isError is True, result
    text = result.content[0].text
    assert "invalid argument" in text, text
    assert "unavailable" not in text, text
    assert await _snapshot(session) == before, "a refused model changed the platform"
    print("control plane refusal ok:", text)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- session-create/시나리오 2 ---")
        await test_session_create_sc2_refusals_create_nothing(session)
        print("ok: test-session-create.md#시나리오 2")


if __name__ == "__main__":
    asyncio.run(run())
