"""Deployed-server e2e for session-archive/AC4 (도구 표기).

검증 시나리오: test-session-archive.md#시나리오 4
실행 대상: primary
병렬 레인: session
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz


async def test_session_archive_sc4_annotations(session) -> None:
    tools = await session.list_tools()
    by_name = {tool.name: tool for tool in tools.tools}
    assert "session_archive" in by_name, sorted(by_name)
    tool = by_name["session_archive"]
    assert tool.inputSchema.get("required") == ["id"], tool.inputSchema
    hints = tool.annotations
    assert hints is not None, tool
    assert hints.readOnlyHint is False, hints
    assert hints.destructiveHint is False, hints
    assert hints.idempotentHint is True, hints
    assert hints.openWorldHint is True, hints


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- session-archive/시나리오 4 ---")
        await test_session_archive_sc4_annotations(session)
        print("ok: test-session-archive.md#시나리오 4")


if __name__ == "__main__":
    asyncio.run(run())
