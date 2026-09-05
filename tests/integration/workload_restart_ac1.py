"""Deployed-server e2e for workload-restart/AC1 (롤링 재시작 트리거).

검증 AC: workload-restart/AC1
실행 대상: primary

픽스처를 실제로 변형한다(롤링 재시작). 재시작 전 ``metadata.generation`` 을 읽기
전에 기준선을 성립시켜, 다른 파일이 남긴 진행 중 롤아웃을 자기 세대 증가로
오독하지 않는다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE, WORKLOAD, ensure_workload_fixture_baseline, kubectl_jsonpath, kubectl_wait_rollout


RESTART_ANNOTATION_PATH = (
    r"{.spec.template.metadata.annotations.kubectl\.kubernetes\.io/restartedAt}"
)


async def test_workload_restart_ac1_rolling_restart(session) -> None:
    """AC: workload-restart/AC1 — a restart patches the workload instead of recreating it.

    Asserts the trigger annotation the rollout keys off is written, that a new
    rollout is actually started (metadata.generation advances and the rollout
    completes), and — the "재생성/삭제를 사용하지 않는다" half — that the
    Deployment object is the same one afterwards: a delete+create would mint a
    new metadata.uid and reset creationTimestamp.
    """
    uid_before = kubectl_jsonpath("{.metadata.uid}")
    created_before = kubectl_jsonpath("{.metadata.creationTimestamp}")
    generation_before = int(kubectl_jsonpath("{.metadata.generation}"))
    assert uid_before and created_before, (uid_before, created_before)

    result = await session.call_tool(
        "workload_restart",
        {"kind": "Deployment", "namespace": NAMESPACE, "name": WORKLOAD},
    )
    assert result.isError is False, result
    payload = result.structuredContent
    restarted_at = payload.pop("restartedAt")
    assert restarted_at, "restartedAt should be a non-empty timestamp"
    assert payload == {
        "kind": "Deployment",
        "namespace": NAMESPACE,
        "name": WORKLOAD,
    }, payload

    annotation = kubectl_jsonpath(RESTART_ANNOTATION_PATH)
    print("restartedAt annotation:", annotation)
    assert annotation, "restartedAt annotation missing on resource"
    assert annotation == restarted_at, (annotation, restarted_at)

    generation_after = int(kubectl_jsonpath("{.metadata.generation}"))
    assert generation_after > generation_before, (
        generation_before,
        generation_after,
    )
    assert kubectl_jsonpath("{.metadata.uid}") == uid_before, "workload was recreated"
    assert kubectl_jsonpath("{.metadata.creationTimestamp}") == created_before, (
        "workload was recreated"
    )
    kubectl_wait_rollout()
    print("workload_restart ok at", restarted_at)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    ensure_workload_fixture_baseline()

    async with open_session(url) as session:
        print("--- workload-restart/AC1 ---")
        await test_workload_restart_ac1_rolling_restart(session)
        print("ok: workload-restart/AC1")


if __name__ == "__main__":
    asyncio.run(run())
