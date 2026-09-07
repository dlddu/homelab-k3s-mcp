"""grafana_token: GRAFANA_ISSUER_TOKEN 미설정 시 graceful 거부 (e2e).

검증 AC: grafana-token/AC3
실행 대상: auth-variant
"""

from __future__ import annotations

import asyncio

from _auth_variant import (
    API_KEY,
    GRAFANA_REFUSAL,
    assert_unavailable_refusal,
)
from _helpers import base_url, open_session, wait_for_healthz


async def test_grafana_token_ac3_unconfigured_refusal(session: ClientSession) -> None:
    """AC: grafana-token/AC3

    With GRAFANA_ISSUER_TOKEN unset, grafana_token returns the unavailable
    error rather than minting or leaking anything.
    """
    await assert_unavailable_refusal(session, "grafana_token", {}, GRAFANA_REFUSAL)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        print("--- unconfigured graceful refusal (AC: grafana-token/AC3) ---")
        await test_grafana_token_ac3_unconfigured_refusal(session)
        print("refusal ok: grafana-token/AC3")


if __name__ == "__main__":
    asyncio.run(run())
