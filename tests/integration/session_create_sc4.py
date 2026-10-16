"""session_create: SESSION_PLATFORM_ENDPOINT 미설정 시 graceful 거부 (e2e).

검증 시나리오: test-session-create.md#시나리오 4
실행 대상: auth-variant

인자는 유효하게 준다 — 이름이 비면 서비스에 닿기 전에 ``-32602`` 로 거부되므로, 이 파일이
미설정 거부가 아니라 인자 검증을 단정하게 된다.
"""

from __future__ import annotations

import asyncio

from _auth_variant import (
    API_KEY,
    SESSION_PLATFORM_REFUSAL,
    assert_unavailable_refusal,
)
from _helpers import base_url, open_session, wait_for_healthz


async def test_session_create_sc4_unconfigured_refusal(session) -> None:
    await assert_unavailable_refusal(
        session,
        "session_create",
        {"name": "e2e create sc4 unconfigured"},
        SESSION_PLATFORM_REFUSAL,
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        print("--- session-create/시나리오 4 ---")
        await test_session_create_sc4_unconfigured_refusal(session)
        print("ok: test-session-create.md#시나리오 4")


if __name__ == "__main__":
    asyncio.run(run())
