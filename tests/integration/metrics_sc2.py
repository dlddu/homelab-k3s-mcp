"""거부 사유가 갈린다 — 다섯 사유가 각자의 계열에 1 씩 실리고, 「사람이 거절」과 「통신 실패」는 다른 계열이다.

검증 시나리오: test-metrics.md#시나리오 2
실행 대상: auth-variant
"""

from __future__ import annotations

import asyncio
import json
import subprocess

import httpx
from mcp.shared.exceptions import McpError

from _auth_variant import API_KEY, GRAFANA_REFUSAL
from _gatekeeper import decide, gatekeeper_url, wait_for_pending
from _helpers import EXPECTED_TOOLS, base_url, open_session, port_forward, wait_for_healthz
from _metrics import Snapshot, delta, metrics_url, scrape

AUTH_NAMESPACE = "homelab-k3s-mcp-auth"
AUTH_DEPLOYMENT = "homelab-k3s-mcp-auth"
AUTH_METRICS_LOCAL_PORT = 19091

GATE_NAMESPACE = "homelab-k3s-mcp-gatekeeper-variant"
GATE_DEPLOYMENT = "homelab-k3s-mcp-gatekeeper-variant"
GATE_LOCAL_PORT = 18087
GATE_METRICS_LOCAL_PORT = 19092

NAMESPACE = "workload-test"
TARGET_INVALID = "mt-s2-invalid-coordinate"
TARGET_UNCONFIGURED = "mt-s2-gate-unconfigured"
TARGET_REJECTED = "mt-s2-rejected"

REFUSALS = "mcp_tool_refusals_total"
CALLS = "mcp_tool_calls_total"

REASONS = (
    "auth_failed",
    "invalid_input",
    "unconfigured",
    "gate_rejected",
    "gate_expired",
    "gate_timeout",
    "gate_unreachable",
    "gate_unconfigured",
)


def _raw_call(url: str, bearer: str, tool: str) -> httpx.Response:
    return httpx.post(
        f"{url}/mcp",
        json={"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": tool, "arguments": {}}},
        headers={
            "Authorization": f"Bearer {bearer}",
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
        },
        timeout=10.0,
    )


def _patch(session, name: str):
    return session.call_tool(
        "resource_patch",
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "name": name,
            "patchType": "merge",
            "patch": {"data": {"key": name}},
        },
    )


def _config_map(name: str) -> None:
    manifest = {
        "apiVersion": "v1",
        "kind": "ConfigMap",
        "metadata": {"name": name, "namespace": NAMESPACE},
        "data": {"key": "one"},
    }
    subprocess.run(["kubectl", "apply", "-f", "-"], input=json.dumps(manifest), text=True, check=True, capture_output=True)


def _refused_calls(snapshot: Snapshot) -> float:
    return sum(snapshot.value(CALLS, tool=tool, result="refused") for tool in sorted(EXPECTED_TOOLS) + ["unregistered"])


def _refusals(snapshot: Snapshot) -> float:
    return sum(
        snapshot.value(REFUSALS, tool=tool, reason=reason)
        for tool in sorted(EXPECTED_TOOLS) + ["unregistered"]
        for reason in REASONS
    )


def _assert_invariant(before: Snapshot, after: Snapshot, where: str) -> None:
    refusals, refused = _refusals(after) - _refusals(before), _refused_calls(after) - _refused_calls(before)
    assert refusals == refused, f"{where}: sum(refusals) 의 차 {refusals} != calls{{refused}} 의 차 {refused}"


def test_auth_failed(url: str, before: Snapshot, metrics: str) -> tuple[str, Snapshot]:
    response = _raw_call(url, "not-the-key", "ping")
    assert response.status_code == 401, (response.status_code, response.text[:200])
    after = scrape(metrics)
    assert delta(before, after, REFUSALS, tool="ping", reason="auth_failed") == 1, after.text
    return "auth_failed", after


