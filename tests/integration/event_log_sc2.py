"""게이트 판정의 구분 — 승인·거절·타임아웃·미설정 네 경로의 레코드가 판정 필드에서 갈린다.

검증 시나리오: test-event-log.md#시나리오 2
실행 대상: gatekeeper-variant

통신 실패는 판정을 낳지 못한 요청이라 id 가 없고, 그 구분이 이 두 필드에 그대로 남는다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from mcp.shared.exceptions import McpError

from _auth_variant import API_KEY as AUTH_VARIANT_API_KEY
from _eventlog import exactly_one_new, records, select, wait_for_new
from _gatekeeper import decide, gatekeeper_url, set_auto_response, wait_for_pending
from _helpers import base_url, open_session, port_forward, wait_for_healthz

NAMESPACE = "workload-test"

VARIANT_NAMESPACE = "homelab-k3s-mcp-gatekeeper-variant"
VARIANT_DEPLOYMENT = "homelab-k3s-mcp-gatekeeper-variant"

AUTH_NAMESPACE = "homelab-k3s-mcp-auth"
AUTH_DEPLOYMENT = "homelab-k3s-mcp-auth"
AUTH_LOCAL_PORT = 18089

TARGET_APPROVED = "el-s2-approved"
TARGET_REJECTED = "el-s2-rejected"
TARGET_TIMEOUT = "el-s2-timeout"
TARGET_UNCONFIGURED = "el-s2-unconfigured"

TOOL = "resource_patch"


def _config_map(name: str) -> None:
    manifest = {
        "apiVersion": "v1",
        "kind": "ConfigMap",
        "metadata": {"name": name, "namespace": NAMESPACE},
        "data": {"key": "one"},
    }
    subprocess.run(
        ["kubectl", "apply", "-f", "-"],
        input=json.dumps(manifest),
        text=True,
        check=True,
        capture_output=True,
    )


def _patch(session, name: str):
    return session.call_tool(
        TOOL,
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "name": name,
            "patchType": "merge",
            "patch": {"data": {"key": name}},
        },
    )


def _count(namespace: str, deployment: str, name: str) -> int:
    return len(select(records(namespace, deployment), tool=TOOL, **{"target.name": name}))


def _record(namespace: str, deployment: str, name: str, before: int) -> dict[str, str]:
    found = wait_for_new(namespace, deployment, before, tool=TOOL, **{"target.name": name})
    return exactly_one_new(found, before, name)


async def test_approved(session, gate: str) -> dict[str, str]:
    before = _count(VARIANT_NAMESPACE, VARIANT_DEPLOYMENT, TARGET_APPROVED)
    task = asyncio.create_task(_patch(session, TARGET_APPROVED))
    row = await wait_for_pending(gate, TARGET_APPROVED)
    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result

    record = _record(VARIANT_NAMESPACE, VARIANT_DEPLOYMENT, TARGET_APPROVED, before)
    assert record["result"] == "success" and record["reason"] == "", record
    assert record["gate.decision"] == "approved", record
    assert record["gate.request_id"] == row["id"], (record, row["id"])
    return record


async def test_rejected(session, gate: str) -> dict[str, str]:
    before = _count(VARIANT_NAMESPACE, VARIANT_DEPLOYMENT, TARGET_REJECTED)
    task = asyncio.create_task(_patch(session, TARGET_REJECTED))
    row = await wait_for_pending(gate, TARGET_REJECTED)
    await decide(gate, row["id"], "REJECTED")
    try:
        await task
    except McpError:
        pass
    else:
        raise AssertionError("거절된 호출이 실행됐다")

    record = _record(VARIANT_NAMESPACE, VARIANT_DEPLOYMENT, TARGET_REJECTED, before)
    assert record["result"] == "refused" and record["reason"] == "gate_rejected", record
    assert record["gate.decision"] == "rejected", record
    assert record["gate.decision"] != "unreachable" and record["reason"] != "gate_unreachable", record
    assert record["gate.request_id"] == row["id"], (record, row["id"])
    return record


async def test_timed_out(session, gate: str) -> dict[str, str]:
    before = _count(VARIANT_NAMESPACE, VARIANT_DEPLOYMENT, TARGET_TIMEOUT)
    task = asyncio.create_task(_patch(session, TARGET_TIMEOUT))
    row = await wait_for_pending(gate, TARGET_TIMEOUT)
    try:
        await task
    except McpError:
        pass
    else:
        raise AssertionError("판정 없이 둔 호출이 실행됐다")

    record = _record(VARIANT_NAMESPACE, VARIANT_DEPLOYMENT, TARGET_TIMEOUT, before)
    assert record["result"] == "refused", record
    assert record["gate.decision"] in ("timeout", "expired"), record
    assert record["reason"] == f"gate_{record['gate.decision']}", record
    assert record["gate.request_id"] == row["id"], (record, row["id"])
    return record


async def test_unconfigured() -> dict[str, str]:
    before = _count(AUTH_NAMESPACE, AUTH_DEPLOYMENT, TARGET_UNCONFIGURED)
    with port_forward(AUTH_NAMESPACE, AUTH_DEPLOYMENT, 80, AUTH_LOCAL_PORT, "/healthz") as url:
        headers = {"Authorization": f"Bearer {AUTH_VARIANT_API_KEY}"}
        async with open_session(url, headers=headers) as session:
            try:
                await _patch(session, TARGET_UNCONFIGURED)
            except McpError:
                pass
            else:
                raise AssertionError("게이트 백엔드가 없는 배포에서 게이트 대상 호출이 실행됐다")

    record = _record(AUTH_NAMESPACE, AUTH_DEPLOYMENT, TARGET_UNCONFIGURED, before)
    assert record["result"] == "refused" and record["reason"] == "gate_unconfigured", record
    assert record["gate.decision"] == "unconfigured", record
    assert record["gate.request_id"] == "", record
    return record


def test_the_four_decisions_differ(four: list[dict[str, str]]) -> None:
    decisions = [r["gate.decision"] for r in four]
    assert len(set(decisions)) == 4, f"판정 값이 갈리지 않았다: {decisions}"
    ids = [r["gate.request_id"] for r in four[:3]]
    assert all(ids) and len(set(ids)) == 3, f"요청 id 가 비었거나 겹친다: {ids}"


async def run() -> None:
    url = base_url()
    for name in (TARGET_APPROVED, TARGET_REJECTED, TARGET_TIMEOUT):
        _config_map(name)
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        set_auto_response(gate, "NONE")
        async with open_session(url) as session:
            print("--- event-log/시나리오 2 (승인) ---")
            approved = await test_approved(session, gate)
            print("--- event-log/시나리오 2 (거절) ---")
            rejected = await test_rejected(session, gate)
            print("--- event-log/시나리오 2 (타임아웃) ---")
            timed_out = await test_timed_out(session, gate)
    print("--- event-log/시나리오 2 (미설정 — auth-variant) ---")
    unconfigured = await test_unconfigured()

    print("--- event-log/시나리오 2 (네 판정이 갈린다) ---")
    test_the_four_decisions_differ([approved, rejected, timed_out, unconfigured])
    print("ok: test-event-log.md#시나리오 2")


if __name__ == "__main__":
    asyncio.run(run())
