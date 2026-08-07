"""End-to-end checks for grafana_token against the mock.

Per-AC case names + docstrings declare the AC they verify (registry rule 3);
``docs/doc-tracker.md`` is the AC<->case mapping SSOT.
"""

from __future__ import annotations

import asyncio
import re
from datetime import datetime, timedelta, timezone

from mcp import ClientSession

from _helpers import base_url, open_session, wait_for_healthz

# The CI kind fixture configures the server with this Grafana issuer credential
# (.github/workflows/ci.yml "Create test Grafana secret":
# GRAFANA_ISSUER_TOKEN=glsa_mock_issuer). The grafana-mock only ever returns the
# minted read token (token=glc_mock_<name>), so the issuer credential must never
# surface in the tool response.
ISSUER_TOKEN = "glsa_mock_issuer"

# The query-config values the same fixture sets, echoed back in the .env so the
# caller can authenticate without looking anything else up.
EXPECTED_QUERY_CONFIG = {
    "GRAFANA_METRICS_URL": "https://prometheus-prod-ci.grafana.net/api/prom",
    "GRAFANA_METRICS_USER": "111111",
    "GRAFANA_LOGS_URL": "https://logs-prod-ci.grafana.net",
    "GRAFANA_LOGS_USER": "222222",
}

EXPIRES_RE = re.compile(r"^# token expires (?P<ts>\S+)$", re.MULTILINE)

# internal/grafana pins the TTL at one hour; allow generous slack for clock skew
# between this runner and the deployed pod plus the round trip.
TTL_LOWER = timedelta(minutes=50)
TTL_UPPER = timedelta(minutes=70)


def parse_env_resource(result) -> tuple[str, str]:
    """Extract (env_text, mime_type) from a tool result's embedded resource."""
    assert result.content, result
    block = result.content[0]
    assert block.type == "resource", block
    resource = block.resource
    return resource.text, resource.mimeType


def parse_env_pairs(env_text: str) -> dict[str, str]:
    """Parse the KEY=VALUE lines of an .env payload, ignoring comments."""
    pairs = {}
    for line in env_text.splitlines():
        if line.startswith("#") or "=" not in line:
            continue
        key, _, value = line.partition("=")
        pairs[key] = value
    return pairs


async def test_grafana_token_ac1_read_only_short_lived(
    session: ClientSession,
) -> None:
    """AC: grafana-token/AC1 — a read-only token with a ~1 hour lifetime.

    The server fixes both the scope and the TTL: it mints against the configured
    read access policy (GRAFANA_READ_POLICY_ID) and asks for an expiry one hour
    out, and the mock echoes back the ``expiresAt`` it was handed. That makes the
    lifetime genuinely observable at this layer, unlike the GitHub mock's fixed
    stub expiry — so this parses the ``# token expires <RFC3339>`` comment and
    asserts it lands in a 50-70 minute window from now.

    Read-only scope is asserted as far as the deployed surface allows: the tool
    takes no scope input at all (the policy id is server-side configuration), and
    what comes back is the minted policy token ``glc_mock_...``. Which grants
    that policy actually carries is Grafana Cloud's side of the contract and is
    not reachable from the mock.
    """
    result = await session.call_tool("grafana_token", {})
    assert result.isError is False, result

    env_text, mime = parse_env_resource(result)
    assert mime == "text/plain", mime
    assert "GRAFANA_TOKEN=glc_mock_" in env_text, env_text

    match = EXPIRES_RE.search(env_text)
    assert match is not None, f"no '# token expires' comment in:\n{env_text}"
    expires_at = datetime.fromisoformat(match.group("ts").replace("Z", "+00:00"))
    ttl = expires_at - datetime.now(timezone.utc)
    assert TTL_LOWER < ttl < TTL_UPPER, (
        f"token TTL = {ttl}, expected roughly one hour "
        f"(expires at {match.group('ts')})"
    )


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
        for label, case in (
            ("grafana-token/AC1", test_grafana_token_ac1_read_only_short_lived),
            ("grafana-token/AC2", test_grafana_token_ac2_ready_to_use_env),
            ("grafana-token/AC4", test_grafana_token_ac4_issuer_token_not_exposed),
        ):
            print(f"--- grafana_token (AC: {label}) ---")
            await case(session)
            print(f"ok: {label}")


if __name__ == "__main__":
    asyncio.run(run())
