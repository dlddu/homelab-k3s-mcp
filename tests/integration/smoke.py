"""primary 배포의 도구 표면 확인 (규칙 3 비-AC 파일 — AC를 주검증하지 않는다).

검증 AC: 없음 (스모크/인프라)
실행 대상: primary
실행 순서: 0

모델 `tbm_homelab-k3s-mcp-ac-e2e` 의 규칙 3은 AC 대신 **스모크/인프라 확인(서버 기동·
`/healthz`·도구 표면 존재)** 을 주검증하는 파일을 허용하되 `docs/doc-tracker.md` 의
「비-AC 파일」 절에 등재할 것을 요구한다. 이 파일이 그 등재분이다.

여기서 확인하는 것은 **뒤따르는 AC 파일들의 공유 선행 조건**이다: primary 그룹의 케이스들이
구동하는 도구가 실제로 광고되고 있는지. `실행 순서: 0` 으로 그룹 맨 앞에서 돌기 때문에,
배포가 깨졌을 때 뒤따르는 파일들이 차례로 모호하게 죽는 대신 여기서 한 번에 원인을
말한다(파일 수는 분할이 진행될수록 늘어나므로 적지 않는다).

이것은 platform-auth-safety/AC5(서버 수준 graceful degradation)가 **아니다**. 이 배포는
모든 통합이 구성돼 있어 정상적인 tools/list 가 degradation 에 대해 아무것도 말해 주지
않는다 — AC5 는 자격증명이 없는 배포에서만 관측되므로 그 전용 파일
(`platform_auth_safety_ac5.py`)은 auth-variant 에서 돈다. 두 파일이 같은
`_helpers.EXPECTED_TOOLS` 를 읽는 것은 의도한 것이다: 도구 표면은 구성과 무관하게
`internal/mcp/toolslist.go` 가 정적으로 선언하므로 두 배포에서 같아야 한다.
"""

from __future__ import annotations

import asyncio

from _helpers import EXPECTED_TOOLS, base_url, open_session, wait_for_healthz


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        tools = await session.list_tools()
        names = {tool.name for tool in tools.tools}
        missing = EXPECTED_TOOLS - names
        assert not missing, (
            f"missing tools: {sorted(missing)} (got {sorted(names)})"
        )
        print("tools/list ok:", sorted(names))


if __name__ == "__main__":
    asyncio.run(run())
