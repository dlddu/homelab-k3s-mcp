"""Deployed-server e2e for workload-scale/AC1 (레플리카 설정).

검증 AC: workload-scale/AC1
실행 대상: primary

3 → 0 → 1 로 걷는 유일한 파일이다. 마지막 1 은 픽스처를 선언된 기준선으로
돌려놓기 위한 것이고, 그 복원에 의존하는 파일은 없다 — 픽스처를 읽는 파일은
각자 ``ensure_workload_fixture_baseline()`` 을 부른다.

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE, WORKLOAD, ensure_workload_fixture_baseline, kubectl_jsonpath, kubectl_wait_rollout, wait_for_status_replicas


async def test_workload_scale_ac1_replica_count(session) -> None:
    """AC: workload-scale/AC1 — spec.replicas is set to the requested value, zero included.

    Walks 3 -> 0 -> 1 so both the ordinary path and the AC's explicit
    "0으로의 스케일다운도 허용한다" clause are observed, checking spec.replicas
    on the cluster after each call rather than trusting the tool's echo. Ends
    back at 1 replica, and waits for it, to leave the fixture at the baseline
    tests/k8s/kind/test-deployment.yaml declares -- no other file depends on
    that restoration, because each file that reads the fixture calls
    ``ensure_workload_fixture_baseline()`` itself.
    """
    for replicas in (3, 0, 1):
        result = await session.call_tool(
            "workload_scale",
            {
                "kind": "Deployment",
                "namespace": NAMESPACE,
                "name": WORKLOAD,
                "replicas": replicas,
            },
        )
        assert result.isError is False, (replicas, result)
        assert result.structuredContent == {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": WORKLOAD,
            "replicas": replicas,
        }, result.structuredContent

        observed = kubectl_jsonpath("{.spec.replicas}")
        assert observed == str(replicas), (
            f"expected {replicas} replicas, got {observed!r}"
        )
        if replicas == 0:
            wait_for_status_replicas(0)
        else:
            kubectl_wait_rollout()
        print(f"workload_scale to {replicas} ok")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    ensure_workload_fixture_baseline()

    async with open_session(url) as session:
        print("--- workload-scale/AC1 ---")
        await test_workload_scale_ac1_replica_count(session)
        print("ok: workload-scale/AC1")


if __name__ == "__main__":
    asyncio.run(run())
