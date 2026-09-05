"""Deployed-server e2e for workload-logs/AC2 (tail 라인 제어).

검증 AC: workload-logs/AC2
실행 대상: primary

기본값 응답을 ``deploy/workload-fixture`` 의 파드에서 읽으므로(로그를 내지 않는
``pause`` 컨테이너 → 빈 로그 본문) 픽스처 기준선을 선행 조건으로 성립시킨다.

이 파일이 ``container`` 없는 호출의 **성공**을 단정하기 때문에, 기존 픽스처에
컨테이너를 더해 AC4의 멀티컨테이너 절을 채우는 경로는 막혀 있다(별도 워크로드
신설이 선행 — ``docs/doc-tracker.md`` backlog 참조).
"""

from __future__ import annotations

import asyncio

from mcp.shared.exceptions import McpError

from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE, WORKLOAD, ensure_workload_fixture_baseline


async def test_workload_logs_ac2_tail_lines(session) -> None:
    """AC: workload-logs/AC2 — tailLines defaults to 200 and over-max requests are rejected.

    The omitted-argument call must report the documented default (200) rather
    than a server-side clamp, and 999999 must come back as a rejection. Input
    validation errors are JSON-RPC errors, which the SDK raises as McpError
    rather than returning as a tool result.
    """
    result = await session.call_tool(
        "workload_logs",
        {"kind": "Deployment", "namespace": NAMESPACE, "name": WORKLOAD},
    )
    assert result.isError is False, result
    payload = result.structuredContent
    pod_name = payload.pop("pod")
    assert pod_name.startswith(f"{WORKLOAD}-"), pod_name
    assert payload == {
        "kind": "Deployment",
        "namespace": NAMESPACE,
        "name": WORKLOAD,
        "container": None,
        "tailLines": 200,
        "previous": False,
        "timestamps": False,
        "sinceSeconds": None,
        "logs": "",
    }, payload
    assert result.content[0].text == "(no log output)", result.content[0].text
    print("workload_logs defaults ok, pod:", pod_name)

    try:
        await session.call_tool(
            "workload_logs",
            {
                "kind": "Deployment",
                "namespace": NAMESPACE,
                "name": WORKLOAD,
                "tail_lines": 999_999,
            },
        )
    except McpError as exc:
        assert "tail_lines" in str(exc), exc
        print("workload_logs tail_lines rejection ok")
    else:
        raise AssertionError("expected McpError for tail_lines over max")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    ensure_workload_fixture_baseline()

    async with open_session(url) as session:
        print("--- workload-logs/AC2 ---")
        await test_workload_logs_ac2_tail_lines(session)
        print("ok: workload-logs/AC2")


if __name__ == "__main__":
    asyncio.run(run())