async def test_invalid_input(session, before: Snapshot, metrics: str) -> tuple[str, Snapshot]:
    try:
        await session.call_tool(
            "resource_get", {"apiVersion": "v1", "kind": "", "namespace": NAMESPACE, "name": TARGET_INVALID}
        )
    except McpError:
        pass
    else:
        raise AssertionError("kind 없는 좌표가 거부되지 않았다")
    after = scrape(metrics)
    assert delta(before, after, REFUSALS, tool="resource_get", reason="invalid_input") == 1, after.text
    return "invalid_input", after


async def test_unconfigured(session, before: Snapshot, metrics: str) -> tuple[str, Snapshot]:
    result = await session.call_tool("grafana_token", {})
    assert result.isError is True and result.content[0].text == GRAFANA_REFUSAL, result
    after = scrape(metrics)
    assert delta(before, after, REFUSALS, tool="grafana_token", reason="unconfigured") == 1, after.text
    return "unconfigured", after


async def test_gate_unconfigured(session, before: Snapshot, metrics: str) -> tuple[str, Snapshot]:
    try:
        await _patch(session, TARGET_UNCONFIGURED)
    except McpError:
        pass
    else:
        raise AssertionError("게이트 백엔드가 없는데 게이트 대상 호출이 실행됐다")
    after = scrape(metrics)
    assert delta(before, after, REFUSALS, tool="resource_patch", reason="gate_unconfigured") == 1, after.text
    return "gate_unconfigured", after


async def test_gate_rejected_is_not_unreachable() -> str:
    _config_map(TARGET_REJECTED)
    with metrics_url(GATE_NAMESPACE, GATE_DEPLOYMENT, GATE_METRICS_LOCAL_PORT) as metrics, gatekeeper_url() as gate:
        before = scrape(metrics)
        with port_forward(GATE_NAMESPACE, GATE_DEPLOYMENT, 80, GATE_LOCAL_PORT, "/healthz") as url:
            async with open_session(url) as session:
                task = asyncio.create_task(_patch(session, TARGET_REJECTED))
                row = await wait_for_pending(gate, TARGET_REJECTED)
                await decide(gate, row["id"], "REJECTED")
                try:
                    await task
                except McpError:
                    pass
                else:
                    raise AssertionError("거절된 호출이 실행됐다")
        after = scrape(metrics)
        assert delta(before, after, REFUSALS, tool="resource_patch", reason="gate_rejected") == 1, after.text
        assert delta(before, after, REFUSALS, tool="resource_patch", reason="gate_unreachable") == 0, (
            "사람의 거절이 통신 실패 계열에도 실렸다"
        )
        _assert_invariant(before, after, GATE_DEPLOYMENT)
    return "gate_rejected"


def test_the_five_reasons_differ(reasons: list[str]) -> None:
    assert len(set(reasons)) == 5, f"사유가 다섯으로 갈리지 않았다: {reasons}"


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    reasons: list[str] = []

    with metrics_url(AUTH_NAMESPACE, AUTH_DEPLOYMENT, AUTH_METRICS_LOCAL_PORT) as metrics:
        start = scrape(metrics)
        print("--- metrics/시나리오 2 (인증 실패) ---")
        reason, snapshot = test_auth_failed(url, start, metrics)
        reasons.append(reason)
        async with open_session(url, headers={"Authorization": f"Bearer {API_KEY}"}) as session:
            print("--- metrics/시나리오 2 (입력 검증) ---")
            reason, snapshot = await test_invalid_input(session, snapshot, metrics)
            reasons.append(reason)
            print("--- metrics/시나리오 2 (통합 미설정) ---")
            reason, snapshot = await test_unconfigured(session, snapshot, metrics)
            reasons.append(reason)
            print("--- metrics/시나리오 2 (게이트 미설정) ---")
            reason, snapshot = await test_gate_unconfigured(session, snapshot, metrics)
            reasons.append(reason)
        _assert_invariant(start, snapshot, AUTH_DEPLOYMENT)

    print("--- metrics/시나리오 2 (게이트 거절 ≠ 통신 실패 — gatekeeper-variant) ---")
    reasons.append(await test_gate_rejected_is_not_unreachable())

    print("--- metrics/시나리오 2 (다섯 사유가 갈린다) ---")
    test_the_five_reasons_differ(reasons)
    print("ok: test-metrics.md#시나리오 2")


if __name__ == "__main__":
    asyncio.run(run())
