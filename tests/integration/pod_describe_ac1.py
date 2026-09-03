"""Deployed-server e2e for pod-describe/AC1 (파드 상세 스냅샷).

검증 AC: pod-describe/AC1
실행 대상: primary

셀렉터로 파드 **하나**를 고른 뒤 Running·ready 를 단정하므로, 선행 조건은
「Ready 파드가 정확히 하나」다 — ``ensure_workload_fixture_baseline()`` 이 그
조건까지 기다린다.

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE, WORKLOAD, ensure_workload_fixture_baseline


async def test_pod_describe_ac1_snapshot(session) -> None:
    """AC: pod-describe/AC1 — pod_describe returns a structured pod snapshot.

    Describes the running workload-fixture pod (resolved by label selector so the
    case is independent of the generated pod name) and asserts the snapshot
    carries pod metadata plus per-container state (state / ready / restart count),
    conditions, and the kubectl-describe-style rendered text.
    """
    result = await session.call_tool(
        "pod_describe",
        {"namespace": NAMESPACE, "selector": f"app={WORKLOAD}"},
    )
    assert result.isError is False, result
    snapshot = result.structuredContent
    assert snapshot["namespace"] == NAMESPACE, snapshot
    assert snapshot["name"].startswith(f"{WORKLOAD}-"), snapshot
    assert snapshot["phase"] == "Running", snapshot
    pause = next(
        (c for c in snapshot["containers"] if c["name"] == "pause"), None
    )
    assert pause is not None, snapshot["containers"]
    assert "pause" in pause["image"], pause
    assert pause["ready"] is True, pause
    assert pause["restart_count"] == 0, pause
    assert pause["state"] == "running", pause
    assert isinstance(snapshot["conditions"], list) and snapshot["conditions"], (
        snapshot
    )
    text = result.content[0].text
    assert "Name:" in text and NAMESPACE in text, text
    print("pod_describe snapshot ok, pod:", snapshot["name"])


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    ensure_workload_fixture_baseline()

    async with open_session(url) as session:
        print("--- pod-describe/AC1 ---")
        await test_pod_describe_ac1_snapshot(session)
        print("ok: pod-describe/AC1")


if __name__ == "__main__":
    asyncio.run(run())
