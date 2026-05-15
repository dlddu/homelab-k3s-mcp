"""End-to-end checks for github_app_installation_token against the mock."""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz

# Must match GITHUB_APP_INSTALLATION_ID in tests/k8s/kind/github-app-patch.yaml,
# which is the installation id the mock embeds in the issued token.
EXPECTED_INSTALLATION_ID = "67890"


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github_app_installation_token (defaults) ---")
        result = await session.call_tool("github_app_installation_token", {})
        assert result.isError is False, result
        payload = result.structuredContent
        assert payload["token"] == f"ghs_mock_{EXPECTED_INSTALLATION_ID}", payload
        assert payload["expires_at"] == "2099-01-01T00:00:00Z", payload
        assert payload["repository_selection"] == "all", payload
        assert "contents" in payload["permissions"], payload
        print("defaults ok ->", payload["token"])

        print("--- github_app_installation_token (with scope) ---")
        result = await session.call_tool(
            "github_app_installation_token",
            {
                "repositories": ["homelab-k3s-mcp"],
                "permissions": {"contents": "read"},
            },
        )
        assert result.isError is False, result
        payload = result.structuredContent
        assert payload["token"] == f"ghs_mock_{EXPECTED_INSTALLATION_ID}", payload
        assert payload["repository_selection"] == "selected", payload
        assert payload["permissions"] == {"contents": "read"}, payload
        print("scoped ok")


if __name__ == "__main__":
    asyncio.run(run())
