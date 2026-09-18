"""요청 본문 계약 — 게이트가 gatekeeper 에 보내는 create 의 몸통이 계약 그대로다.

검증 시나리오: test-approval-gate.md#시나리오 2
실행 대상: gatekeeper-variant

시나리오가 요구하는 관측은 create **요청**의 내용이다 — x-api-key 헤더 존재,
externalId·context·requesterName·timeoutSeconds, GATEKEEPER_USER_ID 설정 시의
userId, 그리고 서로 다른 대상에 대한 두 호출의 externalId 가 서로 다르다는 것.
이것들은 upstream(gatekeeper)의 응답과 기록 어디에도 온전히 남지 않는다 — 응답
레코드에는 userId 필드가 없다. 그래서 이 파일은 SUT 를 기록 프록시
(gatekeeper-trace) 뒤에 두는 변형 배포에서만 돈다. 프록시는 모킹이 아니라
http-trace 와 같은 계열의 기록면이다.

「GATEKEEPER_USER_ID 설정 시 userId 포함」 절의 대상 id 는 cuid 라 픽스처가 미리
알 수 없다 — ci.yml 시드 스텝이 forward-auth 로 사용자를 만들고 그 id 를 변형
SUT 의 env 로 핀한다. userId 단언이 성립하면 시드·배선·create 본문 셋이 한
사슬로 맞았다는 것이고, 그래서 여기서 그 사슬 전체를 잰다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from _gatekeeper import (
    CREATE_BODY_FIELDS,
    create_records,
    decide,
    gatekeeper_url,
    get_request,
    me,
    trace_url,
    wait_for_pending,
)
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"

#: 두 호출의 대상. 시나리오가 「서로 다른 대상으로 2회」를 요구하므로 이름이 두 개다.
TARGET_ONE = "gk-ac2-target-one"
TARGET_TWO = "gk-ac2-target-two"

#: 변형 SUT 의 타임아웃. `tests/k8s/kind/gatekeeper-variant.yaml` 의 값과 같다.
VARIANT_TIMEOUT_SECONDS = 5


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


async def _approved_patch(session, gate: str, name: str):
    task = asyncio.create_task(
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
    row = await wait_for_pending(gate, name)
    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result
    return row


async def run() -> None:
    url = base_url()
    _config_map(TARGET_ONE, "one")
    _config_map(TARGET_TWO, "two")
    wait_for_healthz(url)

    print("--- approval-gate/시나리오 2 (게이트 대상 호출 둘) ---")
    with gatekeeper_url() as gate, trace_url() as trace:
        async with open_session(url) as session:
            row_one = await _approved_patch(session, gate, TARGET_ONE)
            row_two = await _approved_patch(session, gate, TARGET_TWO)

        print("--- approval-gate/시나리오 2 (create 본문 계약) ---")
        mine = me(gate)
        creates = [
            r
            for r in create_records(trace)
            if TARGET_ONE in json.dumps(r.get("body", {}))
            or TARGET_TWO in json.dumps(r.get("body", {}))
        ]
        assert len(creates) >= 2, f"create 기록이 {len(creates)} 건뿐이다"
        assert all(r["x_api_key_present"] for r in creates), creates

        bodies = [r["body"] for r in creates[:2]]
        for body in bodies:
            missing = [f for f in CREATE_BODY_FIELDS if f not in body]
            assert not missing, f"create 본문에 {missing} 이 없다: {body}"
            assert body["externalId"], body
            assert body["context"], body
            assert body["requesterName"], body
            assert body["timeoutSeconds"] == VARIANT_TIMEOUT_SECONDS, body
            assert body.get("userId") == mine["id"], (
                f"userId 가 시드한 사용자({mine['id']})와 다르다: {body.get('userId')}"
            )
        assert bodies[0]["externalId"] != bodies[1]["externalId"], (
            "서로 다른 대상의 두 호출이 같은 externalId 를 썼다"
        )
        assert TARGET_ONE in bodies[0]["context"], bodies[0]["context"]
        assert TARGET_TWO in bodies[1]["context"], bodies[1]["context"]

        print("--- approval-gate/시나리오 2 (승인 뒤 실행됐다) ---")
        for row in (row_one, row_two):
            record = get_request(gate, row["id"])
            assert record["status"] == "APPROVED", record
            assert record["externalId"] in (
                bodies[0]["externalId"],
                bodies[1]["externalId"],
            ), record
        print("ok: test-approval-gate.md#시나리오 2")


if __name__ == "__main__":
    asyncio.run(run())
