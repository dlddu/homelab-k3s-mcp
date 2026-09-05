"""Deployed-server e2e for grafana-token/AC4 (issuer token not exposed).

검증 AC: grafana-token/AC4
실행 대상: primary

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _helpers import base_url, open_session, parse_env_resource, wait_for_healthz


# The CI kind fixture configures the server with this Grafana issuer credential
# (.github/workflows/ci.yml "Create test Grafana secret":
# GRAFANA_ISSUER_TOKEN=glsa_mock_issuer). The grafana-mock only ever returns the
# minted read token (token=glc_mock_<name>), so the issuer credential must never
# surface in the tool response.
ISSUER_TOKEN = "glsa_mock_issuer"


async def test_grafana_token_ac4_issuer_token_not_exposed(
    session: ClientSession,
) -> None:
    """AC: grafana-token/AC4 — the server-only issuer token is not exposed.

    Issues a grafana_token and asserts the response .env payload carries only the
    short-lived read token (GRAFANA_TOKEN=glc_mock_...) and never the issuer
    credential: neither the GRAFANA_ISSUER_TOKEN key, its configured value, nor
    the Grafana service-account token prefix (glsa_) appears in the output. This
    promotes the "발급자 토큰 비노출" guarantee to the deployed-server e2e layer.
    """
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
