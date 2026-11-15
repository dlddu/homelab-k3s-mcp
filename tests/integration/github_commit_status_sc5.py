"""github_commit_status_request: PocketAide 미설정 시 graceful 거부 (e2e).

검증 시나리오: test-github-commit-status.md#시나리오 5
실행 대상: auth-variant
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _auth_variant import API_KEY, POCKET_AIDE_REFUSAL, assert_unavailable_refusal
from _helpers import base_url, open_session, wait_for_healthz

# 인자는 전부 유효하다 — 입력 검증과 접두사 판정(auth-fixture 가 접두사 env 를 둔다)을
# 지나야 「PocketAide 미설정」 거부에 닿는다. 흠 있는 입력이면 검증 거부로 끝나 이 시나리오의
# 주장이 사라진다.
VALID_ARGUMENTS = {
    "repository": "dlddu/homelab-k3s-mcp",
    "sha": "0" * 40,
    "state": "success",
    "context": "homelab-k3s-mcp/e2e",
    "requester": "e2e 하네스",
}


async def test_github_commit_status_ac5_unconfigured_refusal(
    session: ClientSession,
) -> None:
    await assert_unavailable_refusal(
        session,
        "github_commit_status_request",
        VALID_ARGUMENTS,
        POCKET_AIDE_REFUSAL,
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
