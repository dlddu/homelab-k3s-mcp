"""End-to-end checks for grafana_token against the mock."""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz


def parse_env_resource(result) -> tuple[str, str]:
    """Extract (env_text, mime_type) from a tool result's embedded resource."""
    assert result.content, result
    block = result.content[0]
    assert block.type == "resource", block
    resource = block.resource
    return resource.text, resource.mimeType


# The CI kind fixture configures the server with this Grafana issuer credential
# (.github/workflows/ci.yml "Create test Grafana secret":
# GRAFANA_ISSUER_TOKEN=glsa_mock_issuer). The grafana-mock only ever returns the
# minted read token (token=glc_mock_<name>), so the issuer credential must never
# surface in the tool response.
ISSUER_TOKEN = "glsa_mock_issuer"


async def test_grafana_token_ac4_issuer_token_not_exposed(session) -> None:
    """AC: grafana-token/AC4 — the server-only issuer token is not exposed.

    Issues a grafana_token and asserts the response .env payload carries only the
    short-lived read token (GRAFANA_TOKEN=glc_mock_...) and never the issuer
    credential: neither the GRAFANA_ISSUER_TOKEN key, its configured value, nor
    the Grafana service-account token prefix (glsa_) appears in the output. This
    promotes the "발급자 토큰 비노출" guarantee to the deployed-server e2e layer.
    """
    result = await session.call_tool("grafana_token", {})
    assert result.isError is False, result
    # The entire payload is the text/plain .env resource; nothing structured.
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
        print("--- grafana_token ---")
        result = await session.call_tool("grafana_token", {})
        assert result.isError is False, result
        assert result.structuredContent is None, result.structuredContent
        env_text, mime = parse_env_resource(result)
        assert mime == "text/plain", mime
        assert "# token expires" in env_text, env_text
        for key in (
            "GRAFANA_METRICS_URL=",
            "GRAFANA_METRICS_USER=",
            "GRAFANA_LOGS_URL=",
            "GRAFANA_LOGS_USER=",
            "GRAFANA_TOKEN=glc_mock_",
        ):
            assert key in env_text, f"missing {key!r} in:\n{env_text}"
        print("ok ->", sorted(line.split("=", 1)[0] for line in env_text.splitlines() if "=" in line))

        print("--- grafana_token issuer-token non-exposure (AC: grafana-token/AC4) ---")
        await test_grafana_token_ac4_issuer_token_not_exposed(session)
        print("grafana_token issuer non-exposure ok")


if __name__ == "__main__":
    asyncio.run(run())
