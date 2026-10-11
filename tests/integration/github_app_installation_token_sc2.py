"""Deployed-server e2e for github-app-installation-token/AC2 (scope restriction).

검증 시나리오: test-github-app-installation-token.md#시나리오 2
실행 대상: primary
병렬 레인: github-app
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _helpers import (
    GITHUB_APP_INSTALLATION_ID,
    base_url,
    open_session,
    parse_env_resource,
    wait_for_healthz,
)


async def test_github_app_installation_token_ac2_scope_restriction(
    session: ClientSession,
) -> None:
    """AC: github-app-installation-token/AC2 — requested scope narrows the token."""
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
    assert f"GITHUB_TOKEN=ghs_mock_{GITHUB_APP_INSTALLATION_ID}" in env_text, env_text
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
