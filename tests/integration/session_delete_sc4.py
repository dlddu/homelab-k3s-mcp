"""session_delete: SESSION_PLATFORM_ENDPOINT 미설정 시 graceful 거부 (e2e).

검증 시나리오: test-session-delete.md#시나리오 4
실행 대상: auth-variant
"""

from __future__ import annotations

import asyncio

from _auth_variant import (
    API_KEY,
    SESSION_PLATFORM_REFUSAL,
    assert_unavailable_refusal,
)
from _helpers import base_url, open_session, wait_for_healthz


async def test_session_delete_sc4_unconfigured_refusal(session) -> None:
    await assert_unavailable_refusal(
        session,
        "session_delete",
        {"id": "e2e-delete-sc4-unconfigured"},
        SESSION_PLATFORM_REFUSAL,
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        print("--- session-delete/시나리오 4 ---")
        await test_session_delete_sc4_unconfigured_refusal(session)
        print("ok: test-session-delete.md#시나리오 4")


if __name__ == "__main__":
    asyncio.run(run())
