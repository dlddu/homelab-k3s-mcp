"""승인 요청의 출처 — 이유는 언제나, 세션 ID 는 헤더가 있을 때 실린다.

검증 시나리오: test-approval-gate.md#시나리오 12
실행 대상: approval-variant

공유 세션(`ReasonedSession`)은 `reason` 을 채워 보내므로 누락 경로는 그 안쪽 세션으로 부른다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from mcp.shared.exceptions import McpError

from _approval_gate import (
    CREATE_BODY_FIELDS,
    approval_url,
    count_requests,
    create_records,
    decide,
    get_request,
    trace_url,
    wait_for_pending,
)
from _helpers import base_url, open_session, wait_for_healthz
from _reason import REASON

NAMESPACE = "workload-test"

SERVER_NAMESPACE = "homelab-k3s-mcp-approval-variant"
SERVER_DEPLOYMENT = "homelab-k3s-mcp-approval-variant"

SESSION_HEADER = "X-Session-Platform-Session-Id"
SESSION_ID = "ac12-session-0001"
LONG_SESSION_ID = "s" * 129

TARGET_MAIN = "gk-sc12-main"
TARGET_MISSING = "gk-sc12-missing"
TARGET_LONG = "gk-sc12-long"
READ_PLAIN = "gk-sc12-read-plain"
SECRET_PROBE = "gk-sc12-secret-probe"


def _config_map(name: str, value: str) -> None:
    manifest = {
        "apiVersion": "v1",
        "kind": "ConfigMap",
        "metadata": {"name": name, "namespace": NAMESPACE},
        "data": {"key": value},
    }
    subprocess.run(
        ["kubectl", "apply", "-f", "-"],
        input=json.dumps(manifest),
        text=True,
        check=True,
        capture_output=True,
    )


def _config_value(name: str) -> str:
    out = subprocess.check_output(
        ["kubectl", "-n", NAMESPACE, "get", "configmap", name, "-o", "json"], text=True
    )
    return json.loads(out)["data"]["key"]


def _server_logs() -> str:
    return subprocess.check_output(
        ["kubectl", "-n", SERVER_NAMESPACE, "logs", f"deploy/{SERVER_DEPLOYMENT}", "--tail=1000"],
        text=True,
    )


def _patch_args(name: str, **extra) -> dict:
    return {
        "apiVersion": "v1",
        "kind": "ConfigMap",
        "namespace": NAMESPACE,
        "name": name,
        "patchType": "merge",
        "patch": {"data": {"key": "changed"}},
        **extra,
    }


def _bare(session):
    return session._session


async def _refused(session, tool: str, args: dict) -> str:
    try:
        result = await _bare(session).call_tool(tool, args)
    except McpError as exc:
        assert exc.error.code == -32602, f"{tool}: 거부 코드가 -32602 가 아니다: {exc.error}"
        return exc.error.message
    assert result.isError is True, f"{tool}: reason 없는 호출이 성공으로 돌아왔다: {result}"
    return json.dumps([getattr(c, "text", "") for c in result.content], ensure_ascii=False)


async def _approved(session, gate: str, tool: str, args: dict, marker: str) -> dict:
    task = asyncio.create_task(session.call_tool(tool, args))
    row = await wait_for_pending(gate, marker)
    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result
    return row


def _create_body(trace: str, marker: str) -> dict:
    records = [r for r in create_records(trace) if marker in json.dumps(r.get("body", {}))]
    assert len(records) == 1, f"{marker}: create 기록이 {len(records)} 건이다"
    assert records[0]["status"] == 201, records[0]
    return records[0]["body"]


async def run() -> None:
    url = base_url()
    for name in (TARGET_MAIN, TARGET_MISSING, TARGET_LONG, READ_PLAIN):
        _config_map(name, "before")
    wait_for_healthz(url)

    print("--- approval-gate/시나리오 12 ---")
    with approval_url() as gate, trace_url() as trace:
        async with open_session(url, headers={SESSION_HEADER: SESSION_ID}) as session:
            print("--- approval-gate/시나리오 12 (a: 헤더와 이유가 승인 요청에 실린다) ---")
            row = await _approved(
                session, gate, "resource_patch",
                _patch_args(TARGET_MAIN, reason=f"  {REASON}  "), TARGET_MAIN,
            )
            body = _create_body(trace, TARGET_MAIN)
            assert set(body) == set(CREATE_BODY_FIELDS) | {"sessionId"}, sorted(body)
            assert body["sessionId"] == SESSION_ID, body
            assert body["reason"] == REASON, body
            assert REASON not in body["context"], body["context"]
            assert _config_value(TARGET_MAIN) == "changed"
            record = get_request(gate, row["id"])
            assert record["session_id"] == SESSION_ID, record
            assert record["reason"] == REASON, record
            logs = _server_logs()
            assert any(
                f"session_id={SESSION_ID}" in line and "reason" in line
                for line in logs.splitlines()
            ) or any(
                SESSION_ID in line and "session_id" in line and REASON in line
                for line in logs.splitlines()
            ), "승인 로그 줄에 session_id·reason 이 없다"

            print("--- approval-gate/시나리오 12 (b: reason 없음) ---")
            message = await _refused(session, "resource_patch", _patch_args(TARGET_MISSING))
            assert "resource_patch" in message and "reason is required" in message, message
            assert count_requests(gate, TARGET_MISSING) == 0
            assert _config_value(TARGET_MISSING) == "before"

            print("--- approval-gate/시나리오 12 (c: 빈 값·공백·숫자) ---")
            for bad in ("", "   ", 42):
                await _refused(session, "resource_patch", _patch_args(TARGET_MISSING, reason=bad))
            assert count_requests(gate, TARGET_MISSING) == 0
            assert _config_value(TARGET_MISSING) == "before"

            print("--- approval-gate/시나리오 12 (d: 민감 종류만 reason 이 필요하다) ---")
            message = await _refused(
                session, "resource_get",
                {"apiVersion": "v1", "kind": "Secret", "namespace": NAMESPACE, "name": SECRET_PROBE},
            )
            assert "reason is required" in message, message
            assert count_requests(gate, SECRET_PROBE) == 0
            plain = await _bare(session).call_tool(
                "resource_get",
                {"apiVersion": "v1", "kind": "ConfigMap", "namespace": NAMESPACE, "name": READ_PLAIN},
            )
            assert plain.isError is False, plain
            assert count_requests(gate, READ_PLAIN) == 0

        print("--- approval-gate/시나리오 12 (e: 129자 헤더는 버려진다) ---")
        async with open_session(url, headers={SESSION_HEADER: LONG_SESSION_ID}) as session:
            await _approved(
                session, gate, "resource_patch",
                _patch_args(TARGET_LONG, reason=REASON), TARGET_LONG,
            )
        body = _create_body(trace, TARGET_LONG)
        assert "sessionId" not in body, body
        assert body["reason"] == REASON, body
        print("ok: test-approval-gate.md#시나리오 12")


if __name__ == "__main__":
    asyncio.run(run())
