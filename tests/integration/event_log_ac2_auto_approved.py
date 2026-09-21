"""``AUTO_APPROVE`` 표기 — 자동 승인과 사람 승인은 판정 필드가 같고 표기 필드만 다르다.

검증 시나리오: test-event-log.md#시나리오 3
실행 대상: gatekeeper-variant

시나리오의 요점은 「두 경우의 판정 필드는 모두 승인이라 표기 필드 없이는 구별되지 않는다」
는 것이다. 그래서 두 레코드를 **같은 배포에서 같은 도구·같은 종류의 대상**으로 만들어 놓고,
대상 이름·요청 id·표기 필드 셋을 빼면 나머지 필드가 바이트 같음을 단언한다 — 표기 필드가
정보를 더한다는 것은 그것이 빠졌을 때 두 줄이 같아진다는 것으로만 보인다.

자동 승인은 이 변형에서만 발화한다(``approval_gate_ac9.py`` 가 같은 손잡이로 세 모드를 잰다 —
gatekeeper 쪽 사용자 설정이고 create 본문의 userId 가 그 사용자여야 하는데 그 id 를 물고 있는
배포가 이것뿐이다). 끈 변형은 새 배포가 아니라 같은 사용자의 모드를 ``NONE`` 으로 되돌리고
사람 대신 forward-auth 헤더로 판정을 내린 경우다. 모드를 바꾸므로 ``병렬 레인:`` 은 선언하지
않는다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from _eventlog import exactly_one_new, records, select, wait_for_new
from _gatekeeper import decide, gatekeeper_url, set_auto_response, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"
VARIANT_NAMESPACE = "homelab-k3s-mcp-gatekeeper-variant"
VARIANT_DEPLOYMENT = "homelab-k3s-mcp-gatekeeper-variant"

TARGET_AUTO = "el-s3-auto-approved"
TARGET_MANUAL = "el-s3-human-approved"

TOOL = "resource_patch"

#: 두 레코드가 갈려도 되는 필드 — 대상 이름, 요청 id, 그리고 시나리오가 재는 표기 자체.
DIFFERING_FIELDS = {"time", "target.name", "gate.request_id", "gate.auto_approved"}


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


def _count(name: str) -> int:
    return len(select(records(VARIANT_NAMESPACE, VARIANT_DEPLOYMENT), tool=TOOL, **{"target.name": name}))


def _record(name: str, before: int) -> dict[str, str]:
    found = wait_for_new(VARIANT_NAMESPACE, VARIANT_DEPLOYMENT, before, tool=TOOL, **{"target.name": name})
    return exactly_one_new(found, before, name)


async def test_auto_approved_is_marked(session, gate: str) -> dict[str, str]:
    before = _count(TARGET_AUTO)
    set_auto_response(gate, "AUTO_APPROVE")
    result = await _patch(session, TARGET_AUTO)
    assert result.isError is False, result

    record = _record(TARGET_AUTO, before)
    assert record["result"] == "success" and record["gate.decision"] == "approved", record
    assert record["gate.auto_approved"] == "true", record
    assert record["gate.request_id"], record
    return record


async def test_human_approved_is_not_marked(session, gate: str) -> dict[str, str]:
    before = _count(TARGET_MANUAL)
    set_auto_response(gate, "NONE")
    task = asyncio.create_task(_patch(session, TARGET_MANUAL))
    row = await wait_for_pending(gate, TARGET_MANUAL)
    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result

    record = _record(TARGET_MANUAL, before)
    assert record["result"] == "success" and record["gate.decision"] == "approved", record
    assert record["gate.auto_approved"] == "false", record
    assert record["gate.request_id"] == row["id"], (record, row["id"])
    return record


def test_only_the_flag_tells_them_apart(auto: dict[str, str], human: dict[str, str]) -> None:
    def rest(record: dict[str, str]) -> dict[str, str]:
        return {k: v for k, v in record.items() if k not in DIFFERING_FIELDS}

    assert rest(auto) == rest(human), (
        "표기 필드 밖에서 두 레코드가 갈린다 — 판정 필드만으로 구별된다면 표기는 잉여다:\n"
        f"{rest(auto)}\n{rest(human)}"
    )
    assert auto["gate.auto_approved"] != human["gate.auto_approved"], (auto, human)


async def run() -> None:
    url = base_url()
    _config_map(TARGET_AUTO)
    _config_map(TARGET_MANUAL)
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        try:
            async with open_session(url) as session:
                print("--- event-log/시나리오 3 (AUTO_APPROVE 켠 경우) ---")
                auto = await test_auto_approved_is_marked(session, gate)
                print("--- event-log/시나리오 3 (끈 경우 — 사람 승인) ---")
                human = await test_human_approved_is_not_marked(session, gate)
        finally:
            set_auto_response(gate, "NONE")

    print("--- event-log/시나리오 3 (표기 필드 없이는 같다) ---")
    test_only_the_flag_tells_them_apart(auto, human)
    print("ok: test-event-log.md#시나리오 3")


if __name__ == "__main__":
    asyncio.run(run())
