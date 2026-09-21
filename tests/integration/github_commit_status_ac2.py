"""Deployed-server e2e for github-commit-status/AC2 (내부 토큰 스코프와 비노출).

검증 시나리오: test-github-commit-status.md#시나리오 2
실행 대상: primary
병렬 레인: github-app

Both claims here are about a token the caller never sees, so neither can be
read off the tool result alone. The scope claim is read from the mint request
body the github-mock recorded; the non-exposure claim is a scan of the whole
serialized result, and it is made non-vacuous by the revoke request that same
log shows — the server demonstrably held ``ghs_mock_67890`` and still returned
a result without it.
"""

from __future__ import annotations

import asyncio
import json
import os
from typing import Any

import httpx
from mcp import ClientSession

from _helpers import base_url, open_session, wait_for_healthz

MOCK_URL = os.environ.get("GITHUB_MOCK_URL", "http://127.0.0.1:8093").rstrip("/")

# Must match GITHUB_APP_INSTALLATION_ID in the CI "Create test GitHub App secret"
# step; the mock embeds it in the token it issues.
INSTALLATION_ID = "67890"
MINT_PATH = f"/app/installations/{INSTALLATION_ID}/access_tokens"
REVOKE_PATH = "/installation/token"
MINTED_TOKEN = f"ghs_mock_{INSTALLATION_ID}"

REPOSITORY = "test"
EXPECTED_MINT_BODY = {
    "repositories": [REPOSITORY],
    "permissions": {"statuses": "write"},
}

SUCCESS_ARGUMENTS = {
    "repository": REPOSITORY,
    "sha": "0123456789abcdef0123456789abcdef01234567",
    "state": "success",
    "context": "homelab-k3s-mcp/e2e",
    "description": "e2e scope check",
    "target_url": "https://example.invalid/run/2",
}
FAILURE_ARGUMENTS = {**SUCCESS_ARGUMENTS, "sha": "f" * 40}

LEAK_MARKERS = (MINTED_TOKEN, "-----BEGIN", "eyJ")


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


async def call_and_read_mint(session: ClientSession, arguments: dict[str, Any]):
    """One tool call; returns (result, the single mint body it caused)."""
    reset_recorded()
    result = await session.call_tool("github_commit_status_create", arguments)
    mints = recorded("POST", MINT_PATH)
    assert len(mints) == 1, f"expected exactly one mint for {arguments['sha']}, got {mints}"
    return result, json.loads(mints[0]["body"])


async def test_github_commit_status_ac2_mints_a_narrow_token(
    session: ClientSession,
) -> None:
    """AC: github-commit-status/AC2 — the mint body is exactly one repo, statuses:write.

    Checked on the success call and on the 422 call alike: the scope is
    decided before the upstream answers, so an error path that widened it
    would be just as much a violation.
    """
    success, success_mint = await call_and_read_mint(session, SUCCESS_ARGUMENTS)
    assert success.isError is False, success
    assert success_mint == EXPECTED_MINT_BODY, success_mint

    failure, failure_mint = await call_and_read_mint(session, FAILURE_ARGUMENTS)
    assert failure.isError is True, failure
    assert failure_mint == EXPECTED_MINT_BODY, failure_mint


async def test_github_commit_status_ac2_never_exposes_the_token(
    session: ClientSession,
) -> None:
    """AC: github-commit-status/AC2 — no token, PEM or JWT in either result.

    The revoke the server sends afterwards carries the minted token in its
    Authorization header, and that recorded header is the positive control:
    the token existed on the server side of this very call, so "absent from
    the result" is a statement about discarding it, not about never having it.
    """
    for arguments in (SUCCESS_ARGUMENTS, FAILURE_ARGUMENTS):
        result, _ = await call_and_read_mint(session, arguments)

        revocations = recorded("DELETE", REVOKE_PATH)
        assert len(revocations) == 1, revocations
        assert revocations[0]["authorization"] == f"Bearer {MINTED_TOKEN}", revocations

        serialized = result.model_dump_json()
        for marker in LEAK_MARKERS:
            assert marker not in serialized, (
                f"{marker!r} leaked into the result for sha {arguments['sha']}: "
                f"{serialized}"
            )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github-commit-status/AC2 ---")
        try:
            await test_github_commit_status_ac2_mints_a_narrow_token(session)
            await test_github_commit_status_ac2_never_exposes_the_token(session)
        finally:
            reset_recorded()
        print("ok: github-commit-status/AC2")


if __name__ == "__main__":
    asyncio.run(run())
