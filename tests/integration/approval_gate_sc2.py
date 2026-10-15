"""요청 본문 계약 — 게이트가 PocketAide 에 보내는 create 의 몸통이 계약 그대로다.

검증 시나리오: test-approval-gate.md#시나리오 2
실행 대상: approval-variant

시나리오가 요구하는 관측은 create **요청**의 내용이다. 그 본문은 PocketAide 의 기록에
온전히 남지 않는다 — 앱 API 는 저장한 필드를 자기 이름(``external_id`` 등)으로 돌려줄 뿐
보낸 키 집합은 보여 주지 않는다. 그래서 이 파일은 SUT 를 기록 프록시(pocket-aide-trace)
뒤에 두는 변형 배포에서만 돈다. 프록시는 모킹이 아니라 http-trace 와 같은 계열의 기록면이다.

「누구의 요청인가」는 본문에 없다 — PocketAide 는 호출자의 서비스 어카운트 토큰으로 그
호출자를 등록한 사용자를 찾는다. 그래서 요청이 **변형 배포를 등록한 사용자의 것으로**
보이는 것까지 잰다. 그것이 성립하면 등록(ci.yml)·토큰 볼륨(배포)·Bearer 헤더(SUT) 셋이
한 사슬로 맞았다는 것이다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from _approval_gate import (
    CREATE_BODY_FIELDS,
    VARIANT_OWNER,
    approval_url,
    create_records,
    decide,
    get_request,
    trace_url,
    wait_for_pending,
)
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"

TARGET_ONE = "gk-ac2-target-one"
TARGET_TWO = "gk-ac2-target-two"

#: 변형 SUT 의 타임아웃. `tests/k8s/kind/approval-variant.yaml` 의 값과 같다.
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
    with approval_url() as gate, trace_url() as trace:
        async with open_session(url) as session:
            row_one = await _approved_patch(session, gate, TARGET_ONE)
            row_two = await _approved_patch(session, gate, TARGET_TWO)

        print("--- approval-gate/시나리오 2 (create 본문 계약) ---")
        creates = [
            r
            for r in create_records(trace)
            if TARGET_ONE in json.dumps(r.get("body", {}))
            or TARGET_TWO in json.dumps(r.get("body", {}))
        ]
        assert len(creates) >= 2, f"create 기록이 {len(creates)} 건뿐이다"
        assert all(r["authorization_scheme"] == "Bearer" for r in creates), creates
        assert all(r["status"] == 201 for r in creates), creates

        bodies = [r["body"] for r in creates[:2]]
        for body in bodies:
            assert set(body) == set(CREATE_BODY_FIELDS), (
                f"create 본문의 키가 계약({CREATE_BODY_FIELDS})과 다르다: {sorted(body)}"
            )
            assert body["externalId"], body
            assert body["context"], body
            assert body["requesterName"], body
            assert body["timeoutSeconds"] == VARIANT_TIMEOUT_SECONDS, body
        assert bodies[0]["externalId"] != bodies[1]["externalId"], (
            "서로 다른 대상의 두 호출이 같은 externalId 를 썼다"
        )
        assert TARGET_ONE in bodies[0]["context"], bodies[0]["context"]
        assert TARGET_TWO in bodies[1]["context"], bodies[1]["context"]

        print("--- approval-gate/시나리오 2 (승인 뒤 실행됐다 · 등록한 사용자의 요청이다) ---")
        for row in (row_one, row_two):
            record = get_request(gate, row["id"])
            assert record["status"] == "APPROVED", record
            assert record["external_id"] in (
                bodies[0]["externalId"],
                bodies[1]["externalId"],
            ), record
            assert record["owner"] == VARIANT_OWNER, (
                f"요청이 변형 배포를 등록한 사용자({VARIANT_OWNER})가 아니라 "
                f"{record['owner']} 의 것으로 보인다"
            )
        print("ok: test-approval-gate.md#시나리오 2")


if __name__ == "__main__":
    asyncio.run(run())
