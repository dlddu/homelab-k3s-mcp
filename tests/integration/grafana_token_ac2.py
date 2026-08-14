"""Deployed-server e2e for grafana-token/AC2 (immediately usable .env).

검증 AC: grafana-token/AC2
실행 대상: primary

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _helpers import base_url, open_session, parse_env_resource, wait_for_healthz


# The query-config values the same fixture sets, echoed back in the .env so the
# caller can authenticate without looking anything else up.
EXPECTED_QUERY_CONFIG = {
    "GRAFANA_METRICS_URL": "https://prometheus-prod-ci.grafana.net/api/prom",
    "GRAFANA_METRICS_USER": "111111",
    "GRAFANA_LOGS_URL": "https://logs-prod-ci.grafana.net",
    "GRAFANA_LOGS_USER": "222222",
}


def parse_env_pairs(env_text: str) -> dict[str, str]:
    """Parse the KEY=VALUE lines of an .env payload, ignoring comments."""
    pairs = {}
    for line in env_text.splitlines():
        if line.startswith("#") or "=" not in line:
            continue
        key, _, value = line.partition("=")
        pairs[key] = value
    return pairs


async def test_grafana_token_ac2_ready_to_use_env(session: ClientSession) -> None:
    """AC: grafana-token/AC2 — the response is immediately usable for querying.

    The AC's verification method is that the returned URL/USER/TOKEN combination
    authenticates a metrics or logs query with no further lookups, so this
    asserts the payload carries a complete Basic-auth pair for both datasources:
    the metrics and logs endpoints with their instance-id usernames (echoed from
    server configuration), plus the single shared token that acts as the
    password for both. The payload is the text/plain .env the AC specifies.

    Issuing a real query against Grafana Cloud is out of reach of the kind
    fixture, so completeness of the credential set is what is asserted, not a
    live 200 from Mimir/Loki.
    """
    result = await session.call_tool("grafana_token", {})
    assert result.isError is False, result
    assert result.structuredContent is None, result.structuredContent

    env_text, mime = parse_env_resource(result)
    assert mime == "text/plain", mime

    pairs = parse_env_pairs(env_text)
    for key, expected in EXPECTED_QUERY_CONFIG.items():
        assert pairs.get(key) == expected, (
            f"{key} = {pairs.get(key)!r}, expected {expected!r} in:\n{env_text}"
        )
    # The shared Basic-auth password for both *_USER values above.
    assert pairs.get("GRAFANA_TOKEN", "").startswith("glc_mock_"), env_text


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- grafana_token (AC: grafana-token/AC2) ---")
        await test_grafana_token_ac2_ready_to_use_env(session)
        print("ok: grafana-token/AC2")


if __name__ == "__main__":
    asyncio.run(run())
