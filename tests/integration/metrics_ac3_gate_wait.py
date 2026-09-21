"""게이트 대기와 처리 지연의 분리 — 승인이 수 초 걸려도 처리 지연 계열은 그만큼 커지지 않는다.

검증 시나리오: test-metrics.md#시나리오 3
실행 대상: primary

「승인이 수 초 걸리는 게이트 대상 도구」는 지연을 주입하는 픽스처가 아니라 **승인자의 지연**으로
만든다. 실물 gatekeeper 앞에서 도구 호출은 판정이 날 때까지 블록하고, 판정은 이 파일이 사람
대신 ``PATCH …/approve`` 로 내리므로(``_gatekeeper.decide``), 승인 요청이 보인 뒤 ``GATE_DELAY``
초를 기다렸다가 승인하면 그 시간이 곧 게이트 대기다 — 서버는 그 대기를 ``askGate`` 에서 따로
재어 핸들러 시간에서 뺀다(``internal/mcp/mcp.go``). 픽스처를 바꿀 것도, 모킹 정책에 등재할
것도 없다. 대조군은 ``ping`` — 게이트를 타지 않아 대기 계열에 실리지 않고 처리 지연만 남긴다.

단언은 두 히스토그램의 ``_sum``·``_count`` 차로 한다: ``mcp_gate_wait_seconds{decision="approved"}``
는 한 번 관측되고 그 합이 ``GATE_DELAY`` 이상, ``mcp_tool_duration_seconds{tool="resource_patch"}``
는 한 번 관측되고 그 합이 대기의 절반에도 못 미친다(kind 의 ConfigMap 패치는 ms 단위다).
``ping`` 은 처리 지연에 한 번 실리고 대기 계열은 어느 판정도 움직이지 않는다. 지연은 주 배포의
게이트 타임아웃(기본 300초)에 한참 못 미치고, 폴링 간격(기본 2초)이 대기에 더해질 뿐이다.

레인을 선언하지 않으므로 레인들이 끝난 뒤 단독으로 돈다 — 그 사이 남이 ``ping`` 이나 게이트
대상 도구를 부르지 않아 두 도구의 계열 차가 곧 이 파일의 호출이다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from _eventlog import exactly_one_new, records, select, wait_for_new
from _gatekeeper import decide, gatekeeper_url, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz
from _metrics import delta, metrics_url, scrape

SERVER_NAMESPACE = "homelab-k3s-mcp"
SERVER_DEPLOYMENT = "homelab-k3s-mcp"
METRICS_LOCAL_PORT = 19090

NAMESPACE = "workload-test"
TARGET_GATED = "mt-s3-gated"
TOOL = "resource_patch"

GATE_DELAY = 3.0

GATE_WAIT = "mcp_gate_wait_seconds"
DURATION = "mcp_tool_duration_seconds"
DECISIONS = ("approved", "rejected", "expired", "timeout", "unreachable", "unconfigured")


def _config_map(name: str) -> None:
    manifest = {
        "apiVersion": "v1",
        "kind": "ConfigMap",
        "metadata": {"name": name, "namespace": NAMESPACE},
        "data": {"key": "one"},
    }
    subprocess.run(["kubectl", "apply", "-f", "-"], input=json.dumps(manifest), text=True, check=True, capture_output=True)


def _delete_config_map(name: str) -> None:
    subprocess.run(
        ["kubectl", "-n", NAMESPACE, "delete", "configmap", name, "--ignore-not-found", "--wait=false"],
        check=False,
        capture_output=True,
    )


async def test_an_immediate_tool_is_only_a_duration(session, metrics: str) -> None:
    before = scrape(metrics)
    pong = await session.call_tool("ping", {})
    assert pong.isError is False and pong.content[0].text == "pong", pong
    after = scrape(metrics)

    assert delta(before, after, f"{DURATION}_count", tool="ping") == 1, after.text
    assert delta(before, after, f"{DURATION}_sum", tool="ping") < 1.0, after.text
    for decision in DECISIONS:
        assert delta(before, after, f"{GATE_WAIT}_count", decision=decision) == 0, (
            f"게이트를 타지 않는 ping 이 대기 계열({decision})에 실렸다"
        )


async def test_a_slow_approval_lands_on_the_wait_series_not_the_duration(session, gate: str, metrics: str) -> None:
    before = scrape(metrics)
    records_before = len(select(records(SERVER_NAMESPACE, SERVER_DEPLOYMENT), tool=TOOL, **{"target.name": TARGET_GATED}))

    task = asyncio.create_task(
        session.call_tool(
            TOOL,
            {
                "apiVersion": "v1",
                "kind": "ConfigMap",
                "namespace": NAMESPACE,
                "name": TARGET_GATED,
                "patchType": "merge",
                "patch": {"data": {"key": TARGET_GATED}},
            },
        )
    )
    row = await wait_for_pending(gate, TARGET_GATED)
    await asyncio.sleep(GATE_DELAY)
    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result

    found = wait_for_new(SERVER_NAMESPACE, SERVER_DEPLOYMENT, records_before, tool=TOOL, **{"target.name": TARGET_GATED})
    record = exactly_one_new(found, records_before, TARGET_GATED)
    assert record["gate.decision"] == "approved" and record["gate.request_id"] == row["id"], record

    after = scrape(metrics)
    waited = delta(before, after, f"{GATE_WAIT}_sum", decision="approved")
    ran = delta(before, after, f"{DURATION}_sum", tool=TOOL)
    assert delta(before, after, f"{GATE_WAIT}_count", decision="approved") == 1, after.text
    assert delta(before, after, f"{DURATION}_count", tool=TOOL) == 1, after.text
    assert waited >= GATE_DELAY, f"승인 대기 {waited:.3f}s 가 지연 {GATE_DELAY}s 보다 짧다 — 대기가 계열에 실리지 않았다"
    assert ran < GATE_DELAY / 2, f"처리 지연 {ran:.3f}s 가 승인 대기({waited:.3f}s)만큼 커졌다 — 대기가 처리 지연에 섞였다"
    print(f"    게이트 대기 {waited:.3f}s · 처리 지연 {ran:.3f}s")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    _config_map(TARGET_GATED)
    try:
        with metrics_url(SERVER_NAMESPACE, SERVER_DEPLOYMENT, METRICS_LOCAL_PORT) as metrics, gatekeeper_url() as gate:
            async with open_session(url) as session:
                print("--- metrics/시나리오 3 (즉답 도구 — 처리 지연만) ---")
                await test_an_immediate_tool_is_only_a_duration(session, metrics)
                print(f"--- metrics/시나리오 3 (승인 {GATE_DELAY:.0f}초 지연 — 대기 계열 ≥ 지연, 처리 지연 ≪ 대기) ---")
                await test_a_slow_approval_lands_on_the_wait_series_not_the_duration(session, gate, metrics)
    finally:
        _delete_config_map(TARGET_GATED)
    print("ok: test-metrics.md#시나리오 3")


if __name__ == "__main__":
    asyncio.run(run())
