"""모든 실패는 거부로 수렴한다 — 여덟 경로 전부가 도구 에러이고 k8s 는 안 움직인다.

검증 시나리오: test-approval-gate.md#시나리오 5
실행 대상: gatekeeper-variant

이 파일이 `gatekeeper-variant` 를 상대로 도는 이유는 그 배포만
`GATEKEEPER_TIMEOUT_SECONDS=5` 라 만료·타임아웃 경로를 초 단위로 관측할 수 있기
때문이다(primary 는 기본 300초다). 미설정·연결 실패 셋은 env 가 배포당이라
그 배포로는 만들 수 없어, `tests/k8s/kind/gate-broken-variant.yaml` 의 전용
배포 셋에 이 파일 자신의 포트포워드로 닿는다 — `github_commit_status_ac4.py`
가 `commit-status-variant` 에 닿는 것과 같은 형태다.

**409·5xx 를 만드는 수단.** 실물 gatekeeper 는 자기 고유 인덱스
(`Request_externalId_key`)와 자기 `httpx.InternalError` 로만 409·500 을 내고,
MCP 의 `randomExternalID` 는 16바이트 난수라 클라이언트가 스스로 충돌을 만들 수
없다. 그래서 `docs/e2e-mocking-policy.md` 의 「실환경 주입 판정」이 확정한 수단을
쓴다 — 실물 DB 에 **마커 한정** `BEFORE INSERT` 트리거를 걸어 실물 핸들러가 자기
오류 분기를 실제로 타게 한다. 그 판정의 조건 C1~C5 가 이 파일의 스펙이고, 각
조건이 어디서 지켜지는지는 아래 케이스 주석에 적었다. 특히 **C4(잔여 0)는 이
파일이 스스로 단언한다** — `injected()` 가 트리거를 걷고, 그 뒤 `injector_state`
가 `triggers: [] · shadow_rows: 0` 인지 케이스마다 확인한다.

**만료와 「판정 없음」이 두 경로인 이유.** 이 배포에서 클라이언트의 폴링 마감과
gatekeeper 의 만료는 같은 `GATEKEEPER_TIMEOUT_SECONDS` 에서 파생해 사실상 같은
순간에 닫히고, 어느 쪽이 먼저인지는 타이머 경주다(`approval_gate_ac4.py` (b) 가
같은 사실을 적는다). 그래서 둘을 **서로 다른 관측면**으로 가른다 — 「판정 없음」은
*클라이언트가* 판정 없이 마감했다는 것(거부 지연 ≥ 타임아웃, 판정 없음)이고,
`EXPIRED` 는 *gatekeeper 기록이* 만료로 전이했다는 것이다. 도구 에러의 문면은
그 둘 어디에도 쓰지 않는다.

**k8s 호출 0 의 관측.** 시나리오가 정한 대로 대상 객체의 불변으로 잰다 — 여덟
경우 모두 대상 ConfigMap 의 값이 호출 전 그대로여야 한다. 이것이 C5 가 요구하는
「거부가 대상 리소스를 바꾸지 않았음을 **클러스터에서** 관측」이기도 하다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess
import time

from mcp.shared.exceptions import McpError

from _gatekeeper import (
    INJECT_CONFLICT,
    INJECT_ERROR,
    count_requests,
    create_records,
    decide,
    gatekeeper_url,
    get_request,
    injected,
    injector_state,
    injector_url,
    trace_url,
    wait_for_pending,
)
from _helpers import base_url, open_session, port_forward, wait_for_healthz

NAMESPACE = "workload-test"

#: 호출 전 값. 어떤 경로로도 이 값이 바뀌면 게이트가 샌 것이다.
SENTINEL = "untouched"
#: 거부됐어야 할 호출이 성공했다면 대상에 남았을 값.
FORBIDDEN = "must-not-apply"

TARGET_REJECTED = "gk-ac5-rejected"
TARGET_EXPIRED = "gk-ac5-expired"
TARGET_NO_VERDICT = "gk-ac5-no-verdict"
TARGET_CONFLICT = "gk-ac5-conflict"
TARGET_SERVER_ERROR = "gk-ac5-server-error"
TARGET_UNREACHABLE = "gk-ac5-unreachable"
TARGET_NO_BASE_URL = "gk-ac5-no-base-url"
TARGET_NO_API_KEY = "gk-ac5-no-api-key"

#: 읽기 대조군이 읽는 비민감 대상. 게이트가 죽은 조건에서도 살아 있어야 한다.
TARGET_CONTROL = "gk-ac5-control"

ALL_TARGETS = (
    TARGET_REJECTED,
    TARGET_EXPIRED,
    TARGET_NO_VERDICT,
    TARGET_CONFLICT,
    TARGET_SERVER_ERROR,
    TARGET_UNREACHABLE,
    TARGET_NO_BASE_URL,
    TARGET_NO_API_KEY,
    TARGET_CONTROL,
)

#: 변형 SUT 의 타임아웃(gatekeeper-variant.yaml)과 「판정 없음」 지연의 하한.
VARIANT_TIMEOUT_SECONDS = 5
LATENCY_FLOOR = VARIANT_TIMEOUT_SECONDS - 0.5
EXPIRY_BUDGET = VARIANT_TIMEOUT_SECONDS + 8

BROKEN_NAMESPACE = "homelab-k3s-mcp-gate-broken"
#: 이 레인이 도는 동안 열려 있는 다른 포워드(_gatekeeper 8095·8096, ci.yml 의 그룹
#: 포워드 8080·8088·8089·8090·8092·8093, github_commit_status_ac4 18084,
#: _session_platform 18083, platform_auth_safety 18080-18082)와 겹치지 않는 자리.
BROKEN_VARIANTS = {
    TARGET_UNREACHABLE: ("homelab-k3s-mcp-gate-unreachable", 18085),
    TARGET_NO_BASE_URL: ("homelab-k3s-mcp-gate-no-base-url", 18086),
    TARGET_NO_API_KEY: ("homelab-k3s-mcp-gate-no-api-key", 18087),
}


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


def assert_target_untouched(name: str, path: str) -> None:
    """C5 — 거부가 대상 리소스를 바꾸지 않았음을 클러스터에서 직접 읽어 확인한다."""
    value = _config_map_value(name)
    assert value == SENTINEL, (
        f"{path}: 거부됐는데 대상 ConfigMap {name} 의 값이 {value!r} 다 "
        f"(호출 전 {SENTINEL!r}) — 게이트를 지나 k8s 호출이 나갔다"
    )


async def _patch(session, name: str, value: str = FORBIDDEN):
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


async def refuse(session, name: str, path: str, budget: float = 30.0) -> str:
    """게이트 대상 호출 하나가 **거부**되는 것을 확인하고 그 문면을 돌려준다.

    거부는 도구 결과가 아니라 JSON-RPC 에러로 온다(`internal/mcp/mcp.go` 의
    `gateRefusal` → `errf(-32603, "%s", err)`), 그래서 `McpError` 로 잡는다.
    """
    try:
        result = await asyncio.wait_for(
            asyncio.create_task(_patch(session, name)), timeout=budget
        )
    except asyncio.TimeoutError as exc:
        raise AssertionError(
            f"{path}: 호출이 {budget}초 안에 돌아오지 않았다 — 거부로 수렴하지 않는다"
        ) from exc
    except McpError as exc:
        return str(exc)
    raise AssertionError(f"{path}: 거부되지 않았다 — {result}")


# --- (1) REJECTED -----------------------------------------------------------

async def test_rejected_verdict_refuses(session, gate) -> None:
    """사람이 거절하면 거부된다."""
    task = asyncio.create_task(_patch(session, TARGET_REJECTED))
    row = await wait_for_pending(gate, TARGET_REJECTED)
    await decide(gate, row["id"], "REJECTED")
    try:
        result = await asyncio.wait_for(task, timeout=EXPIRY_BUDGET)
    except McpError:
        pass
    else:
        raise AssertionError(f"REJECTED 인데 거부되지 않았다: {result}")

    record = get_request(gate, row["id"])
    assert record["status"] == "REJECTED", record
    assert_target_untouched(TARGET_REJECTED, "REJECTED")
    print("(1) ok: REJECTED → 도구 에러, 대상 불변")


# --- (2) EXPIRED ------------------------------------------------------------

async def test_expired_record_refuses(session, gate) -> None:
    """판정을 내리지 않으면 gatekeeper 기록이 EXPIRED 로 전이하고 호출은 거부된다.

    여기서 단언하는 것은 **기록 쪽 얼굴**이다(만료 평가가 조회 시점에 일어나는 것은
    실물에서만 관측된다). 클라이언트 쪽 얼굴은 (3) 이 잰다.
    """
    task = asyncio.create_task(_patch(session, TARGET_EXPIRED))
    row = await wait_for_pending(gate, TARGET_EXPIRED)
    try:
        result = await asyncio.wait_for(task, timeout=EXPIRY_BUDGET)
    except McpError:
        pass
    else:
        raise AssertionError(f"판정 없는 호출이 거부되지 않았다: {result}")

    record = get_request(gate, row["id"])
    assert record["status"] == "EXPIRED", (
        f"기록이 {record['status']} 다 — 만료로 전이하지 않았다"
    )
    assert_target_untouched(TARGET_EXPIRED, "EXPIRED")
    print("(2) ok: 기록 EXPIRED → 도구 에러, 대상 불변")


# --- (3) 타임아웃 내 판정 없음 ------------------------------------------------

async def test_no_verdict_within_timeout_refuses(session, gate) -> None:
    """클라이언트가 판정 없이 마감한다 — 거부가 타임아웃을 **기다린 뒤에** 온다.

    최초 응답만 보고 끝낸 구현이라면 즉시 거부했을 것이므로, 지연 하한이 이 경로의
    계약을 진다. 승인도 거절도 하지 않으므로 어떤 판정도 관측되지 않아야 한다.
    """
    started = time.monotonic()
    task = asyncio.create_task(_patch(session, TARGET_NO_VERDICT))
    row = await wait_for_pending(gate, TARGET_NO_VERDICT)
    try:
        result = await asyncio.wait_for(task, timeout=EXPIRY_BUDGET)
    except McpError:
        pass
    else:
        raise AssertionError(f"판정 없는 호출이 거부되지 않았다: {result}")
    wall = time.monotonic() - started

    assert wall >= LATENCY_FLOOR, (
        f"거부가 {wall:.1f}초에 왔다 — 타임아웃 {VARIANT_TIMEOUT_SECONDS}초를 "
        "기다리지 않았다(최초 응답만 보고 끝냈을 수 있다)"
    )
    record = get_request(gate, row["id"])
    assert record["status"] != "APPROVED", (
        f"아무도 승인하지 않았는데 기록이 {record['status']} 다"
    )
    assert record.get("processedById") in (None, ""), (
        f"판정한 사람이 없어야 하는데 처리자가 {record.get('processedById')!r} 다"
    )
    assert_target_untouched(TARGET_NO_VERDICT, "판정 없음")
    print(f"(3) ok: 판정 없이 {wall:.1f}초(≥{LATENCY_FLOOR}) 뒤 거부, 대상 불변")


# --- (4) externalId 충돌 (409) ----------------------------------------------

async def test_external_id_conflict_refuses(session, gate, inject, trace) -> None:
    """409 — 실물 고유 인덱스 위반이 실물 핸들러의 409 를 내고 거부로 수렴한다.

    C1: status·본문은 실물 `store.isUniqueViolation` → 실물 핸들러가 만든다. 주입은
    충돌이라는 **전제**만 만들고 응답을 흉내 내지 않는다. C2: 트리거는 이 대상
    이름(마커)이 context 에 든 INSERT 에만 걸린다. C4: 아래에서 잔여 0 을 단언한다.

    「진짜 409 였다」의 증거 둘 — ⑴ 클라이언트가 본 문면이 `(409)` 이고, ⑵ 그 마커의
    요청이 gatekeeper 에 **한 건도 저장되지 않았다**(INSERT 가 실제로 실패했다).
    프록시 기록에는 create 시도가 남으므로 「보내지도 않았다」와 구별된다.
    """
    with injected(inject, INJECT_CONFLICT, TARGET_CONFLICT):
        message = await refuse(session, TARGET_CONFLICT, "409")

    assert "409" in message, f"409 경로의 거부 문면에 409 가 없다: {message}"
    assert count_requests(gate, TARGET_CONFLICT) == 0, (
        "충돌했는데 요청이 저장됐다 — INSERT 가 실패하지 않았다"
    )
    attempted = [
        r for r in create_records(trace)
        if TARGET_CONFLICT in json.dumps(r.get("body", {}), ensure_ascii=False)
    ]
    assert attempted, (
        "프록시에 create 시도가 없다 — 409 가 아니라 요청 자체를 보내지 않았다"
    )
    assert_target_untouched(TARGET_CONFLICT, "409")

    residue = injector_state(inject)
    assert residue["triggers"] == [] and residue["shadow_rows"] == 0, (
        f"C4 위반 — 주입 잔여가 남았다: {residue}"
    )
    print("(4) ok: 실물 409 → 도구 에러, 저장 0건, 대상 불변, 잔여 0")


# --- (5) 5xx ----------------------------------------------------------------

async def test_server_error_refuses(session, gate, inject) -> None:
    """5xx — 실물 저장소 오류가 실물 `InternalError` 를 내고 거부로 수렴한다.

    409 와 같은 수단이지만 트리거가 `RAISE(ABORT, …)` 라 고유 인덱스가 아니라
    저장소 오류 분기를 탄다. 두 경로가 **서로 다른 문면**으로 갈리는 것이 「5xx 가
    409 로 뭉뚱그려지지 않는다」의 관측면이다.
    """
    with injected(inject, INJECT_ERROR, TARGET_SERVER_ERROR):
        message = await refuse(session, TARGET_SERVER_ERROR, "5xx")

    assert "500" in message, f"5xx 경로의 거부 문면에 500 이 없다: {message}"
    assert "409" not in message, (
        f"5xx 인데 문면이 409 로 갈렸다: {message}"
    )
    assert count_requests(gate, TARGET_SERVER_ERROR) == 0, (
        "저장소 오류였는데 요청이 저장됐다"
    )
    assert_target_untouched(TARGET_SERVER_ERROR, "5xx")

    residue = injector_state(inject)
    assert residue["triggers"] == [] and residue["shadow_rows"] == 0, (
        f"C4 위반 — 주입 잔여가 남았다: {residue}"
    )
    print("(5) ok: 실물 500 → 도구 에러, 저장 0건, 대상 불변, 잔여 0")


# --- (6)(7)(8) 연결 실패 · 두 미설정 ------------------------------------------

async def _refuse_on_broken_variant(target: str, path: str, expected: str) -> None:
    """게이트가 죽은 배포에서 ⑴ 쓰기는 거부되고 ⑵ 읽기 대조군은 살아 있다.

    셋 다 `gatekeeper.FromEnv` 의 서로 다른 분기에서 나오고, `main.go` 가 그것을
    `NewUnavailable` 로 바꿔 **`Describe` 를 부르지 않고** 거부한다. 그 「부르지
    않음」이 AC5 의 핵심이라 대상 불변을 함께 잰다 — 사전 읽기조차 없었다면 대상은
    당연히 그대로다.
    """
    service, local_port = BROKEN_VARIANTS[target]
    with port_forward(
        BROKEN_NAMESPACE, service, 3000, local_port, ready_path="/healthz"
    ) as url:
        wait_for_healthz(url)
        async with open_session(url) as session:
            message = await refuse(session, target, path)
            assert expected in message, (
                f"{path}: 거부 문면이 기대와 다르다 — {expected!r} 를 찾지 못했다: {message}"
            )
            assert_target_untouched(target, path)
            await assert_reads_survive(session, path)
    print(f"({path}) ok: 거부 문면 확인, 대상 불변, 읽기 대조군 정상")


async def assert_reads_survive(session, path: str) -> None:
    """시나리오의 읽기 대조군 — 같은 조건에서 `resource_list` 와 비민감
    `resource_get` 은 정상 동작한다.

    「승인 경로가 죽었는데 변경이 나가면 게이트가 있으나 마나」의 반대편이다:
    죽은 게이트가 **읽기까지** 막으면 그것도 계약 위반이다.
    """
    listed = await session.call_tool(
        "resource_list",
        {"apiVersion": "v1", "kind": "ConfigMap", "namespace": NAMESPACE},
    )
    assert listed.isError is False, f"{path}: 게이트가 죽자 resource_list 까지 막혔다: {listed}"

    got = await session.call_tool(
        "resource_get",
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "name": TARGET_CONTROL,
        },
    )
    assert got.isError is False, (
        f"{path}: 게이트가 죽자 비민감 resource_get 까지 막혔다: {got}"
    )


async def test_unreachable_gate_refuses() -> None:
    """(6) 연결 실패 — 백엔드에 닿지 못해도 변경은 전부 멈춘다."""
    await _refuse_on_broken_variant(TARGET_UNREACHABLE, "연결 실패", "refusing")


async def test_missing_base_url_refuses() -> None:
    """(7) `GATEKEEPER_BASE_URL` 미설정 — `FromEnv` 의 그 분기 문면이 그대로 온다."""
    await _refuse_on_broken_variant(
        TARGET_NO_BASE_URL, "BASE_URL 미설정", "GATEKEEPER_BASE_URL is not"
    )


async def test_missing_api_key_refuses() -> None:
    """(8) `GATEKEEPER_API_KEY` 미설정 — 반대쪽 분기 문면이 그대로 온다."""
    await _refuse_on_broken_variant(
        TARGET_NO_API_KEY, "API_KEY 미설정", "GATEKEEPER_API_KEY is not"
    )


async def run() -> None:
    url = base_url()
    for name in ALL_TARGETS:
        _config_map(name, SENTINEL)
    wait_for_healthz(url)

    with gatekeeper_url() as gate, trace_url() as trace, injector_url() as inject:
        start = injector_state(inject)
        assert start["triggers"] == [] and start["shadow_rows"] == 0, (
            f"시작부터 주입 잔여가 있다 — 앞 실행이 걷지 않았다: {start}"
        )
        async with open_session(url) as session:
            print("--- approval-gate/시나리오 5 (1) REJECTED ---")
            await test_rejected_verdict_refuses(session, gate)
            print("--- approval-gate/시나리오 5 (2) EXPIRED ---")
            await test_expired_record_refuses(session, gate)
            print("--- approval-gate/시나리오 5 (3) 판정 없음 ---")
            await test_no_verdict_within_timeout_refuses(session, gate)
            print("--- approval-gate/시나리오 5 (4) 409 ---")
            await test_external_id_conflict_refuses(session, gate, inject, trace)
            print("--- approval-gate/시나리오 5 (5) 5xx ---")
            await test_server_error_refuses(session, gate, inject)
            print("--- approval-gate/시나리오 5 (읽기 대조군, 살아 있는 게이트) ---")
            await assert_reads_survive(session, "살아 있는 게이트")

        print("--- approval-gate/시나리오 5 (6) 연결 실패 ---")
        await test_unreachable_gate_refuses()
        print("--- approval-gate/시나리오 5 (7) BASE_URL 미설정 ---")
        await test_missing_base_url_refuses()
        print("--- approval-gate/시나리오 5 (8) API_KEY 미설정 ---")
        await test_missing_api_key_refuses()

        end = injector_state(inject)
        assert end["triggers"] == [] and end["shadow_rows"] == 0, (
            f"C4 위반 — 실행 뒤 주입 잔여가 남았다: {end}"
        )
    print("ok: test-approval-gate.md#시나리오 5")


if __name__ == "__main__":
    asyncio.run(run())
