"""자동 승인은 숨기지 않는다 — AUTO_APPROVE·AUTO_REJECT 가 표기와 함께 관측된다.

검증 시나리오: test-approval-gate.md#시나리오 9
실행 대상: gatekeeper-variant

자동 응답 모드는 gatekeeper 쪽 사용자 설정이라 실물에서만 세울 수 있고,
모드가 작동하려면 create 본문의 userId 가 그 사용자의 id(cuid)와 일치해야 한다 —
픽스처가 미리 알 수 없는 값이라 ci.yml 시드 스텝이 만든 사용자를 변형 SUT 의
GATEKEEPER_USER_ID 로 핀해 둔다. 이 파일은 같은 사용자의 모드를 AUTO_APPROVE →
AUTO_REJECT → NONE 으로 바꿔 세 상태를 잰다 — 사용자가 둘이어야 할 이유가 없다,
모드가 곧 상태다.

「숨기지 않는다」의 관측면 셋: ⑴ 도구 응답 본문에 자동 승인 표기(SUT 가 붙인다)
⑵ SUT 로그의 auto_approved=true ⑶ gatekeeper 기록에 처리자가 사용자 자신으로
남는다. ⑶ 이 단독으로는 사람 댄스와 구별되지 않지만, ⑴ 이 있었으면 모드가
작동했다는 것이고 ⑵ 는 그것이 SUT 쪽에서도 관측됐음을 맞춰 본다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from mcp.shared.exceptions import McpError

from _gatekeeper import (
    decide,
    gatekeeper_url,
    get_request,
    list_requests,
    me,
    set_auto_response,
    wait_for_pending,
)
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"
VARIANT_NAMESPACE = "homelab-k3s-mcp-gatekeeper-variant"
VARIANT_DEPLOYMENT = "homelab-k3s-mcp-gatekeeper-variant"

TARGET_APPROVED = "gk-ac9-auto-approve"
TARGET_REJECTED = "gk-ac9-auto-reject"
TARGET_MANUAL = "gk-ac9-manual"

AUTO_NOTICE = "[auto-approved by gatekeeper without human review"


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


def _variant_log() -> str:
    return subprocess.check_output(
        ["kubectl", "-n", VARIANT_NAMESPACE, "logs", f"deploy/{VARIANT_DEPLOYMENT}", "--tail=400"],
        text=True,
    )


async def _patch(session, name: str):
    return await session.call_tool(
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


def _request_id_from_notice(text: str) -> str:
    marker = "request "
    head, _, tail = text.rpartition(marker)
    assert head or tail, f"자동 승인 표기에 요청 id 가 없다: {text!r}"
    return tail.split("]")[0]


async def run() -> None:
    url = base_url()
    for name in (TARGET_APPROVED, TARGET_REJECTED, TARGET_MANUAL):
        _config_map(name, "one")
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        mine = me(gate)
        try:
            async with open_session(url) as session:
                print("--- approval-gate/시나리오 9 (AUTO_APPROVE) ---")
                set_auto_response(gate, "AUTO_APPROVE")
                approved = await _patch(session, TARGET_APPROVED)
                assert approved.isError is False, approved
                notice = approved.content[-1].text
                assert AUTO_NOTICE in notice, f"자동 승인 표기가 응답에 없다: {notice}"
                auto_row = get_request(gate, _request_id_from_notice(notice))
                assert auto_row["status"] == "APPROVED", auto_row
                assert auto_row["processedById"] == mine["id"], (
                    f"기록의 처리자({auto_row['processedById']})가 사용자({mine['id']})와 다르다"
                )
                assert "auto_approved=true" in _variant_log()

                print("--- approval-gate/시나리오 9 (AUTO_REJECT) ---")
                set_auto_response(gate, "AUTO_REJECT")
                try:
                    await _patch(session, TARGET_REJECTED)
                except McpError as exc:
                    assert "auto-rejected" in str(exc), exc
                else:
                    raise AssertionError("AUTO_REJECT 사용자의 호출이 실행됐다")
                rejected_rows = [
                    r
                    for r in list_requests(gate, status="REJECTED")
                    if TARGET_REJECTED in r.get("context", "")
                ]
                assert rejected_rows, "AUTO_REJECT 기록이 남지 않았다"
                assert rejected_rows[-1]["processedById"] == mine["id"], rejected_rows[-1]

                print("--- approval-gate/시나리오 9 (모드 NONE 은 댄스로 돌아온다) ---")
                set_auto_response(gate, "NONE")
                task = asyncio.create_task(_patch(session, TARGET_MANUAL))
                row = wait_for_pending(gate, TARGET_MANUAL)
                decide(gate, row["id"], "APPROVED")
                manual = await task
                assert manual.isError is False, manual
                manual_text = manual.content[0].text
                assert AUTO_NOTICE not in manual_text, manual_text
        finally:
            set_auto_response(gate, "NONE")
    print("ok: test-approval-gate.md#시나리오 9")


if __name__ == "__main__":
    asyncio.run(run())
