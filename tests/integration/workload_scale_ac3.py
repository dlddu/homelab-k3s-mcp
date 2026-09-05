"""Deployed-server e2e for workload-scale/AC3 (파괴적 작업 표기).

검증 AC: workload-scale/AC3
실행 대상: primary

``tools/list`` 메타데이터만 읽고 스케일을 실행하지 않는다 — 클러스터 픽스처와
무관하므로 선행 조건이 없다.
"""

from __future__ import annotations

import asyncio

from _helpers import assert_destructive_annotation, base_url, open_session, wait_for_healthz


async def test_workload_scale_ac3_destructive_hint(session) -> None:
    """AC: workload-scale/AC3 — workload_scale advertises destructiveHint=true.

    Verifies the destructive-operation marking via tools/list metadata only; no
    scale is performed.
    """
    await assert_destructive_annotation(session, "workload_scale")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- workload-scale/AC3 ---")
        await test_workload_scale_ac3_destructive_hint(session)
        print("ok: workload-scale/AC3")


if __name__ == "__main__":
    asyncio.run(run())
