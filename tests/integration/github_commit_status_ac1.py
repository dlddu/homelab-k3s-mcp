"""Deployed-server e2e for github-commit-status/AC1 (status 기록과 GitHub 오류 전달).

검증 시나리오: test-github-commit-status.md#시나리오 1
실행 대상: primary
병렬 레인: github-app

(a) is measured at both ends — the github-mock request log for what the tool
sent, the tool result for what the caller got — because either end alone is
satisfiable by a server that fabricates the other. (b) is measured on the
refusal text alone: it has to carry the stub's own 422 wording, not a
paraphrase, or a bad sha becomes indistinguishable from an outage.
"""

from __future__ import annotations

import asyncio
import json
import os
from typing import Any

import httpx
from mcp import ClientSession

from _helpers import base_url, open_session, wait_for_healthz

# tests/k8s/kind/github-mock.yaml 의 admin 표면. 포트 8093 은 ci.yml 의
# "Run integration tests (primary deployment)" 스텝이 여는 포트포워드와 짝이다.
MOCK_URL = os.environ.get("GITHUB_MOCK_URL", "http://127.0.0.1:8093").rstrip("/")

# Fixture contract: docs/test-github-commit-status.md 「픽스처」.
INSTALLATION_OWNER = "dlddu"
STUB_ID = 1
STUB_CREATED_AT = "2026-01-01T00:00:00Z"
UNPROCESSABLE_SHA = "f" * 40

REPOSITORY = "test"
SHA = "0123456789abcdef0123456789abcdef01234567"
ARGUMENTS = {
    "repository": REPOSITORY,
    "sha": SHA,
    "state": "success",
    "context": "homelab-k3s-mcp/e2e",
    "description": "e2e status write",
    "target_url": "https://example.invalid/run/1",
}


def status_path(sha: str) -> str:
    return f"/repos/{INSTALLATION_OWNER}/{REPOSITORY}/statuses/{sha}"


def reset_recorded() -> None:
    httpx.delete(f"{MOCK_URL}/_admin/requests", timeout=10.0).raise_for_status()


def recorded(method: str, path: str) -> list[dict[str, Any]]:
    response = httpx.get(f"{MOCK_URL}/_admin/requests", timeout=10.0)
    response.raise_for_status()
    return [
        record
        for record in response.json()["requests"]
        if record["method"] == method and record["path"] == path
    ]


def refusal_text(result) -> str:
    assert result.isError is True, f"call did not refuse: {result}"
    assert result.content, result
    block = result.content[0]
    assert block.type == "text", block
    return block.text


async def test_github_commit_status_ac1_writes_the_status(session: ClientSession) -> None:
    """AC: github-commit-status/AC1 (a) — the four fields go out, the stub's answer comes back."""
    reset_recorded()
    result = await session.call_tool("github_commit_status_create", dict(ARGUMENTS))
    assert result.isError is False, result

    posts = recorded("POST", status_path(SHA))
    assert len(posts) == 1, f"expected exactly one status write, got {posts}"
    sent = json.loads(posts[0]["body"])
    assert sent == {
        "state": ARGUMENTS["state"],
        "context": ARGUMENTS["context"],
        "description": ARGUMENTS["description"],
        "target_url": ARGUMENTS["target_url"],
    }, sent

    payload = result.structuredContent
    assert isinstance(payload, dict), result
    assert payload["id"] == STUB_ID, payload
    assert payload["state"] == ARGUMENTS["state"], payload
    assert payload["context"] == ARGUMENTS["context"], payload
    assert payload["created_at"] == STUB_CREATED_AT, payload
    assert payload["sha"] == SHA, payload


async def test_github_commit_status_ac1_surfaces_github_error(
    session: ClientSession,
) -> None:
    """AC: github-commit-status/AC1 (b) — the upstream's 422 reaches the caller verbatim.

    The recorded write is the control: without it a locally produced error
    that happened to contain "422" would pass.
    """
    reset_recorded()
    result = await session.call_tool(
        "github_commit_status_create", {**ARGUMENTS, "sha": UNPROCESSABLE_SHA}
    )
    text = refusal_text(result)
    assert "422" in text, text
    assert f"No commit found for SHA: {UNPROCESSABLE_SHA}" in text, text
    assert len(recorded("POST", status_path(UNPROCESSABLE_SHA))) == 1, (
        "the 422 did not come from the upstream — no status write was recorded"
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github-commit-status/AC1 ---")
        try:
            await test_github_commit_status_ac1_writes_the_status(session)
            await test_github_commit_status_ac1_surfaces_github_error(session)
        finally:
            # The request log is shared mock state; the lane runs one file at a
            # time, so leaving records behind would leak into the next file.
            reset_recorded()
        print("ok: github-commit-status/AC1")


if __name__ == "__main__":
    asyncio.run(run())
