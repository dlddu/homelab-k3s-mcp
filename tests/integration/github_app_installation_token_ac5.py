"""Deployed-server e2e for github-app-installation-token/AC5 (statuses write/read).

검증 시나리오: test-github-app-installation-token.md#시나리오 5
실행 대상: primary
병렬 레인: github-app
"""

from __future__ import annotations

import asyncio
import json
import os
from typing import Any

import httpx
from mcp import ClientSession

from _helpers import base_url, open_session, parse_env_resource, wait_for_healthz


# Must match GITHUB_APP_INSTALLATION_ID in the CI "Create test GitHub App secret"
# step, which is the installation id the mock embeds in the issued token.
EXPECTED_INSTALLATION_ID = "67890"
MINT_PATH = f"/app/installations/{EXPECTED_INSTALLATION_ID}/access_tokens"
INSTALLATION_PATH = f"/app/installations/{EXPECTED_INSTALLATION_ID}"
REVOKE_PATH = "/installation/token"

# The admin surface of tests/k8s/kind/github-mock.yaml. It is reached directly
# rather than through _helpers because this is the only file that drives the
# mock's knobs; the http-trace proxy in front of MinIO/OpenSearch is the shared
# case and lives there.
MOCK_URL = os.environ.get("GITHUB_MOCK_URL", "http://127.0.0.1:8093").rstrip("/")

# AC5 is mostly a claim about requests that must NOT happen, so the upstream
# request log — not the returned token — is what decides these cases.


def reset_mock(**config: Any) -> None:
    """Clear the recorded requests and set the mock's knobs to ``config``.

    ``reset_mock()`` with no arguments restores the stock mock the other
    github-app files expect.
    """
    httpx.post(f"{MOCK_URL}/_admin/config", json=config, timeout=10.0).raise_for_status()
    httpx.delete(f"{MOCK_URL}/_admin/requests", timeout=10.0).raise_for_status()


def recorded(method: str, path: str) -> list[dict[str, Any]]:
    """Return the requests the mock recorded for ``method`` ``path``."""
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


async def test_ac5_explicit_statuses_write_is_refused(session: ClientSession) -> None:
    """AC5 ① — an explicit ``statuses: write`` never reaches the mint call.

    Both shapes the scenario names are checked: ``statuses`` alone and
    ``statuses`` riding along with an unrelated permission. The refusal has to
    name ``github_commit_status_create`` because that is where the capability
    moved; a bare "not allowed" would leave the caller with no next step. Zero
    recorded mint requests is the load-bearing half — a server that asked for
    the write scope and then discarded the answer would still be wrong.
    """
    for arguments in (
        {"permissions": {"statuses": "write"}},
        {"permissions": {"contents": "read", "statuses": "write"}},
    ):
        reset_mock()
        result = await session.call_tool("github_app_installation_token", arguments)
        text = refusal_text(result)
        assert "github_commit_status_create" in text, text
        assert "statuses" in text, text
        assert recorded("POST", MINT_PATH) == [], f"{arguments} reached the mint call"


async def test_ac5_statuses_read_is_issued(session: ClientSession) -> None:
    """AC5 ② — ``statuses: read`` is ordinary and is issued unchanged."""
    reset_mock()
    result = await session.call_tool(
        "github_app_installation_token", {"permissions": {"statuses": "read"}}
    )
    assert result.isError is False, result
    env_text, _ = parse_env_resource(result)
    assert "# Permissions: statuses=read" in env_text, env_text
    assert len(recorded("POST", MINT_PATH)) == 1, "expected exactly one mint call"


async def test_ac5_default_downgrades_statuses_to_read(session: ClientSession) -> None:
    """AC5 ③ — with no arguments the installation grant is downgraded, not copied.

    The mock is put in the state the scenario describes: the installation
    genuinely holds ``statuses: write``. The assertion is on the *request body*
    the server sent, because "the token happens to come back with read" would
    also hold for a server that forwarded the write scope and got lucky with the
    upstream's response. ``permissions`` must be present and explicit — omitting
    the key would let GitHub apply the full installation grant.
    """
    reset_mock(installation_permissions={"statuses": "write"})
    result = await session.call_tool("github_app_installation_token", {})
    assert result.isError is False, result

    mints = recorded("POST", MINT_PATH)
    assert len(mints) == 1, mints
    body = json.loads(mints[0]["body"])
    assert "permissions" in body, body
    assert body["permissions"].get("statuses") == "read", body

    env_text, _ = parse_env_resource(result)
    assert "statuses=read" in env_text, env_text
    assert "statuses=write" not in env_text, env_text


async def test_ac5_unreadable_installation_refuses(session: ClientSession) -> None:
    """AC5 ④ — if the installation grant cannot be read, nothing is minted.

    The downgrade needs to know what the installation holds. When that lookup
    fails the safe move is to refuse, not to fall back to an unscoped mint.
    """
    reset_mock(installation_status=500)
    result = await session.call_tool("github_app_installation_token", {})
    text = refusal_text(result)
    assert INSTALLATION_PATH in text, text
    assert len(recorded("GET", INSTALLATION_PATH)) == 1, "lookup was not attempted"
    assert recorded("POST", MINT_PATH) == [], "minted despite an unreadable grant"


async def test_ac5_token_carrying_statuses_write_is_revoked(
    session: ClientSession,
) -> None:
    """AC5 ⑤ — a token that comes back with ``statuses: write`` is discarded.

    This is the case the request filter cannot cover: the upstream hands back
    more than it was asked for. The token must be revoked upstream *and* must
    not reach the caller, so the whole serialized result is scanned — the token
    string leaking into a structured field would be just as bad as leaking into
    the .env text.
    """
    reset_mock(mint_extra_permissions={"statuses": "write"})
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


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github_app_installation_token (AC: github-app-installation-token/AC5) ---")
        try:
            await test_ac5_explicit_statuses_write_is_refused(session)
            await test_ac5_statuses_read_is_issued(session)
            await test_ac5_default_downgrades_statuses_to_read(session)
            await test_ac5_unreadable_installation_refuses(session)
            await test_ac5_token_carrying_statuses_write_is_revoked(session)
        finally:
            reset_mock()
        print("ok: github-app-installation-token/AC5")


if __name__ == "__main__":
    asyncio.run(run())
