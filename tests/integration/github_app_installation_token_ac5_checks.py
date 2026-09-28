"""Deployed-server e2e for github-app-installation-token/AC5 (checks write/read).

검증 시나리오: test-github-app-installation-token.md#시나리오 6
실행 대상: primary
병렬 레인: github-app

EXPECTED_INSTALLATION_ID must match GITHUB_APP_INSTALLATION_ID in the CI
"Create test GitHub App secret" step, which is the installation id the mock
embeds in the issued token.
"""

from __future__ import annotations

import asyncio
import json
import os
from typing import Any

import httpx
from mcp import ClientSession

from _helpers import base_url, open_session, parse_env_resource, wait_for_healthz


EXPECTED_INSTALLATION_ID = "67890"
MINT_PATH = f"/app/installations/{EXPECTED_INSTALLATION_ID}/access_tokens"
REVOKE_PATH = "/installation/token"

MOCK_URL = os.environ.get("GITHUB_MOCK_URL", "http://127.0.0.1:8093").rstrip("/")


def reset_mock(**config: Any) -> None:
    """Clear the recorded requests and set the mock's knobs to ``config``.

    ``reset_mock()`` with no arguments restores the stock mock the other
    github-app files expect.
    """
    httpx.post(f"{MOCK_URL}/_admin/config", json=config, timeout=10.0).raise_for_status()
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


def mint_body() -> dict[str, Any]:
    mints = recorded("POST", MINT_PATH)
    assert len(mints) == 1, mints
    body = json.loads(mints[0]["body"])
    assert "permissions" in body, body
    return body


async def test_sc6_explicit_checks_write_is_refused(session: ClientSession) -> None:
    """시나리오 6 ① — an explicit ``checks: write`` never reaches the mint call.

    Zero recorded mint requests is the load-bearing half: a server that asked
    for the write scope and then discarded the answer would still be wrong.
    """
    for arguments in (
        {"permissions": {"checks": "write"}},
        {"permissions": {"contents": "read", "checks": "write"}},
    ):
        reset_mock()
        result = await session.call_tool("github_app_installation_token", arguments)
        text = refusal_text(result)
        assert "checks" in text, text
        assert "check run" in text, text
        assert recorded("POST", MINT_PATH) == [], f"{arguments} reached the mint call"


async def test_sc6_checks_read_is_issued(session: ClientSession) -> None:
    """시나리오 6 ② — ``checks: read`` is ordinary and is issued unchanged."""
    reset_mock()
    result = await session.call_tool(
        "github_app_installation_token", {"permissions": {"checks": "read"}}
    )
    assert result.isError is False, result
    env_text, _ = parse_env_resource(result)
    assert "# Permissions: checks=read" in env_text, env_text
    assert len(recorded("POST", MINT_PATH)) == 1, "expected exactly one mint call"


async def test_sc6_default_downgrades_checks_to_read(session: ClientSession) -> None:
    """시나리오 6 ③ — with no arguments the installation grant is downgraded.

    The assertion is on the *request body* the server sent, because "the token
    happens to come back with read" would also hold for a server that forwarded
    the write scope and got lucky with the upstream's response.
    """
    reset_mock(installation_permissions={"checks": "write"})
    result = await session.call_tool("github_app_installation_token", {})
    assert result.isError is False, result

    body = mint_body()
    assert body["permissions"].get("checks") == "read", body

    env_text, _ = parse_env_resource(result)
    assert "checks=read" in env_text, env_text
    assert "checks=write" not in env_text, env_text


async def test_sc6_token_carrying_checks_write_is_revoked(
    session: ClientSession,
) -> None:
    """시나리오 6 ④ — a token that comes back with ``checks: write`` is discarded.

    This is the case the request filter cannot cover: the upstream hands back
    more than it was asked for. The token must be revoked upstream *and* must
    not reach the caller, so the whole serialized result is scanned — the token
    string leaking into a structured field would be just as bad as leaking into
    the .env text.
    """
    reset_mock(mint_extra_permissions={"checks": "write"})
    result = await session.call_tool(
        "github_app_installation_token", {"permissions": {"contents": "read"}}
    )
    text = refusal_text(result)
    assert "revoked" in text, text

    revocations = recorded("DELETE", REVOKE_PATH)
    assert len(revocations) == 1, revocations
    minted = f"ghs_mock_{EXPECTED_INSTALLATION_ID}"
    assert revocations[0]["authorization"] == f"Bearer {minted}", revocations
    assert minted not in result.model_dump_json(), result


async def test_sc6_both_excluded_permissions_are_downgraded(
    session: ClientSession,
) -> None:
    """시나리오 6 ⑤ — a grant carrying both write scopes is downgraded in full."""
    reset_mock(installation_permissions={"statuses": "write", "checks": "write"})
    result = await session.call_tool("github_app_installation_token", {})
    assert result.isError is False, result

    body = mint_body()
    assert body["permissions"].get("statuses") == "read", body
    assert body["permissions"].get("checks") == "read", body


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github_app_installation_token (AC: github-app-installation-token/AC5, checks) ---")
        try:
            await test_sc6_explicit_checks_write_is_refused(session)
            await test_sc6_checks_read_is_issued(session)
            await test_sc6_default_downgrades_checks_to_read(session)
            await test_sc6_token_carrying_checks_write_is_revoked(session)
            await test_sc6_both_excluded_permissions_are_downgraded(session)
        finally:
            reset_mock()
        print("ok: github-app-installation-token/AC5 (checks)")


if __name__ == "__main__":
    asyncio.run(run())
