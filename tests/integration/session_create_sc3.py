"""Deployed-server e2e for session-create/AC3 (비파괴 추가 작업 표기).

검증 시나리오: test-session-create.md#시나리오 3
실행 대상: primary
병렬 레인: session
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz


async def test_session_create_sc3_annotations(session) -> None:
    tools = {tool.name: tool for tool in (await session.list_tools()).tools}
    assert "session_create" in tools, sorted(tools)
    annotations = tools["session_create"].annotations
    assert annotations is not None, "session_create advertises no annotations"
    assert annotations.readOnlyHint is False, annotations
    assert annotations.destructiveHint is False, annotations
    assert annotations.idempotentHint is False, annotations


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- session-create/시나리오 3 ---")
        await test_session_create_sc3_annotations(session)
        print("ok: test-session-create.md#시나리오 3")


if __name__ == "__main__":
    asyncio.run(run())
