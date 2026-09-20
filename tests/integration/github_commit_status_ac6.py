"""Deployed-server e2e for github-commit-status/AC6 (어노테이션 광고).

검증 시나리오: test-github-commit-status.md#시나리오 6
실행 대상: primary
병렬 레인: github-app

이 파일은 도구를 호출하지 않는다 — `tools/list` 메타데이터만 읽으므로 mock 의
요청 로그도 픽스처 상태도 건드리지 않는다. 레인을 선언한 것은 상태 때문이 아니라
같은 도메인의 파일들과 한 줄에 세워 두기 위해서다.
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _helpers import base_url, open_session, wait_for_healthz

TOOL = "github_commit_status_create"

# 세 값을 한 벌로 재는 것이 이 시나리오의 요점이다. 이 도구는 "외부에 쓰지만
# 파괴하지는 않는다" 는 자리에 있고, 그 자리는 세 힌트의 **조합**으로만 표현된다 —
# 하나씩 따로 재면 읽기 전용으로 잘못 광고된 서버와 파괴적으로 과표기된 서버가
# 서로 다른 단언에서 갈려 어느 쪽도 이 자리를 지키지 못한다.
EXPECTED_HINTS = {
    "readOnlyHint": False,
    "destructiveHint": False,
    "openWorldHint": True,
}


async def test_github_commit_status_ac6_annotation_advertisement(
    session: ClientSession,
) -> None:
    """AC: github-commit-status/AC6

    Promotes the in-process assertion in ``internal/server/mcp_test.go``
    (``TestToolsListAdvertisesCommitStatus``) to the deployed-server layer: the
    hints are read back off the wire from the running pod, so a build that
    advertises them only in the in-process fixture is told apart from one that
    serves them.
    """
    tools = await session.list_tools()
    by_name = {tool.name: tool for tool in tools.tools}
    assert TOOL in by_name, f"{TOOL} not advertised by tools/list: {sorted(by_name)}"

    annotations = by_name[TOOL].annotations
    assert annotations is not None, f"{TOOL} advertises no annotations"
    advertised = {name: getattr(annotations, name, None) for name in EXPECTED_HINTS}
    assert advertised == EXPECTED_HINTS, (
        f"{TOOL} annotations = {advertised}, expected {EXPECTED_HINTS}"
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github-commit-status/AC6 ---")
        await test_github_commit_status_ac6_annotation_advertisement(session)
        print("ok: github-commit-status/AC6")


if __name__ == "__main__":
    asyncio.run(run())
