"""Deployed-server e2e for dear-baby-reset-user/AC3 (destructive-operation marking).

검증 AC: dear-baby-reset-user/AC3
실행 대상: primary
"""

from __future__ import annotations

import asyncio

from _helpers import (
    assert_destructive_annotation,
    base_url,
    open_session,
    wait_for_healthz,
)


async def test_dear_baby_reset_user_ac3_destructive_hint(session) -> None:
    """AC: dear-baby-reset-user/AC3 — dear_baby_reset_user advertises destructiveHint=true.

    Verifies the destructive-operation marking via tools/list metadata only; no
    user reset is exec'd.
    """
    await assert_destructive_annotation(session, "dear_baby_reset_user")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- dear_baby_reset_user (AC: dear-baby-reset-user/AC3) ---")
        await test_dear_baby_reset_user_ac3_destructive_hint(session)
        print("ok: dear-baby-reset-user/AC3")


if __name__ == "__main__":
    asyncio.run(run())
