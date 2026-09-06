"""Deployed-server e2e for session-write/AC3 (파괴적 작업 표기).

검증 AC: session-write/AC3
실행 대상: primary

``tools/list`` 메타데이터만 읽고 **write 를 실행하지 않는다** — 세션도 에이전트 파드도
필요 없으므로 선행 조건이 없다. 이 AC 가 backlog 의 나머지와 갈라지는 지점이 정확히
그것이다: 같은 도구의 AC1·AC2·AC4 는 제어면이 에이전트 파드를 치는 경로라 실 데이터
플레인(그리고 AC2 는 추가로 CRIU 게이트)이 선행이지만, AC3 의 검증 방법은 광고된
어노테이션뿐이다.

단언이 vacuous 하지 않은 이유: ``internal/mcp`` 의 ``toolsListJSON`` 은 서비스 구성과
무관한 **정적 리터럴**이고 ``mcp.go`` 가 그것을 그대로 반환하므로, 이 파일은 배포된
서버가 실제로 내보내는 표면을 읽는다 — 도구가 등록에서 빠지거나 어노테이션이
뒤집히면 여기서 잡힌다. 같은 단언의 in-process 판은
``internal/server/mcp_test.go`` 의 ``TestToolsListAdvertisesAnnotations`` 이며,
이 파일은 그것을 배포 서버 계층으로 승격한 것이다.

``workload_scale_ac3.py`` · ``workload_restart_ac2.py`` ·
``opensearch_document_{put,delete}_ac3.py`` · ``dear_baby_reset_user_ac3.py`` 가
같은 자리의 선례다.
"""

from __future__ import annotations

import asyncio

from _helpers import assert_destructive_annotation, base_url, open_session, wait_for_healthz


async def test_session_write_ac3_destructive_hint(session) -> None:
    """AC: session-write/AC3 — session_write advertises destructiveHint=true.

    Verifies the destructive-operation marking via tools/list metadata only; no
    payload is injected into any session.
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
