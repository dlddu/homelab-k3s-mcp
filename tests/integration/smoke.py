"""primary 배포의 도구 표면 확인 (규칙 3 비-시나리오 파일 — 시나리오를 주검증하지 않는다).

검증 시나리오: 없음 (스모크/인프라)
실행 대상: primary
실행 순서: 0

이 파일은 `docs/doc-tracker.md` 「비-시나리오 파일」 절의 규칙 3 등재분이다.

여기서 확인하는 것은 **뒤따르는 AC 파일들의 공유 선행 조건**이다: primary 그룹의 케이스들이
구동하는 도구가 실제로 광고되고 있는지. `실행 순서: 0` 으로 그룹 맨 앞에서 돌기 때문에,
배포가 깨졌을 때 뒤따르는 파일들이 차례로 모호하게 죽는 대신 여기서 한 번에 원인을
말한다(파일 수는 분할이 진행될수록 늘어나므로 적지 않는다).

이것은 platform-auth-safety/AC5(서버 수준 graceful degradation)가 **아니다**. 이 배포는
모든 통합이 구성돼 있어 정상적인 tools/list 가 degradation 에 대해 아무것도 말해 주지
않는다 — AC5 는 자격증명이 없는 배포에서만 관측되므로 그 전용 파일
(`platform_auth_safety_ac5.py`)은 auth-variant 에서 돈다.
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
