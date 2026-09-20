"""github_commit_status_create: GitHub App 미설정 시 graceful 거부 (e2e).

검증 시나리오: test-github-commit-status.md#시나리오 5
실행 대상: auth-variant
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _auth_variant import API_KEY, GITHUB_REFUSAL, assert_unavailable_refusal
from _helpers import base_url, open_session, wait_for_healthz

# 인자는 전부 유효하다. `Unavailable` 은 입력 검증을 거치지 않고 곧바로 거부하므로
# 흠 있는 입력으로 부르면 "미설정이라 거부했다" 와 "인자가 틀려서 거부했다" 가
# 같은 문면으로 겹쳐 이 시나리오의 주장이 사라진다 — 유효한 입력만이 두 경로를 가른다.
VALID_ARGUMENTS = {
    "repository": "homelab-k3s-mcp",
    "sha": "0" * 40,
    "state": "success",
    "context": "homelab-k3s-mcp/e2e",
}


async def test_github_commit_status_ac5_unconfigured_refusal(
    session: ClientSession,
) -> None:
    """AC: github-commit-status/AC5

    ``assert_unavailable_refusal`` 이 재는 두 조각이 그대로 이 시나리오의 기대
    결과다 — 거부가 전송 실패가 아니라 정상 도구 결과로 돌아오는 것과, 그 직후
    같은 세션의 ``ping`` 이 살아 있는 것("graceful" 의 나머지 절반).
    """
    await assert_unavailable_refusal(
        session,
        "github_commit_status_create",
        VALID_ARGUMENTS,
        GITHUB_REFUSAL,
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        print("--- unconfigured graceful refusal (AC: github-commit-status/AC5) ---")
        await test_github_commit_status_ac5_unconfigured_refusal(session)
        print("refusal ok: github-commit-status/AC5")


if __name__ == "__main__":
    asyncio.run(run())
