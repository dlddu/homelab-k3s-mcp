"""승인은 한 번만 쓰인다 — 호출마다 새 승인 요청이고 기존 externalId 는 재사용되지 않는다.

검증 시나리오: test-approval-gate.md#시나리오 7
실행 대상: primary

시나리오의 세 절 중 e2e 가 잴 수 있는 것은 요청-레코드 계약이다: 승인받아 실행한
뒤 같은 도구 호출을 반복하면 **새** 승인 요청이 만들어지고(externalId 재사용
없음), 같은 Secret 의 재조회도 새 승인 요청을 만든다 — 한 번 승인이 그 대화
내내 유효해지지 않는다. 「같은 승인 id 로 재시도하면 두 번째 실행은 거부된다」의
소비-한-번 단정은 시나리오의 자동화 필드가 Go 단위
(``gatekeeper_test.go::TestApprovalIsConsumedOnce``)에 배정한 대로 단위 층의
몫이다 — 바깥에서는 SUT 안의 Decision 을 같은 id 로 두 번 쓰게 만들 경로가 없다.

primary 가 이 파일의 배포다 — 변형 배포의 관측면(기록 프록시)이 필요 없고,
타임아웃 기본값(300초)이 댄스에 여유를 준다.
"""

from __future__ import annotations

import asyncio
import base64
import json
import subprocess

from _gatekeeper import decide, gatekeeper_url, get_request, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"

TARGET_MAP = "gk-ac7-configmap"
TARGET_SECRET = "gk-ac7-secret"
SECRET_VALUE = "gk-ac7-value-0f61b3c"

APPROVED_IDS: list[str] = []


def _apply(manifest: dict) -> None:
    subprocess.run(
        ["kubectl", "apply", "-f", "-"],
        input=json.dumps(manifest),
        text=True,
        check=True,
        capture_output=True,
    )


async def _new_pending(gate: str, marker: str):
    """같은 대상에 대한 새 승인 요청을 기다린다 — 이미 처리된 요청은 PENDING 목록에 없다."""
    return await wait_for_pending(gate, marker)


async def _approved_patch(session, gate: str):
    task = asyncio.create_task(
        session.call_tool(
            "resource_patch",
            {
                "apiVersion": "v1",
                "kind": "ConfigMap",
                "namespace": NAMESPACE,
                "name": TARGET_MAP,
                "patchType": "merge",
                "patch": {"data": {"key": "patched"}},
            },
        )
    )
    row = await _new_pending(gate, TARGET_MAP)
    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result
    APPROVED_IDS.append(row["id"])
    return result


async def _approved_secret_read(session, gate: str) -> str:
    task = asyncio.create_task(
        session.call_tool(
            "resource_get",
            {
                "apiVersion": "v1",
                "kind": "Secret",
                "namespace": NAMESPACE,
                "name": TARGET_SECRET,
            },
        )
    )
    row = await _new_pending(gate, TARGET_SECRET)
    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result
    APPROVED_IDS.append(row["id"])
    return result.content[0].text


async def run() -> None:
    url = base_url()
    _apply(
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {"name": TARGET_MAP, "namespace": NAMESPACE},
            "data": {"key": "initial"},
        }
    )
    _apply(
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {"name": TARGET_SECRET, "namespace": NAMESPACE},
            "stringData": {"token": SECRET_VALUE},
        }
    )
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- approval-gate/시나리오 7 (첫 승인·실행) ---")
            first = await _approved_patch(session, gate)
            first_record = get_request(gate, APPROVED_IDS[0])
            assert first_record["status"] == "APPROVED", first_record
            assert first_record["processedById"], first_record

            print("--- approval-gate/시나리오 7 (같은 호출의 재시도) ---")
            second = await _approved_patch(session, gate)
            second_record = get_request(gate, APPROVED_IDS[1])
            assert APPROVED_IDS[0] != APPROVED_IDS[1], "승인 요청이 재사용됐다"
            assert first_record["externalId"] != second_record["externalId"], (
                "재시도가 기존 externalId 를 재사용했다"
            )
            assert first.structuredContent["object"]["data"]["key"] == "patched"

            print("--- approval-gate/시나리오 7 (같은 Secret 의 재조회) ---")
            value = await _approved_secret_read(session, gate)
            encoded = base64.b64encode(SECRET_VALUE.encode()).decode()
            assert encoded in value, (
                "승인된 Secret 읽기가 값을 돌려주지 않았다 — apiserver 응답의 값은 "
                "base64 인코딩 그대로다(SUT 는 디코딩하지 않는다)"
            )
            third_record = get_request(gate, APPROVED_IDS[2])
            value_again = await _approved_secret_read(session, gate)
            assert encoded in value_again
            fourth_record = get_request(gate, APPROVED_IDS[3])
            assert APPROVED_IDS[2] != APPROVED_IDS[3], "재조회가 승인을 재사용했다"
            assert third_record["externalId"] != fourth_record["externalId"], (
                "같은 Secret 재조회가 externalId 를 재사용했다"
            )
    print("ok: test-approval-gate.md#시나리오 7")


if __name__ == "__main__":
    asyncio.run(run())
