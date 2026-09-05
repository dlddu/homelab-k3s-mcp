"""Deployed-server e2e for github-app-installation-token/AC2 (scope restriction).

검증 AC: github-app-installation-token/AC2
실행 대상: primary
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _helpers import base_url, open_session, parse_env_resource, wait_for_healthz


# Must match GITHUB_APP_INSTALLATION_ID in the CI "Create test GitHub App secret"
# step, which is the installation id the mock embeds in the issued token.
EXPECTED_INSTALLATION_ID = "67890"


async def test_github_app_installation_token_ac2_scope_restriction(
    session: ClientSession,
) -> None:
    """AC: github-app-installation-token/AC2 — requested scope narrows the token.

    Asserts both branches the AC names. Unscoped: with neither ``repositories``
    nor ``permissions`` the token is issued for the whole installation
    (``Repository selection: all``) with the installation's default permissions.
    Scoped: passing one repository and ``contents=read`` narrows the issued
    token to that subset (``Repository selection: selected`` and
    ``Permissions: contents=read`` — the default ``metadata=read`` is gone).

    The AC's "설치 범위를 벗어난 요청은 거부된다" clause is GitHub-side behaviour;
    the mock echoes whatever scope it is handed, so rejection of an
    out-of-installation repository is not observable against this fixture.
    """
    unscoped = await session.call_tool("github_app_installation_token", {})
    assert unscoped.isError is False, unscoped
    env_text, _ = parse_env_resource(unscoped)
    assert "# Repository selection: all" in env_text, env_text
    assert "# Permissions: contents=read, metadata=read" in env_text, env_text

    scoped = await session.call_tool(
        "github_app_installation_token",
        {
            "repositories": ["homelab-k3s-mcp"],
            "permissions": {"contents": "read"},
        },
    )
    assert scoped.isError is False, scoped
    env_text, _ = parse_env_resource(scoped)
    assert f"GITHUB_TOKEN=ghs_mock_{EXPECTED_INSTALLATION_ID}" in env_text, env_text
    assert "# Repository selection: selected" in env_text, env_text
    assert "# Permissions: contents=read" in env_text, env_text
    assert "metadata=read" not in env_text, env_text


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github_app_installation_token (AC: github-app-installation-token/AC2) ---")
        await test_github_app_installation_token_ac2_scope_restriction(session)
        print("ok: github-app-installation-token/AC2")


if __name__ == "__main__":
    asyncio.run(run())
