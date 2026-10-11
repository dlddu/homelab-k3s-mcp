"""Deployed-server e2e for grafana-token/AC4 (issuer token not exposed).

검증 시나리오: test-grafana-token.md#시나리오 4
실행 대상: primary
병렬 레인: grafana
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _helpers import base_url, open_session, parse_env_resource, wait_for_healthz


# The CI kind fixture configures the server with this Grafana issuer credential
# (.github/workflows/ci.yml "Create test Grafana secret":
# GRAFANA_ISSUER_TOKEN=glsa_mock_issuer).
ISSUER_TOKEN = "glsa_mock_issuer"


async def test_grafana_token_ac4_issuer_token_not_exposed(
    session: ClientSession,
) -> None:
    """AC: grafana-token/AC4 — the server-only issuer token is not exposed."""
    result = await session.call_tool("grafana_token", {})
    assert result.isError is False, result
    assert result.structuredContent is None, result.structuredContent
    env_text, _ = parse_env_resource(result)
    # The minted read token IS returned (that is the whole point of the tool)...
    assert "GRAFANA_TOKEN=glc_mock_" in env_text, env_text
    # ...but the issuer credential must not leak in any form.
    assert "GRAFANA_ISSUER_TOKEN" not in env_text, env_text
    assert ISSUER_TOKEN not in env_text, env_text
    assert "glsa_" not in env_text, env_text


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- grafana_token (AC: grafana-token/AC4) ---")
        await test_grafana_token_ac4_issuer_token_not_exposed(session)
        print("ok: grafana-token/AC4")


if __name__ == "__main__":
    asyncio.run(run())
