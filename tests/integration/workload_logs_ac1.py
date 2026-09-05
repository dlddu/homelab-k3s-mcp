"""Deployed-server e2e for workload-logs/AC1 (워크로드 기준 로그 조회).

검증 AC: workload-logs/AC1
실행 대상: primary

출력을 실제로 내는 ``crashloop-fixture`` 를 상대하며, 그 전제(재시작 1회 이상 ·
복구 인스턴스가 충분히 오래 떠 있음)를 멱등 폴링으로 스스로 성립시킨다.
Deployment 픽스처는 건드리지 않는다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _workload import CRASHLOOP_WORKLOAD, NAMESPACE, RECOVERED_MARKER, wait_for_crashloop_log_age, wait_for_crashloop_restart


async def test_workload_logs_ac1_logs_by_workload(session) -> None:
    """AC: workload-logs/AC1 — the workload's selector resolves to a pod and its logs come back.

    Runs against the crash-once fixture rather than the pause-image fixture:
    pause emits nothing, so reading it back proves selector resolution but not
    that log *content* is returned. The recovered instance prints a known
    marker at startup, so a non-empty body containing that marker is the AC's
    "최근 로그가 반환된다". A workload no selector can resolve comes back as a
    tool error instead of a crash.
    """
    wait_for_crashloop_restart()
    wait_for_crashloop_log_age(5.0)

    result = await session.call_tool(
        "workload_logs",
        {"kind": "Deployment", "namespace": NAMESPACE, "name": CRASHLOOP_WORKLOAD},
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload["pod"].startswith(f"{CRASHLOOP_WORKLOAD}-"), payload
    assert payload["previous"] is False, payload
    assert RECOVERED_MARKER in payload["logs"], payload["logs"]
    assert RECOVERED_MARKER in result.content[0].text, result.content[0].text
    print("workload_logs by-workload ok, pod:", payload["pod"])

    missing = await session.call_tool(
        "workload_logs",
        {"kind": "Deployment", "namespace": NAMESPACE, "name": "does-not-exist"},
    )
    assert missing.isError, missing
    print("workload_logs missing-workload rejection ok")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- workload-logs/AC1 ---")
        await test_workload_logs_ac1_logs_by_workload(session)
        print("ok: workload-logs/AC1")


if __name__ == "__main__":
    asyncio.run(run())
