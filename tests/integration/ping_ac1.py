"""ping: 인자 없는 호출은 언제나 ``pong`` 으로 성공한다 (e2e).

검증 AC: ping/AC1
실행 대상: primary

`tests/integration/check_ac_mapping.py`가 이 선언을 읽어 `docs/doc-tracker.md`의 레지스트리와
대조하고, `tests/integration/run_all.py`가 `실행 대상`을 읽어 이 파일을 배차한다.

배포 서버를 상대로 돈다 — ``internal/server/mcp_test.go`` 의 in-process 단언
(``TestPingToolReturnsPong``)을 배포 e2e 계층으로 승격한 것이다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz


async def test_ping_ac1_always_pong(session: ClientSession) -> None:
    """AC: ping/AC1 — an argument-less call always succeeds with ``pong``.

    Calls the deployed ``ping`` tool with no arguments and asserts the result is
    a non-error MCP tool result whose single content block is the text ``pong``
    exactly — the AC's stated verification method. This promotes the in-process
    assertion in ``internal/server/mcp_test.go`` (``TestPingToolReturnsPong``) to
    the deployed-server e2e layer.
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
