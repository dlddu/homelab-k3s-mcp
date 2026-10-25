"""pocket_aide_notification_send: APPROVAL_GATE_URL 미설정 시 graceful 거부 (e2e).

검증 시나리오: test-pocket-aide-notification-send.md#시나리오 4
실행 대상: auth-variant
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _auth_variant import (
    API_KEY,
    POCKET_AIDE_REFUSAL,
    assert_unavailable_refusal,
)
from _helpers import base_url, open_session, wait_for_healthz


async def test_pocket_aide_notification_send_ac4_unconfigured_refusal(
    session: ClientSession,
) -> None:
    """AC: pocket-aide-notification-send/AC4

    With APPROVAL_GATE_URL unset, pocket_aide_notification_send returns the
    unavailable error and the server keeps serving.
    """
    await assert_unavailable_refusal(
        session,
        "pocket_aide_notification_send",
        {"title": "no-config-probe"},
        POCKET_AIDE_REFUSAL,
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        print("--- unconfigured graceful refusal (AC: pocket-aide-notification-send/AC4) ---")
        await test_pocket_aide_notification_send_ac4_unconfigured_refusal(session)
        print("refusal ok: pocket-aide-notification-send/AC4")


if __name__ == "__main__":
    asyncio.run(run())
