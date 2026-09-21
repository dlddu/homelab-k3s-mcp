"""결과별 카운터와 미호출 도구의 0 — 세 결과가 각자의 계열에 하나씩 실리고, 부르지 않은 도구는 0 으로 **있다**.

검증 시나리오: test-metrics.md#시나리오 1
실행 대상: primary
실행 순서: 2

성공·거부·에러는 한 도구에서 갈리지 않는다 — ``ping`` 은 성공만 하고, ``resource_get`` 은 좌표가
비면 디스패처가 핸들러 앞에서 끊어 **거부**(``invalid_input``), 좌표가 갖춰졌는데 객체가 없으면
핸들러가 apiserver 의 404 를 돌려 **에러**다(``internal/mcp/mcp.go::outcome`` — JSON-RPC 오류는 늘
거부, 핸들러가 돌고 ``isError`` 면 에러). 그래서 셋을 두 도구로 만들고, 각 (도구, 결과) 계열의
차가 1 이며 같은 도구의 **다른** 결과 계열은 0 인지로 「섞이지 않는다」를 잰다.

「한 번도 호출되지 않은 등록 도구도 0 으로 노출된다」는 그런 도구가 있는 배포에서만 관측된다.
레인들이 다 돈 뒤의 주 배포는 등록 도구 거의 전부를 이미 불렀으므로, 이 파일은 두 스모크 바로
뒤의 장벽 자리(``실행 순서: 2``)에서 돈다 — 그래도 미호출 집합은 **가정하지 않고 파드 stdout 의
레코드에서 계산한다**(레코드에 한 번도 나오지 않은 등록 도구). 그 집합이 비어 있지 않고, 그
도구들의 세 결과 계열이 모두 값 0 으로 **존재**하는 것이 「계열 부재와 0 의 구별」이다. 등록 도구
전집은 ``_helpers.EXPECTED_TOOLS`` 로, 서버의 ``toolslist.go`` 와 같은 표면이다.
"""

from __future__ import annotations

import asyncio

from mcp.shared.exceptions import McpError

from _eventlog import parse, records
from _helpers import EXPECTED_TOOLS, base_url, open_session, wait_for_healthz
from _metrics import Snapshot, delta, metrics_url, scrape

SERVER_NAMESPACE = "homelab-k3s-mcp"
SERVER_DEPLOYMENT = "homelab-k3s-mcp"
METRICS_LOCAL_PORT = 19090

NAMESPACE = "workload-test"
TARGET_MISSING = "mt-s1-does-not-exist"

RESULTS = ("success", "refused", "error")
CALLS = "mcp_tool_calls_total"


async def test_success_refused_error_land_on_their_own_series(session, url: str) -> None:
    before = scrape(url)

    pong = await session.call_tool("ping", {})
    assert pong.isError is False and pong.content[0].text == "pong", pong

    try:
        await session.call_tool(
            "resource_get", {"apiVersion": "v1", "kind": "", "namespace": NAMESPACE, "name": TARGET_MISSING}
        )
    except McpError:
        pass
    else:
        raise AssertionError("kind 없는 좌표가 거부되지 않았다")

    missing = await session.call_tool(
        "resource_get", {"apiVersion": "v1", "kind": "ConfigMap", "namespace": NAMESPACE, "name": TARGET_MISSING}
    )
    assert missing.isError is True, f"없는 객체의 조회가 에러가 아니다: {missing}"

    after = scrape(url)
    moved = {
        (tool, result): delta(before, after, CALLS, tool=tool, result=result)
        for tool in ("ping", "resource_get")
        for result in RESULTS
    }
    assert moved[("ping", "success")] == 1, moved
    assert moved[("resource_get", "refused")] == 1, moved
    assert moved[("resource_get", "error")] == 1, moved
    others = {k: v for k, v in moved.items() if k not in {("ping", "success"), ("resource_get", "refused"), ("resource_get", "error")}}
    assert all(v == 0 for v in others.values()), f"결과가 다른 계열에 섞였다: {others}"


def test_uncalled_tools_are_exposed_at_zero(snapshot: Snapshot) -> None:
    called = {parse(line).get("tool") for line in records(SERVER_NAMESPACE, SERVER_DEPLOYMENT)}
    uncalled = sorted(EXPECTED_TOOLS - called)
    assert uncalled, (
        "이 배포는 등록 도구를 전부 이미 불렀다 — 「미호출 도구의 0」을 관측할 도구가 없다 "
        f"(레코드의 도구: {sorted(called)})"
    )
    for tool in uncalled:
        for result in RESULTS:
            assert snapshot.has(CALLS, tool=tool, result=result), (
                f"미호출 도구 {tool} 의 {result} 계열이 노출되지 않았다 — 부재와 0 이 구별되지 않는다"
            )
            assert snapshot.value(CALLS, tool=tool, result=result) == 0, (tool, result)
    print(f"    미호출 등록 도구 {len(uncalled)}개가 0 으로 노출됨: {uncalled}")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    with metrics_url(SERVER_NAMESPACE, SERVER_DEPLOYMENT, METRICS_LOCAL_PORT) as metrics:
        async with open_session(url) as session:
            print("--- metrics/시나리오 1 (성공·거부·에러 각 1, 섞이지 않음) ---")
            await test_success_refused_error_land_on_their_own_series(session, metrics)
        print("--- metrics/시나리오 1 (미호출 등록 도구의 0) ---")
        test_uncalled_tools_are_exposed_at_zero(scrape(metrics))
    print("ok: test-metrics.md#시나리오 1")


if __name__ == "__main__":
    asyncio.run(run())
