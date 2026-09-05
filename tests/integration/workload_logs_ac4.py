"""Deployed-server e2e for workload-logs/AC4 (컨테이너 선택과 필터).

검증 AC: workload-logs/AC4
실행 대상: primary

두 픽스처를 함께 쓴다 — 컨테이너 선택은 ``deploy/workload-fixture``(기준선을 선행
조건으로 성립시킨다), 필터의 출력 반영은 실제로 출력을 내는 ``crashloop-fixture``
(멱등 폴링으로 전제를 성립시킨다).

AC 문언의 「파드에 컨테이너가 둘 이상이면 ``container`` 가 필요하다」 절은 여전히
단정되지 않는다(케이스 docstring의 Not asserted 절 참조). 러닝 멀티컨테이너
워크로드가 필요하고, 기존 픽스처에 컨테이너를 더하는 경로는 ``workload_logs_ac2.py``
의 성공 단언을 깨뜨리므로 **별도 워크로드 신설**이 선행이다
(``docs/doc-tracker.md`` 의 e2e backlog에 등재).
"""

from __future__ import annotations

import asyncio
import re

from _helpers import base_url, open_session, wait_for_healthz
from _workload import CRASHLOOP_WORKLOAD, NAMESPACE, RECOVERED_MARKER, WORKLOAD, ensure_workload_fixture_baseline, wait_for_crashloop_log_age, wait_for_crashloop_restart


# A kubelet log line printed with timestamps=true is prefixed with an RFC3339
# instant and a single space.
LOG_TIMESTAMP_RE = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z ")


async def test_workload_logs_ac4_container_and_filters(session) -> None:
    """AC: workload-logs/AC4 — container selection and the timestamps/since_seconds filters.

    Asserts container selection is really applied (a name no container has is
    refused, the fixture's own container is accepted) and that the two filters
    change the *output*, not just the echoed request: timestamps=true prefixes
    every line with an RFC3339 instant, and a since_seconds window narrower
    than the recovered instance's age drops the startup marker the unfiltered
    call returns.

    Not asserted: the AC's "파드에 컨테이너가 둘 이상이면 container가 필요하다"
    clause. Every fixture pod in this deployment is single-container, so the
    multi-container refusal has no target here; it needs a running
    multi-container fixture (tracked in the doc-tracker e2e backlog).
    """
    accepted = await session.call_tool(
        "workload_logs",
        {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": WORKLOAD,
            "container": "pause",
            "tail_lines": 10,
            "timestamps": True,
            "since_seconds": 60,
        },
    )
    assert accepted.isError is False, accepted
    payload = accepted.structuredContent
    assert payload["container"] == "pause", payload
    assert payload["tailLines"] == 10, payload
    assert payload["timestamps"] is True, payload
    assert payload["sinceSeconds"] == 60, payload

    refused = await session.call_tool(
        "workload_logs",
        {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": WORKLOAD,
            "container": "no-such-container",
        },
    )
    assert refused.isError, refused
    print("workload_logs container selection ok")

    # Filters are observed on a workload that actually emits output.
    wait_for_crashloop_restart()
    wait_for_crashloop_log_age(5.0)

    stamped = await session.call_tool(
        "workload_logs",
        {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": CRASHLOOP_WORKLOAD,
            "timestamps": True,
        },
    )
    assert stamped.isError is False, stamped
    lines = [line for line in stamped.structuredContent["logs"].splitlines() if line]
    assert lines, stamped.structuredContent
    for line in lines:
        assert LOG_TIMESTAMP_RE.match(line), line
    assert any(RECOVERED_MARKER in line for line in lines), lines

    recent = await session.call_tool(
        "workload_logs",
        {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": CRASHLOOP_WORKLOAD,
            "since_seconds": 1,
        },
    )
    assert recent.isError is False, recent
    assert RECOVERED_MARKER not in recent.structuredContent["logs"], (
        recent.structuredContent
    )
    print("workload_logs timestamps/since_seconds ok")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    ensure_workload_fixture_baseline()

    async with open_session(url) as session:
        print("--- workload-logs/AC4 ---")
        await test_workload_logs_ac4_container_and_filters(session)
        print("ok: workload-logs/AC4")


if __name__ == "__main__":
    asyncio.run(run())
