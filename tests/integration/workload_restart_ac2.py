"""Deployed-server e2e for workload-restart/AC2 (파괴적 작업 표기).

검증 AC: workload-restart/AC2
실행 대상: primary

``tools/list`` 메타데이터만 읽고 재시작을 실행하지 않는다 — 클러스터 픽스처와
무관하므로 선행 조건이 없다.
"""

from __future__ import annotations

import asyncio

from _helpers import assert_destructive_annotation, base_url, open_session, wait_for_healthz


async def test_workload_restart_ac2_destructive_hint(session) -> None:
    """AC: workload-restart/AC2 — workload_restart advertises destructiveHint=true.

    Verifies the destructive-operation marking via tools/list metadata only; no
    restart is triggered.
    """
    await assert_destructive_annotation(session, "workload_restart")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- workload-restart/AC2 ---")
        await test_workload_restart_ac2_destructive_hint(session)
        print("ok: workload-restart/AC2")


if __name__ == "__main__":
    asyncio.run(run())
