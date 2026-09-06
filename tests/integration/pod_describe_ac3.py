"""Deployed-server e2e for pod-describe/AC3 (이벤트 best-effort).

검증 AC: pod-describe/AC3
실행 대상: primary

셀렉터로 파드를 고르므로 픽스처 기준선을 선행 조건으로 성립시킨다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE, WORKLOAD, ensure_workload_fixture_baseline


async def test_pod_describe_ac3_events_best_effort(session) -> None:
    """AC: pod-describe/AC3 — the snapshot includes an events section best-effort.

    The server lists events best-effort and always returns an ``events`` array
    (empty when unavailable) without failing the describe call.
    """
    result = await session.call_tool(
        "pod_describe",
        {"namespace": NAMESPACE, "selector": f"app={WORKLOAD}"},
    )
    assert result.isError is False, result
    events = result.structuredContent["events"]
    assert isinstance(events, list), result.structuredContent
    print("pod_describe events best-effort ok, events:", len(events))


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    ensure_workload_fixture_baseline()

    async with open_session(url) as session:
        print("--- pod-describe/AC3 ---")
        await test_pod_describe_ac3_events_best_effort(session)
        print("ok: pod-describe/AC3")


if __name__ == "__main__":
    asyncio.run(run())
