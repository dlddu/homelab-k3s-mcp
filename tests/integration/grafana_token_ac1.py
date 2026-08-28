"""Deployed-server e2e for grafana-token/AC1 (read-only, ~1h token).

검증 AC: grafana-token/AC1
실행 대상: primary

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
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


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- grafana_token (AC: grafana-token/AC1) ---")
        await test_grafana_token_ac1_read_only_short_lived(session)
        print("ok: grafana-token/AC1")


if __name__ == "__main__":
    asyncio.run(run())
