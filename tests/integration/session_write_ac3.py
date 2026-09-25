"""Deployed-server e2e for session-write/AC3 (파괴적 작업 표기).

검증 시나리오: test-session-write.md#시나리오 3
실행 대상: primary
병렬 레인: session

단언이 vacuous 하지 않은 이유: ``internal/mcp`` 의 ``toolsListJSON`` 은 서비스 구성과
무관한 **정적 리터럴**이고 ``mcp.go`` 가 그것을 그대로 반환하므로, 이 파일은 배포된
서버가 실제로 내보내는 표면을 읽는다 — 도구가 등록에서 빠지거나 어노테이션이
뒤집히면 여기서 잡힌다.

``workload_scale_ac3.py`` · ``workload_restart_ac2.py`` ·
``opensearch_document_{put,delete}_ac3.py`` · ``dear_baby_reset_user_ac3.py`` 가
같은 자리의 선례다.
"""

from __future__ import annotations

import asyncio

from _helpers import assert_destructive_annotation, base_url, open_session, wait_for_healthz


async def test_session_write_ac3_destructive_hint(session) -> None:
    """AC: session-write/AC3 — session_write advertises destructiveHint=true.
    """
    await assert_destructive_annotation(session, "session_write")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- session-write/AC3 ---")
        await test_session_write_ac3_destructive_hint(session)
        print("ok: session-write/AC3")


if __name__ == "__main__":
    asyncio.run(run())
