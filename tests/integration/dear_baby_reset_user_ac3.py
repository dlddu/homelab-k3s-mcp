"""Deployed-server e2e for dear-baby-reset-user/AC3 (destructive-operation marking).

검증 AC: dear-baby-reset-user/AC3
실행 대상: primary

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
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
