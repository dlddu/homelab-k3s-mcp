"""Deployed-server e2e for workload-scale/AC2 (DaemonSet 거부).

검증 AC: workload-scale/AC2
실행 대상: primary

거부 대상은 DaemonSet ``workload-fixture-ds`` 이고 Deployment 픽스처의 레플리카
상태와 무관하다 — 선행 조건이 없다.

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _workload import DS_WORKLOAD, NAMESPACE


async def test_workload_scale_ac2_daemonset_rejected(session) -> None:
    """AC: workload-scale/AC2 — DaemonSet is refused because the kind has no replicas.

    Targets the DaemonSet that really exists in the fixture namespace, so the
    refusal is observably about the *kind* rather than about a missing object,
    and checks the tool advertises the same restriction in its input schema
    (kind enum excludes DaemonSet).
    """
    tools = await session.list_tools()
    scale = next(tool for tool in tools.tools if tool.name == "workload_scale")
    kind_enum = scale.inputSchema["properties"]["kind"]["enum"]
    assert "DaemonSet" not in kind_enum, kind_enum
    assert {"Deployment", "StatefulSet"} <= set(kind_enum), kind_enum

    result = await session.call_tool(
        "workload_scale",
        {
            "kind": "DaemonSet",
            "namespace": NAMESPACE,
            "name": DS_WORKLOAD,
            "replicas": 1,
        },
    )
    assert result.isError, result
    text = result.content[0].text
    assert "DaemonSet does not have replicas" in text, text
    print("workload_scale daemonset rejection ok")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- workload-scale/AC2 ---")
        await test_workload_scale_ac2_daemonset_rejected(session)
        print("ok: workload-scale/AC2")


if __name__ == "__main__":
    asyncio.run(run())
