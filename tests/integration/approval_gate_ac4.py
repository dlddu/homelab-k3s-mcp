"""폴링으로 판정을 관측한다 — PENDING→APPROVED 전이와 만료 관측이 실물에서 온다.

검증 시나리오: test-approval-gate.md#시나리오 4
실행 대상: gatekeeper-variant

(a) 승인 댄스: 도구 호출이 블록한 채 승인 요청을 만들고, (e2e 의) 폴링이 PENDING
전이를 보고, 승인 뒤 실행이 이어진다. 클라이언트 자신이 폴링했다는 증거는
upstream 기록에 남지 않으므로 기록 프록시의 GET 카운트로 잰다 — 「폴링을 끊고
최초 응답만 본 구현은 이 시나리오를 통과하지 못함」의 관측면이다.

(b) 만료: 아무 판정도 하지 않으면 gatekeeper 는 **조회 시점에** 만료를 평가한다.
만료 순간 폴링이 EXPIRED 상태 문자열을 보는 것은 이 배포에서 관측 불가다 —
클라이언트의 폴링 마감과 gatekeeper 의 만료가 같은 env
(GATEKEEPER_TIMEOUT_SECONDS)에서 파생해 정확히 같은 순간에 닫히고, 둘 중 무엇이
이기는지는 타이머 경주라 단언의 재료가 아니다. 그래서 만료는 세 가지로 잰다:
⑴ 거부가 만료 근처(≥ 타임아웃)에 온다 — 최초 응답만 보고 끝낸 구현이라면
즉시 거부했을 것이다 ⑵ e2e 의 조회로 기록이 EXPIRED 로 전이된다 — 만료 평가가
조회 시점임이 실물에서만 관측되는 부분이다 ⑶ 프록시 기록에 폴링이 여러 번
찍힌다. 도구 에러 문면은 이 셋 어디에도 쓰지 않는다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess
import time

from mcp.shared.exceptions import McpError

from _gatekeeper import (
    decide,
    gatekeeper_url,
    get_request,
    poll_count,
    trace_url,
    wait_for_pending,
)
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"

TARGET_APPROVED = "gk-ac4-approved"
TARGET_EXPIRED = "gk-ac4-expired"

#: 변형 SUT 의 타임아웃(gatekeeper-variant.yaml)과 (b) 거부 지연의 하한.
VARIANT_TIMEOUT_SECONDS = 5
LATENCY_FLOOR = VARIANT_TIMEOUT_SECONDS - 0.5
#: (b) 의 관측 예산 — 거부가 T 초에 오므로 그 위로 조금 더 여유를 둔다.
EXPIRY_BUDGET = VARIANT_TIMEOUT_SECONDS + 8


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


def _config_map_value(name: str) -> str:
    out = subprocess.check_output(
        [
            "kubectl", "-n", NAMESPACE, "get", "configmap", name,
            "-o", "jsonpath={.data.key}",
        ],
        text=True,
    )
    return out.strip()


async def _patch(session, name: str, value: str):
    return await session.call_tool(
        "resource_patch",
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "name": name,
            "patchType": "merge",
            "patch": {"data": {"key": value}},
        },
    )


async def test_approved_verdict_is_observed_by_polling(session, gate, trace) -> None:
    """(a) PENDING→APPROVED 전이가 폴링으로 관측되고 실행이 이어진다."""
    task = asyncio.create_task(_patch(session, TARGET_APPROVED, "approved"))
    row = await wait_for_pending(gate, TARGET_APPROVED)
    assert row["status"] == "PENDING", row
    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result

    record = get_request(gate, row["id"])
    assert record["status"] == "APPROVED", record
    polls = poll_count(trace, row["id"])
    assert polls >= 2, (
        f"승인 요청 {row['id']} 에 대한 클라이언트 폴링이 {polls} 회뿐이다 — "
        "최초 응답만 본 구현이다"
    )
    assert _config_map_value(TARGET_APPROVED) == "approved"
    print(f"(a) ok: PENDING→APPROVED, 폴링 {polls}회, 실행 이어짐")


async def test_expired_verdict_is_observed(session, gate, trace) -> None:
    """(b) 아무 판정도 없으면 거부가 만료 근처에 오고 기록은 EXPIRED 로 읽힌다.

    거부는 도구 결과가 아니라 JSON-RPC 에러로 온다(`internal/mcp/gate.go` 의
    `errf(-32603)`), 그래서 `McpError` 로 잡는다. 그 **문면은 단정하지 않는다** —
    클라이언트의 폴링 마감과 gatekeeper 의 만료가 같은 `GATEKEEPER_TIMEOUT_SECONDS`
    에서 파생해 어느 쪽이 먼저 닫는지가 타이 레이스이고, 그에 따라
    「approval expired」 와 「no approval within …」 가 갈린다. 둘 다 정당한 거부라
    아래의 지연·기록·폴링 수가 이 절의 계약을 진다.
    """
    started = time.monotonic()
    task = asyncio.create_task(_patch(session, TARGET_EXPIRED, "must-not-apply"))
    row = await wait_for_pending(gate, TARGET_EXPIRED)
    try:
        result = await asyncio.wait_for(task, timeout=EXPIRY_BUDGET)
    except asyncio.TimeoutError as exc:
        raise AssertionError(
            f"판정 없는 호출이 {EXPIRY_BUDGET}초 안에도 돌아오지 않았다 — "
            "폴링 마감이 만료를 따라가지 못했다"
        ) from exc
    except McpError:
        pass
    else:
        raise AssertionError(f"판정 없는 호출이 거부되지 않았다: {result}")
    wall = time.monotonic() - started
    assert wall >= LATENCY_FLOOR, (
        f"거부가 {wall:.1f}초에 왔다 — 타임아웃 {VARIANT_TIMEOUT_SECONDS}초를 "
        "기다리지 않은 구현이다(최초 응답만 보고 끝냈을 수 있다)"
    )

    record = get_request(gate, row["id"])
    assert record["status"] == "EXPIRED", (
        f"기록이 {record['status']}다 — 만료 평가가 조회 시점에 일어나지 않았다"
    )
    polls = poll_count(trace, row["id"])
    assert polls >= 2, f"만료까지 폴링이 {polls} 회뿐이다"
    assert _config_map_value(TARGET_EXPIRED) != "must-not-apply"
    print(f"(b) ok: 거부 {wall:.1f}초(≥{LATENCY_FLOOR}), 기록 EXPIRED, 폴링 {polls}회")


async def run() -> None:
    url = base_url()
    _config_map(TARGET_APPROVED, "one")
    _config_map(TARGET_EXPIRED, "one")
    wait_for_healthz(url)

    with gatekeeper_url() as gate, trace_url() as trace:
        async with open_session(url) as session:
            print("--- approval-gate/시나리오 4 (a) 승인 전이 관측 ---")
            await test_approved_verdict_is_observed_by_polling(session, gate, trace)
            print("--- approval-gate/시나리오 4 (b) 만료 관측 ---")
            await test_expired_verdict_is_observed(session, gate, trace)
    print("ok: test-approval-gate.md#시나리오 4")


if __name__ == "__main__":
    asyncio.run(run())
