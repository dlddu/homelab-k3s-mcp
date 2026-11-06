"""Deployed-server e2e for session-create/AC5 (codex 워크로드는 사람 승인을 거친다).

검증 시나리오: test-session-create.md#시나리오 5
실행 대상: primary
병렬 레인: session

kind 의 제어면은 ``shell`` 만 제공하므로 승인된 codex 호출도 세션을 만들지 못하고 제공되지
않는 타입으로 거부된다. 그 거부가 승인 **뒤에** 온다는 것이 게이트가 제어면보다 앞에 있다는
관측이다.
"""

from __future__ import annotations

import asyncio
import uuid

from mcp.shared.exceptions import McpError

from _approval_gate import (
    approval_url,
    count_requests,
    decide,
    get_request,
    wait_for_pending,
)
from _helpers import base_url, open_session, wait_for_healthz
from _session_platform import pod_names, sessions_from

REASON = "e2e 시나리오가 codex 세션 생성의 승인 경로를 검증하려고 부르는 호출이다."


async def _snapshot(session) -> tuple[set[str], set[str]]:
    listed = sessions_from(await session.call_tool("session_list", {}))
    return {s["id"] for s in listed}, pod_names()


async def _decided_codex_create(session, gate: str, marker: str, status: str):
    task = asyncio.create_task(
        session.call_tool(
            "session_create",
            {"name": marker, "workloadType": "codex", "reason": REASON},
        )
    )
    row = await wait_for_pending(gate, marker)
    assert 'workload type "codex"' in row["context"], row["context"]
    assert get_request(gate, row["id"])["reason"] == REASON, row
    await decide(gate, row["id"], status)
    return task


async def test_session_create_sc5_codex_needs_approval(session, gate: str) -> None:
    before = await _snapshot(session)

    marker = f"e2e-create-sc5-{uuid.uuid4().hex[:8]}"
    try:
        await session.call_tool("session_create", {"name": marker, "workloadType": "codex"})
    except McpError as exc:
        assert "reason is required" in str(exc), exc
    else:
        raise AssertionError("a codex create without reason was not refused")
    assert count_requests(gate, marker) == 0, "a refused call still asked for approval"
    assert await _snapshot(session) == before, "a refused call changed the platform"
    print("reason refusal ok")

    marker = f"e2e-create-sc5-{uuid.uuid4().hex[:8]}"
    task = await _decided_codex_create(session, gate, marker, "REJECTED")
    try:
        await task
    except McpError as exc:
        assert "approval rejected" in str(exc), exc
    else:
        raise AssertionError("a rejected codex create was not refused")
    assert await _snapshot(session) == before, "a rejected call changed the platform"
    print("rejection ok")

    marker = f"e2e-create-sc5-{uuid.uuid4().hex[:8]}"
    task = await _decided_codex_create(session, gate, marker, "APPROVED")
    result = await task
    assert result.isError is True, result
    text = result.content[0].text
    assert "workloadType must be one of shell" in text, text
    assert await _snapshot(session) == before, "an unoffered codex type changed the platform"
    print("approved call reaches the control plane ok:", text)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    with approval_url() as gate:
        async with open_session(url) as session:
            print("--- session-create/시나리오 5 ---")
            await test_session_create_sc5_codex_needs_approval(session, gate)
            print("ok: test-session-create.md#시나리오 5")


if __name__ == "__main__":
    asyncio.run(run())
