"""감사 로그 — 승인 실행과 거부가 SUT 로그에 감사 레코드를 남긴다.

검증 시나리오: test-approval-gate.md#시나리오 8
실행 대상: primary

시나리오가 요구하는 것은 요청 id·externalId·도구·동사·대상 좌표가 로그에 남는
것, 승인 실행에 processedById, 거부에 사유가 함께 남는 것이다. 이 로그는 SUT 파드
stdout 의 slog 라인이라 gatekeeper 기록이 아니라 kubectl logs 로 잰다 — 요청
id 와 external_id 는 gatekeeper 레코드와 대조해 같은 값임까지 확인한다(로그 한
줄이 어느 승인 요청의 것인지 대조 없이는 알 수 없다).

거부 절의 processedById 는 일부러 단언하지 않는다 — 거부된 요청은 처리자가
gatekeeper 에서는 남지만 SUT 는 실행하지 않았으므로 granted 라인이 아예 없다.
「거부에 거부 사유가 남는다」가 그 절의 계약이다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from _gatekeeper import decide, gatekeeper_url, get_request, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"
SERVER_NAMESPACE = "homelab-k3s-mcp"
SERVER_DEPLOYMENT = "homelab-k3s-mcp"

TARGET_GRANTED = "gk-ac8-granted"
TARGET_REJECTED = "gk-ac8-rejected"


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


def _server_log_lines() -> str:
    return subprocess.check_output(
        [
            "kubectl", "-n", SERVER_NAMESPACE, "logs",
            f"deploy/{SERVER_DEPLOYMENT}", "--tail=400",
        ],
        text=True,
    )


async def _patch(session, name: str):
    return asyncio.create_task(
        session.call_tool(
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
    )


async def run() -> None:
    url = base_url()
    _config_map(TARGET_GRANTED, "one")
    _config_map(TARGET_REJECTED, "one")
    wait_for_healthz(url)

    print("--- approval-gate/시나리오 8 (승인 실행 1회·거부 1회) ---")
    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            granted_task = _patch(session, TARGET_GRANTED)
            granted_row = await wait_for_pending(gate, TARGET_GRANTED)
            decided = await decide(gate, granted_row["id"], "APPROVED")
            granted = await granted_task
            assert granted.isError is False, granted

            rejected_task = _patch(session, TARGET_REJECTED)
            rejected_row = await wait_for_pending(gate, TARGET_REJECTED)
            await decide(gate, rejected_row["id"], "REJECTED")
            rejected = await rejected_task
            assert rejected.isError is True, rejected

        print("--- approval-gate/시나리오 8 (로그 감사 레코드) ---")
        log = _server_log_lines()
        granted_lines = [
            line
            for line in log.splitlines()
            if "approval granted" in line and granted_row["id"] in line
        ]
        assert granted_lines, f"승인 실행의 감사 라인이 없다: 요청 {granted_row['id']}"
        granted_line = granted_lines[-1]
        for expected in (
            "tool=resource_patch",
            "update on configmaps",
            f"external_id={get_request(gate, granted_row['id'])['externalId']}",
            f"processed_by_id={decided['processedById']}",
            "auto_approved=false",
        ):
            assert expected in granted_line, f"granted 라인에 {expected!r} 이 없다"

        rejected_lines = [
            line
            for line in log.splitlines()
            if "approval refused" in line and rejected_row["id"] in line
        ]
        assert rejected_lines, f"거부의 감사 라인이 없다: 요청 {rejected_row['id']}"
        rejected_line = rejected_lines[-1]
        for expected in (
            "tool=resource_patch",
            "update on configmaps",
            "error=",
            "approval rejected (request",
        ):
            assert expected in rejected_line, f"refused 라인에 {expected!r} 이 없다"
    print("ok: test-approval-gate.md#시나리오 8")


if __name__ == "__main__":
    asyncio.run(run())
