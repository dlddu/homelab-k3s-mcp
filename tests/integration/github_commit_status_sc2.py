"""Deployed-server e2e for github-commit-status/AC2 (서버는 GitHub 에 닿지 않고 자격증명을 다루지 않는다).

검증 시나리오: test-github-commit-status.md#시나리오 2
실행 대상: primary
병렬 레인: github-app

github-mock 의 요청 로그가 비어 있는 것만으로는 헛통과할 수 있다(스텁이 기록을 멈췄거나 로그가
늘 비어 있거나). 같은 세션에서 토큰 도구를 한 번 불러 로그가 **차는 것**을 양성 대조로 둔다.
"""

from __future__ import annotations

import asyncio
import os

import httpx
from mcp import ClientSession

from _approval_gate import approval_url, reject_commit_status_request
from _helpers import base_url, open_session, wait_for_healthz

TOOL = "github_commit_status_request"
MOCK_URL = os.environ.get("GITHUB_MOCK_URL", "http://127.0.0.1:8093").rstrip("/")
LEAK_MARKERS = ("ghs_", "-----BEGIN", "eyJ")

ARGUMENTS = {
    "repository": "dlddu/test",
    "sha": "abcdefabcdefabcdefabcdefabcdefabcdefabcd",
    "state": "success",
    "context": "homelab-k3s-mcp/e2e",
    "requester": "e2e 하네스 — AC2",
}


def reset_recorded() -> None:
    httpx.delete(f"{MOCK_URL}/_admin/requests", timeout=10.0).raise_for_status()


def recorded() -> list[dict]:
    response = httpx.get(f"{MOCK_URL}/_admin/requests", timeout=10.0)
    response.raise_for_status()
    return response.json()["requests"]


async def test_github_commit_status_ac2_never_reaches_github(session: ClientSession) -> None:
    reset_recorded()
    result = await session.call_tool(TOOL, ARGUMENTS)
    assert result.isError is False, result
    try:
        assert recorded() == [], f"the request tool reached GitHub: {recorded()}"
        serialized = result.model_dump_json()
        for marker in LEAK_MARKERS:
            assert marker not in serialized, f"{marker!r} in the result: {serialized}"
    finally:
        with approval_url() as url:
            reject_commit_status_request(url, result.structuredContent["id"])

    await session.call_tool(
        "github_app_installation_token", {"permissions": {"contents": "read"}}
    )
    assert recorded(), (
        "the token tool recorded nothing upstream either — the zero-request assertion above "
        "would hold vacuously"
    )
    reset_recorded()


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github-commit-status/AC2 ---")
        await test_github_commit_status_ac2_never_reaches_github(session)
        print("ok: github-commit-status/AC2")


if __name__ == "__main__":
    asyncio.run(run())
