"""Deployed-server e2e for grafana-token/AC1 (read-only, ~1h token).

검증 시나리오: test-grafana-token.md#시나리오 1
실행 대상: primary
병렬 레인: grafana
"""

from __future__ import annotations

import asyncio
import re
from datetime import datetime, timedelta, timezone

from mcp import ClientSession

from _helpers import base_url, open_session, parse_env_resource, wait_for_healthz


EXPIRES_RE = re.compile(r"^# token expires (?P<ts>\S+)$", re.MULTILINE)

# internal/grafana pins the TTL at one hour; allow generous slack for clock skew
# between this runner and the deployed pod plus the round trip.
TTL_LOWER = timedelta(minutes=50)

TTL_UPPER = timedelta(minutes=70)


async def test_grafana_token_ac1_read_only_short_lived(
    session: ClientSession,
) -> None:
    """AC: grafana-token/AC1 — a read-only token with a ~1 hour lifetime."""
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


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- grafana_token (AC: grafana-token/AC1) ---")
        await test_grafana_token_ac1_read_only_short_lived(session)
        print("ok: grafana-token/AC1")


if __name__ == "__main__":
    asyncio.run(run())
