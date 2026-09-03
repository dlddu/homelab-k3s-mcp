"""Deployed-server e2e for workload-list/AC1 (종류별 워크로드 조회).

검증 AC: workload-list/AC1
실행 대상: primary

``deploy/workload-fixture`` 의 요약 필드를 정수로 단정하므로 픽스처가 기준선에
있어야 한다 — ``run()`` 이 ``ensure_workload_fixture_baseline()`` 으로 스스로
성립시킨다. 레플리카 **값**은 여전히 고정하지 않는다(같은 그룹의
``workload_scale_ac1.py`` 가 자기 프로세스에서 그 값을 움직인다).

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _workload import DS_WORKLOAD, NAMESPACE, STS_WORKLOAD, WORKLOAD, ensure_workload_fixture_baseline


async def test_workload_list_ac1_kinds_with_replica_summary(session) -> None:
    """AC: workload-list/AC1 — every kind enum returns its workloads with a replica summary.

    Calls workload_list once per enum member against the fixture namespace,
    which holds one object of each kind (test-deployment.yaml), and asserts
    both halves of the criterion: the listed object is the one of that kind,
    and each item carries the kind's own replica-summary fields as integers.
    The counts themselves are not pinned to a value because workload-scale/AC1
    moves the Deployment's replicas around in the same run.
    """
    expected_fields = {
        "Deployment": (
            WORKLOAD,
            ["replicas", "ready_replicas", "updated_replicas", "available_replicas"],
        ),
        "StatefulSet": (
            STS_WORKLOAD,
            ["replicas", "ready_replicas", "updated_replicas", "current_replicas"],
        ),
        "DaemonSet": (
            DS_WORKLOAD,
            [
                "desired_number_scheduled",
                "current_number_scheduled",
                "number_ready",
                "number_available",
                "updated_number_scheduled",
            ],
        ),
    }

    for kind, (fixture, fields) in expected_fields.items():
        result = await session.call_tool(
            "workload_list", {"kind": kind, "namespace": NAMESPACE}
        )
        assert result.isError is False, (kind, result)
        payload = result.structuredContent
        assert payload["kind"] == kind, payload
        items = payload["items"]
        names = [item["name"] for item in items]
        assert fixture in names, (kind, names)

        item = next(i for i in items if i["name"] == fixture)
        for field in fields:
            assert field in item, (kind, field, item)
            assert isinstance(item[field], int), (kind, field, item)
        # A kind's summary must not carry another kind's shape.
        for other_kind, (_, other_fields) in expected_fields.items():
            if other_kind == kind:
                continue
            for field in set(other_fields) - set(fields):
                assert field not in item, (kind, other_kind, field, item)
        print(f"workload_list {kind} ok:", names)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    ensure_workload_fixture_baseline()

    async with open_session(url) as session:
        print("--- workload-list/AC1 ---")
        await test_workload_list_ac1_kinds_with_replica_summary(session)
        print("ok: workload-list/AC1")


if __name__ == "__main__":
    asyncio.run(run())
