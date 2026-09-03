"""Deployed-server e2e for pod-describe/AC2 (대상 지정 방식).

검증 AC: pod-describe/AC2
실행 대상: primary

세 targeting 경로가 **같은 파드**로 수렴하는지 보므로, 픽스처가 기준선(Ready 파드
정확히 하나)이어야 관측이 결정적이다.

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from mcp.shared.exceptions import McpError

from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE, WORKLOAD, ensure_workload_fixture_baseline


async def test_pod_describe_ac2_target_resolution(session) -> None:
    """AC: pod-describe/AC2 — name / selector / workload targeting resolves one pod.

    Verifies each single targeting mode resolves to a workload-fixture pod and
    that supplying two modes at once is rejected (mutually exclusive). Target
    argument errors come back as JSON-RPC errors, surfaced by the SDK as
    McpError rather than a tool result object.
    """
    by_selector = await session.call_tool(
        "pod_describe",
        {"namespace": NAMESPACE, "selector": f"app={WORKLOAD}"},
    )
    assert by_selector.isError is False, by_selector
    pod_name = by_selector.structuredContent["name"]
    assert pod_name.startswith(f"{WORKLOAD}-"), pod_name

    by_name = await session.call_tool(
        "pod_describe",
        {"namespace": NAMESPACE, "name": pod_name},
    )
    assert by_name.isError is False, by_name
    assert by_name.structuredContent["name"] == pod_name, by_name.structuredContent

    by_workload = await session.call_tool(
        "pod_describe",
        {
            "namespace": NAMESPACE,
            "workload_kind": "Deployment",
            "workload_name": WORKLOAD,
        },
    )
    assert by_workload.isError is False, by_workload
    assert by_workload.structuredContent["name"].startswith(f"{WORKLOAD}-"), (
        by_workload.structuredContent
    )

    try:
        await session.call_tool(
            "pod_describe",
            {
                "namespace": NAMESPACE,
                "name": pod_name,
                "selector": f"app={WORKLOAD}",
            },
        )
    except McpError as exc:
        assert "mutually exclusive" in str(exc), exc
        print("pod_describe mutual-exclusion rejection ok")
    else:
        raise AssertionError("expected McpError for name+selector both provided")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    ensure_workload_fixture_baseline()

    async with open_session(url) as session:
        print("--- pod-describe/AC2 ---")
        await test_pod_describe_ac2_target_resolution(session)
        print("ok: pod-describe/AC2")


if __name__ == "__main__":
    asyncio.run(run())
