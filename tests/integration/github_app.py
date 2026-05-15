"""End-to-end checks for github_app_installation_token against the mock."""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz

# Must match GITHUB_APP_INSTALLATION_ID in tests/k8s/kind/github-app-patch.yaml,
# which is the installation id the mock embeds in the issued token.
EXPECTED_INSTALLATION_ID = "67890"


def parse_env_resource(result) -> tuple[str, str]:
    """Extract (env_text, mime_type) from a tool result's embedded resource."""
    assert result.content, result
    block = result.content[0]
    assert block.type == "resource", block
    resource = block.resource
    return resource.text, resource.mimeType


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github_app_installation_token (defaults) ---")
        result = await session.call_tool("github_app_installation_token", {})
        assert result.isError is False, result
        assert result.structuredContent is None, result.structuredContent
        env_text, mime = parse_env_resource(result)
        assert mime == "text/plain", mime
        assert f"GITHUB_TOKEN=ghs_mock_{EXPECTED_INSTALLATION_ID}" in env_text, env_text
        assert "# Expires at: 2099-01-01T00:00:00Z" in env_text, env_text
        assert "# Repository selection: all" in env_text, env_text
        assert "contents=" in env_text, env_text
        print("defaults ok ->", env_text.splitlines()[-1])

        print("--- github_app_installation_token (with scope) ---")
        result = await session.call_tool(
            "github_app_installation_token",
            {
                "repositories": ["homelab-k3s-mcp"],
                "permissions": {"contents": "read"},
            },
        )
        assert result.isError is False, result
        env_text, _ = parse_env_resource(result)
        assert f"GITHUB_TOKEN=ghs_mock_{EXPECTED_INSTALLATION_ID}" in env_text, env_text
        assert "# Repository selection: selected" in env_text, env_text
        assert "# Permissions: contents=read" in env_text, env_text
        print("scoped ok")


if __name__ == "__main__":
    asyncio.run(run())
