"""ping: 인자 없는 호출은 언제나 ``pong`` 으로 성공한다 (e2e).

검증 시나리오: test-ping.md#시나리오 1
실행 대상: primary
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz


async def test_ping_ac1_always_pong(session: ClientSession) -> None:
    """AC: ping/AC1 — an argument-less call always succeeds with ``pong``.

    """
    result = await session.call_tool("ping", {})
    assert result.isError is False, result
    assert result.content, result
    block = result.content[0]
    assert block.type == "text", block
    assert block.text == "pong", block.text


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- ping (AC: ping/AC1) ---")
        await test_ping_ac1_always_pong(session)
        print("ping ok")


if __name__ == "__main__":
    asyncio.run(run())
