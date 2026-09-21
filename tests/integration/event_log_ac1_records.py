"""호출당 한 레코드, 다섯 필드 — 부류가 다른 도구 셋이 각각 정확히 한 줄을 남기고, 그 줄 수가 카운터 증가분과 같다.

검증 시나리오: test-event-log.md#시나리오 1
실행 대상: primary

세 도구는 시나리오가 고른 그대로다 — 좌표를 쓰는 ``resource_get``, 좌표가 없는 ``ping``, 외부
시스템을 쓰는 ``grafana_token``(주 배포는 grafana-mock 이 배선돼 성공한다). ``resource_get`` 의
대상은 모든 네임스페이스에 apiserver 가 넣어 두는 ``kube-root-ca.crt`` ConfigMap 이라 픽스처가
필요 없다. 다섯 필드는 레코드 형식(``internal/eventlog/eventlog.go::Attrs``)의 ``time`` · ``tool`` ·
``principal`` · ``target.*`` · ``result`` 이고, 「좌표가 없는 도구는 대상 필드가 비어 있되 **필드
자체는 존재**한다」는 ``_eventlog.parse`` 가 ``key=""`` 를 빈 문자열로 보존하기 때문에 키의 존재와
값의 공백을 따로 잰다.

기대 결과의 등식 절 — 「같은 구간의 레코드 수가 ``mcp_tool_calls_total`` 증가분과 같다」 — 은 두
층을 같은 구간에서 읽어 잰다: 호출 전후의 ``msg="tool call"`` 줄 수 차와, 같은 전후의 카운터
전 계열 합의 차. 둘 다 3 이어야 하고 서로 같아야 한다. 레인을 선언하지 않으므로 레인들이 끝난
뒤 단독으로 돌고, 그 구간에 남의 호출은 없다 — 등식이 이 파일의 세 호출만을 센다는 성립 조건이다.
"""

from __future__ import annotations

import asyncio

from _eventlog import exactly_one_new, parse, records, select, wait_for_new
from _helpers import EXPECTED_TOOLS, base_url, open_session, wait_for_healthz
from _metrics import Snapshot, metrics_url, scrape

SERVER_NAMESPACE = "homelab-k3s-mcp"
SERVER_DEPLOYMENT = "homelab-k3s-mcp"
METRICS_LOCAL_PORT = 19090

NAMESPACE = "workload-test"
TARGET = "kube-root-ca.crt"

FIELDS = ("time", "tool", "principal", "target.apiVersion", "target.kind", "target.namespace", "target.name", "result")
TARGET_FIELDS = ("target.apiVersion", "target.kind", "target.namespace", "target.name")
CALLS = "mcp_tool_calls_total"
RESULTS = ("success", "refused", "error")


def _count(**wanted: str) -> int:
    return len(select(records(SERVER_NAMESPACE, SERVER_DEPLOYMENT), **wanted))


def _all_calls(snapshot: Snapshot) -> float:
    return sum(
        snapshot.value(CALLS, tool=tool, result=result)
        for tool in sorted(EXPECTED_TOOLS) + ["unregistered"]
        for result in RESULTS
    )


def _one_record(before: int, **wanted: str) -> dict[str, str]:
    found = wait_for_new(SERVER_NAMESPACE, SERVER_DEPLOYMENT, before, **wanted)
    record = exactly_one_new(found, before, str(wanted))
    for field in FIELDS:
        assert field in record, f"{wanted} 레코드에 {field} 가 없다: {record}"
    assert record["principal"] != "", record
    assert record["result"] == "success", record
    return record


async def test_a_coordinate_tool_leaves_one_record_with_its_target(session) -> None:
    wanted = {"tool": "resource_get", "target.name": TARGET}
    before = _count(**wanted)
    result = await session.call_tool(
        "resource_get", {"apiVersion": "v1", "kind": "ConfigMap", "namespace": NAMESPACE, "name": TARGET}
    )
    assert result.isError is False, result
    record = _one_record(before, **wanted)
    assert record["target.kind"] == "ConfigMap" and record["target.namespace"] == NAMESPACE, record


async def test_a_coordinate_free_tool_has_empty_target_fields_that_exist(session) -> None:
    wanted = {"tool": "ping"}
    before = _count(**wanted)
    pong = await session.call_tool("ping", {})
    assert pong.isError is False and pong.content[0].text == "pong", pong
    record = _one_record(before, **wanted)
    for field in TARGET_FIELDS:
        assert record[field] == "", f"좌표 없는 도구의 {field} 가 비어 있지 않다: {record}"


async def test_an_external_tool_leaves_one_record(session) -> None:
    wanted = {"tool": "grafana_token"}
    before = _count(**wanted)
    result = await session.call_tool("grafana_token", {})
    assert result.isError is False, result
    _one_record(before, **wanted)


def test_record_count_equals_the_counter_increase(records_before: int, before: Snapshot, metrics: str) -> None:
    lines = len(records(SERVER_NAMESPACE, SERVER_DEPLOYMENT)) - records_before
    counted = _all_calls(scrape(metrics)) - _all_calls(before)
    assert lines == 3, f"구간의 레코드가 {lines}줄 — 호출 셋에 셋이어야 한다"
    assert counted == lines, f"레코드 {lines}줄 != 카운터 증가분 {counted} — 집계의 기준이 둘이 됐다"


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    with metrics_url(SERVER_NAMESPACE, SERVER_DEPLOYMENT, METRICS_LOCAL_PORT) as metrics:
        records_before = len(records(SERVER_NAMESPACE, SERVER_DEPLOYMENT))
        counters_before = scrape(metrics)
        async with open_session(url) as session:
            print("--- event-log/시나리오 1 (좌표 도구 resource_get) ---")
            await test_a_coordinate_tool_leaves_one_record_with_its_target(session)
            print("--- event-log/시나리오 1 (무좌표 도구 ping — 대상 필드는 비어 있되 존재) ---")
            await test_a_coordinate_free_tool_has_empty_target_fields_that_exist(session)
            print("--- event-log/시나리오 1 (외부 도구 grafana_token) ---")
            await test_an_external_tool_leaves_one_record(session)
        print("--- event-log/시나리오 1 (레코드 수 == 카운터 증가분) ---")
        test_record_count_equals_the_counter_increase(records_before, counters_before, metrics)
    print("ok: test-event-log.md#시나리오 1")


if __name__ == "__main__":
    asyncio.run(run())
