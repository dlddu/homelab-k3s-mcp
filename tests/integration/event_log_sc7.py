"""검증·게이트·미설정 거부의 기록 — 셋 다 레코드가 남고 사유가 갈리며, 아무 데도 요청이 나가지 않았다.

검증 시나리오: test-event-log.md#시나리오 7
실행 대상: auth-variant
"""

from __future__ import annotations

import asyncio

from mcp.shared.exceptions import McpError

from _auth_variant import API_KEY, GRAFANA_REFUSAL
from _eventlog import exactly_one_new, records, select, wait_for_new
from _gatekeeper import gatekeeper_url, list_requests
from _helpers import base_url, open_session, wait_for_healthz

AUTH_NAMESPACE = "homelab-k3s-mcp-auth"
AUTH_DEPLOYMENT = "homelab-k3s-mcp-auth"

NAMESPACE = "workload-test"
TARGET_INVALID = "el-s7-invalid-coordinate"
TARGET_GATED = "el-s7-gated"

INVALID_PARAMS = -32602


def _count(**wanted: str) -> int:
    return len(select(records(AUTH_NAMESPACE, AUTH_DEPLOYMENT), **wanted))


def _record(before: int, **wanted: str) -> dict[str, str]:
    found = wait_for_new(AUTH_NAMESPACE, AUTH_DEPLOYMENT, before, **wanted)
    record = exactly_one_new(found, before, str(wanted))
    assert record["result"] == "refused", record
    return record


async def test_invalid_input_is_refused_before_any_handler(session) -> dict[str, str]:
    wanted = {"tool": "resource_get", "target.name": TARGET_INVALID}
    before = _count(**wanted)
    try:
        await session.call_tool(
            "resource_get",
            {"apiVersion": "v1", "kind": "", "namespace": NAMESPACE, "name": TARGET_INVALID},
        )
    except McpError as exc:
        assert exc.error.code == INVALID_PARAMS, exc.error
    else:
        raise AssertionError("kind 없는 좌표가 거부되지 않았다")
    record = _record(before, **wanted)
    assert record["reason"] == "invalid_input", record
    assert record["target.kind"] == "", record
    return record


async def test_gate_refusal_without_a_backend(session, gate: str) -> dict[str, str]:
    wanted = {"tool": "resource_patch", "target.name": TARGET_GATED}
    before = _count(**wanted)
    try:
        await session.call_tool(
            "resource_patch",
            {
                "apiVersion": "v1",
                "kind": "ConfigMap",
                "namespace": NAMESPACE,
                "name": TARGET_GATED,
                "patchType": "merge",
                "patch": {"data": {"key": TARGET_GATED}},
            },
        )
    except McpError:
        pass
    else:
        raise AssertionError("게이트 백엔드가 없는데 게이트 대상 호출이 실행됐다")
    record = _record(before, **wanted)
    assert record["reason"] == "gate_unconfigured", record
    assert record["gate.decision"] == "unconfigured" and record["gate.request_id"] == "", record
    strays = [r for r in list_requests(gate) if TARGET_GATED in r.get("context", "")]
    assert not strays, f"백엔드가 없는 배포의 호출이 gatekeeper 에 요청을 만들었다: {strays}"
    return record


async def test_unconfigured_integration_is_a_refusal(session) -> dict[str, str]:
    wanted = {"tool": "grafana_token"}
    before = _count(**wanted)
    result = await session.call_tool("grafana_token", {})
    assert result.isError is True and result.content[0].text == GRAFANA_REFUSAL, result
    record = _record(before, **wanted)
    assert record["reason"] == "unconfigured", record
    return record


def test_the_three_reasons_differ(three: list[dict[str, str]]) -> None:
    reasons = [r["reason"] for r in three]
    assert len(set(reasons)) == 3, f"사유가 갈리지 않았다: {reasons}"


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url, headers={"Authorization": f"Bearer {API_KEY}"}) as session:
            print("--- event-log/시나리오 7 (입력 검증 거부) ---")
            invalid = await test_invalid_input_is_refused_before_any_handler(session)
            print("--- event-log/시나리오 7 (게이트 거부 — 백엔드 없음) ---")
            gated = await test_gate_refusal_without_a_backend(session, gate)
            print("--- event-log/시나리오 7 (미설정 거부) ---")
            unconfigured = await test_unconfigured_integration_is_a_refusal(session)

    print("--- event-log/시나리오 7 (세 사유가 갈린다) ---")
    test_the_three_reasons_differ([invalid, gated, unconfigured])
    print("ok: test-event-log.md#시나리오 7")


if __name__ == "__main__":
    asyncio.run(run())
