"""Deployed-server e2e for github-commit-status/AC1 (요청은 PocketAide 에 쌓이고 도구는 바로 돌아온다).

검증 시나리오: test-github-commit-status.md#시나리오 1
실행 대상: primary
병렬 레인: github-app

두 끝에서 잰다 — 도구 결과만 보면 요청을 지어내는 서버를 못 가르고, PocketAide 만 보면 결과를
돌려주지 않는 서버를 못 가른다. 요청은 실물 PocketAide 의 primary 사용자(`e2e-primary`) 몫으로
쌓이고, 다음 실행의 대기 목록에 남지 않게 끝에서 거절한다(거절은 GitHub 에 아무것도 쓰지 않는다).
"""

from __future__ import annotations

import asyncio
import os
import time
import uuid

import httpx
from mcp import ClientSession

from _approval_gate import approval_url, commit_status_request, reject_commit_status_request
from _helpers import base_url, open_session, wait_for_healthz

TOOL = "github_commit_status_request"
MOCK_URL = os.environ.get("GITHUB_MOCK_URL", "http://127.0.0.1:8093").rstrip("/")
OWNER = "e2e-primary"


def arguments(marker: str) -> dict:
    return {
        "repository": "dlddu/test",
        "sha": "0123456789abcdef0123456789abcdef01234567",
        "state": "success",
        "context": "homelab-k3s-mcp/e2e",
        "description": f"e2e {marker}",
        "target_url": "https://example.invalid/pr/1",
        "requester": f"e2e 하네스 {marker}",
        "explanation": "e2e 가 요청을 쌓고 거절한다.",
    }


def github_requests() -> list[dict]:
    response = httpx.get(f"{MOCK_URL}/_admin/requests", timeout=10.0)
    response.raise_for_status()
    return response.json()["requests"]


async def test_github_commit_status_ac1_request_is_filed_and_returns_at_once(
    session: ClientSession,
) -> None:
    marker = uuid.uuid4().hex[:12]
    httpx.delete(f"{MOCK_URL}/_admin/requests", timeout=10.0).raise_for_status()

    started = time.monotonic()
    result = await session.call_tool(TOOL, arguments(marker))
    elapsed = time.monotonic() - started
    assert result.isError is False, result
    ticket = result.structuredContent
    assert ticket["status"] == "PENDING" and ticket["id"], ticket
    assert isinstance(ticket["created_at"], int) and ticket["created_at"] > 0, ticket
    assert elapsed < 15, f"the call took {elapsed:.1f}s — it must not wait for a decision"

    with approval_url() as url:
        stored = commit_status_request(url, ticket["id"], OWNER)
        assert stored is not None, f"PocketAide has no request {ticket['id']} for {OWNER}"
        try:
            expected = {
                "status": "PENDING",
                "owner": "dlddu",
                "repo": "test",
                "sha": "0123456789abcdef0123456789abcdef01234567",
                "state": "success",
                "context": "homelab-k3s-mcp/e2e",
                "description": f"e2e {marker}",
                "target_url": "https://example.invalid/pr/1",
                "requester": f"e2e 하네스 {marker}",
                "reason": "e2e 가 요청을 쌓고 거절한다.",
                "key_name": stored["key_name"],
                "github_status_id": None,
            }
            got = {key: stored.get(key) for key in expected}
            assert got == expected, f"stored request {got} != {expected}"
            assert stored["key_name"], stored
        finally:
            reject_commit_status_request(url, ticket["id"], OWNER)

    writes = [r for r in github_requests() if "/statuses/" in r["path"]]
    assert writes == [], f"the tool wrote a status itself: {writes}"


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github-commit-status/AC1 ---")
        await test_github_commit_status_ac1_request_is_filed_and_returns_at_once(session)
        print("ok: github-commit-status/AC1")


if __name__ == "__main__":
    asyncio.run(run())
