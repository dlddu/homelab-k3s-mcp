"""서브리소스 조회 — 로그·tail 절단·직전 인스턴스·컨테이너 후보·스케일이 전부 `get` 하나로 온다.

검증 시나리오: test-resource-generic.md#시나리오 5
실행 대상: primary

폐기된 `workload_logs_ac{1,2,3,4}.py` 의 단언을 승계하되, 그 파일들이 **단정하지 못한 채
남겨 둔 한 절**을 이번에 닫는다: 「파드에 컨테이너가 둘 이상이면 `container` 가 필요하다」.
그때 막고 있던 것은 둘이었고 지금은 둘 다 없다 — ⑴ 러닝 다중 컨테이너 파드가 없었고(이번
슬라이스가 `resource-generic-fixture.yaml` 에 세운다), ⑵ 기존 픽스처에 컨테이너를 더하는 길은
`workload_logs_ac2.py` 가 **`container` 없는 호출의 성공을 단정**하고 있어 막혀 있었다. 그
파일은 사라졌지만 결론(별도 워크로드 신설)은 그대로 승계했다.

로그 축이 기준선 파드가 아니라 `resource-generic-multi` 를 상대하는 이유는 그 픽스처 머리말에
있다. 여기서 정한 것은 **어떻게 재는가**다 — 절단은 개수만이 아니라 **어느 줄이 남았는지**로
잰다. 개수만 보면 앞에서 5줄을 잘라 오는 구현도 통과한다.

**「클램프가 아니라 거부」는 거부 하나만 봐서는 성립하지 않는다**(시나리오 3 이 같은 자리에서
고정한 모양이다). 인자를 아예 못 받는 구현도 그 단독 관측을 통과하므로, 상한값 `5000` 이
**정상 응답**임을 나란히 보여 거부가 상한을 넘긴 것에 대한 것임을 성립시킨다. 상한 초과는
도구가 k8s 에 닿기 전에 인자 검증으로 거부하므로 도구 결과가 아니라 `McpError`(-32602) 로 온다.

**마지막 절(「승인 요청이 생기지 않음」)은 성공만 봐서는 공허하다** — 게이트를 아예 달지 않은
서버도 여섯 호출을 통과시킨다. 그래서 같은 세션에서 게이트 대상 호출(`v1/Secret` 읽기)이
거부되는 것을 대조군으로 나란히 본다. ⚠️ 이 배포에는 gatekeeper 가 없어 그 거부 문면이
「approval gate is not configured」다. 실물 gatekeeper 픽스처가 서면(시나리오 16 의 선행,
`docs/doc-tracker.md` 구현 대기 표) 그 문면이 판정 결과로 바뀌므로 이 대조군도 그때 함께 옮긴다.

선행 조건(픽스처 파드 기동·크래시루프 재시작·기준선 레플리카)은 도구가 아니라 kubectl 로
세운다 — 전제를 검증 대상 자신으로 세우면 「픽스처가 아직 안 섰다」와 「서브리소스 조회가
틀렸다」가 구분되지 않는다.
"""

from __future__ import annotations

import asyncio
import subprocess
import time

from mcp import ClientSession
from mcp.shared.exceptions import McpError

from _helpers import base_url, open_session, wait_for_healthz
from _workload import (
    CRASHLOOP_MARKER,
    CRASHLOOP_WORKLOAD,
    FIXTURE_REPLICAS,
    NAMESPACE,
    RECOVERED_MARKER,
    WORKLOAD,
    ensure_workload_fixture_baseline,
    wait_for_crashloop_log_age,
    wait_for_crashloop_restart,
)

#: `tests/k8s/kind/resource-generic-fixture.yaml` 이 세우는 다중 컨테이너 워크로드.
MULTI_WORKLOAD = "resource-generic-multi"

#: 같은 파일이 그 파드에 선언하는 컨테이너 이름. 순서까지 매니페스트와 같다 —
#: apiserver 의 후보 목록이 spec 순서를 따른다.
MULTI_CONTAINERS = ("chatty", "quiet")

#: 로그를 내는 쪽. 나머지 하나는 `pause` 라 한 줄도 내지 않는다.
LOG_CONTAINER = "chatty"

#: `chatty` 가 찍는 줄 수. 픽스처의 `seq 1 40` 과 같은 값이어야 한다.
LOG_LINE_COUNT = 40

#: `internal/mcp/resource.go` 의 `logsDefaultTailLines` — `tailLines` 미지정의 기본값.
DEFAULT_TAIL_LINES = 200

#: 같은 파일의 `logsMaxTailLines` 와 그것을 넘긴 값. 넘긴 쪽은 거부돼야 한다.
MAX_TAIL_LINES = 5000
OVER_MAX_TAIL_LINES = 5001

#: 절단이 관측되는 작은 값. 모집단보다 작아야 의미가 있다.
SMALL_TAIL_LINES = 5


def _log_line(n: int) -> str:
    return f"chatty line {n}"


def _pod_name(selector: str) -> str:
    out = subprocess.check_output(
        [
            "kubectl", "-n", NAMESPACE, "get", "pod", "-l", selector,
            "-o", "jsonpath={.items[0].metadata.name}",
        ],
        text=True,
    ).strip()
    assert out, f"{NAMESPACE} 에 {selector} 파드가 없다"
    return out


def _lines(text: str) -> list[str]:
    return [line for line in text.splitlines() if line]


def test_resource_generic_ac5_precondition_multi_container_pod(
    timeout: float = 180.0,
) -> str:
    """시나리오 5 사전 조건 — 다중 컨테이너 파드가 서 있고 로그 모집단이 선언대로다.

    셋을 함께 건다. ⑴ 컨테이너가 **둘**이다 ⑵ 모집단이 `LOG_LINE_COUNT` 줄이다 ⑶ 재시작이
    없다. 셋 다 무너져도 케이스는 **조용히 통과한다** — ⑴ 이 무너지면 거부가 일어나지 않아
    후보 단언에 닿지 못하고, ⑵ 가 `SMALL_TAIL_LINES` 이하로 떨어지면 절단 단언이 공전하며,
    ⑶ 이 무너지면 줄 수가 관측마다 달라져 ⑵ 가 흔들린다. 그래서 여기서 큰 소리로 세운다.
    """
    assert LOG_LINE_COUNT > SMALL_TAIL_LINES, (
        f"픽스처 {LOG_LINE_COUNT}줄이 tail {SMALL_TAIL_LINES} 보다 크지 않다 "
        f"— 절단이 관측되지 않아 이 시나리오가 공전한다"
    )
    deadline = time.monotonic() + timeout
    last = "<no probe yet>"
    while time.monotonic() < deadline:
        probe = subprocess.run(
            [
                "kubectl", "-n", NAMESPACE, "get", "pod",
                "-l", f"app={MULTI_WORKLOAD}",
                "-o", "jsonpath={.items[0].metadata.name}|"
                "{range .items[0].spec.containers[*]}{.name},{end}|"
                "{range .items[0].status.containerStatuses[*]}"
                "{.ready}{.restartCount},{end}",
            ],
            capture_output=True,
            text=True,
        )
        last = probe.stdout.strip() or probe.stderr.strip()
        if probe.returncode == 0 and probe.stdout.count("|") == 2:
            pod, containers, statuses = probe.stdout.split("|")
            names = tuple(name for name in containers.split(",") if name)
            ready = [entry for entry in statuses.split(",") if entry]
            if pod and names == MULTI_CONTAINERS and ready == ["true0"] * len(names):
                logs = subprocess.check_output(
                    ["kubectl", "-n", NAMESPACE, "logs", pod, "-c", LOG_CONTAINER],
                    text=True,
                )
                observed = _lines(logs)
                assert len(observed) == LOG_LINE_COUNT, (
                    f"{LOG_CONTAINER} 가 {LOG_LINE_COUNT}줄이 아니라 {len(observed)}줄을 찍었다 "
                    f"— 픽스처와 LOG_LINE_COUNT 가 어긋났다"
                )
                assert observed[-1] == _log_line(LOG_LINE_COUNT), observed[-3:]
                print(
                    f"precondition ok: {pod} containers={names} "
                    f"logs={len(observed)}줄 (재시작 0)"
                )
                return pod
        time.sleep(3)
    raise RuntimeError(
        f"{NAMESPACE} 의 app={MULTI_WORKLOAD} 파드가 {timeout:.0f}s 안에 "
        f"컨테이너 {MULTI_CONTAINERS} 전부 Ready(재시작 0) 가 되지 않았다 "
        f"(마지막 관측: {last!r})"
    )


async def _logs(session: ClientSession, pod: str, **extra) -> tuple[dict, str]:
    result = await session.call_tool(
        "resource_get",
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "namespace": NAMESPACE,
            "name": pod,
            "subresource": "log",
            **extra,
        },
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload is not None, result
    assert payload["subresource"] == "log", payload
    assert payload["resource"] == "pods/log", payload
    return payload, result.content[0].text


async def test_resource_generic_ac5_log_returns_recent_lines(
    session: ClientSession, pod: str
) -> None:
    """시나리오 5 — `subresource=log` 가 최근 로그를 돌려준다.

    「돌려준다」를 비어 있지 않음이 아니라 **모집단 전체**로 건다. 기본값(`tailLines` 200)이
    모집단(40)보다 크므로 전집이 와야 하고, 그래야 다음 케이스의 절단이 무엇에 대한 절단인지
    기준을 갖는다. 구조화 응답과 사람이 읽는 본문 양쪽을 보는 것은, 도구를 쓰는 쪽이 구조화
    필드를 보지 않아도 같은 로그를 읽는다는 것이 이 도구의 계약이라서다.
    """
    assert LOG_LINE_COUNT < DEFAULT_TAIL_LINES, (
        f"모집단 {LOG_LINE_COUNT}줄이 기본값 {DEFAULT_TAIL_LINES} 이상이라 "
        f"기본 호출이 이미 잘린다 — 전집 단언이 성립하지 않는다"
    )
    payload, text = await _logs(session, pod, container=LOG_CONTAINER)

    lines = _lines(payload["text"])
    assert len(lines) == LOG_LINE_COUNT, (
        f"기본 호출이 전집 {LOG_LINE_COUNT}줄이 아니다: {len(lines)}줄"
    )
    assert lines[0] == _log_line(1), lines[:3]
    assert lines[-1] == _log_line(LOG_LINE_COUNT), lines[-3:]
    assert _lines(text) == lines, "본문과 구조화 응답이 다른 로그를 담고 있다"
    print(f"log ok: {len(lines)}줄 ({lines[0]} … {lines[-1]})")


async def test_resource_generic_ac5_tail_lines_actually_truncates(
    session: ClientSession, pod: str
) -> None:
    """시나리오 5 — `tailLines=5` 가 라인 수를 실제로 줄이고, 남는 것은 **끝** 5줄이다.

    개수만 보면 앞에서 5줄을 잘라 오는 구현도 통과한다. `tail` 이 약속하는 것은 개수가 아니라
    **어느 쪽 끝인지**이므로 남은 줄의 이름까지 본다.
    """
    payload, _ = await _logs(
        session, pod, container=LOG_CONTAINER, tailLines=SMALL_TAIL_LINES
    )

    lines = _lines(payload["text"])
    assert len(lines) == SMALL_TAIL_LINES, (
        f"tailLines={SMALL_TAIL_LINES} 인데 {len(lines)}줄이 왔다 — 줄지 않았다"
    )
    expected = [
        _log_line(n)
        for n in range(LOG_LINE_COUNT - SMALL_TAIL_LINES + 1, LOG_LINE_COUNT + 1)
    ]
    assert lines == expected, f"끝 {SMALL_TAIL_LINES}줄이 아니다: {lines}"
    print(f"tail ok: {LOG_LINE_COUNT} → {len(lines)}줄 ({lines[0]} … {lines[-1]})")


async def test_resource_generic_ac5_over_max_tail_is_rejected_not_clamped(
    session: ClientSession, pod: str
) -> None:
    """시나리오 5 — `tailLines=5001` 은 클램프되지 않고 거부되며, 상한값 자체는 정상 응답이다."""
    payload, _ = await _logs(
        session, pod, container=LOG_CONTAINER, tailLines=MAX_TAIL_LINES
    )
    assert len(_lines(payload["text"])) == LOG_LINE_COUNT, (
        f"상한값 호출이 전집을 돌려주지 않았다: {len(_lines(payload['text']))}줄"
    )

    try:
        await _logs(
            session, pod, container=LOG_CONTAINER, tailLines=OVER_MAX_TAIL_LINES
        )
    except McpError as exc:
        message = str(exc)
        assert f"tailLines must be <= {MAX_TAIL_LINES}" in message, message
        print(f"over-max rejection ok: {message}")
    else:
        raise AssertionError(
            f"tailLines={OVER_MAX_TAIL_LINES} 가 거부되지 않았다 — 클램프됐을 수 있다"
        )


async def test_resource_generic_ac5_previous_returns_the_terminated_instance(
    session: ClientSession,
) -> None:
    """시나리오 5 — 크래시 루프 파드의 `previous=true` 가 **직전** 인스턴스 로그를 돌려준다.

    직전 인스턴스가 찍는 마커는 지금 인스턴스가 찍는 것과 달라, 라이브 로그를 읽어 통과할 수
    없다. 그 구분을 실제로 세우려고 `previous` 없는 호출을 대조군으로 나란히 둔다 — 두 호출의
    답이 **서로를 배제**해야 「직전」이 관측된 것이다.
    """
    wait_for_crashloop_restart()
    wait_for_crashloop_log_age(5.0)
    pod = _pod_name(f"app={CRASHLOOP_WORKLOAD}")

    previous, previous_text = await _logs(session, pod, previous=True)
    assert CRASHLOOP_MARKER in previous["text"], previous["text"]
    assert RECOVERED_MARKER not in previous["text"], previous["text"]
    assert CRASHLOOP_MARKER in previous_text, previous_text

    live, _ = await _logs(session, pod)
    assert RECOVERED_MARKER in live["text"], live["text"]
    assert CRASHLOOP_MARKER not in live["text"], live["text"]

    print(f"previous ok: {pod} 직전 인스턴스 마커 관측, 라이브와 서로 배제")


async def test_resource_generic_ac5_missing_container_is_refused_with_candidates(
    session: ClientSession, pod: str
) -> None:
    """시나리오 5 — 다중 컨테이너 파드에 `container` 없이 부르면 거부되고 후보가 제시된다.

    거부만으로는 부족하다. 시나리오가 요구하는 것은 **부르는 쪽이 다음에 무엇을 적어야 하는지**
    알 수 있다는 것이라, 거부 문면에 컨테이너 이름이 **전부** 실려야 한다. 이름을 하나 골라 부른
    호출이 성공하는 것을 나란히 봐야, 거부가 이름 누락 때문이지 이 파드를 못 읽어서가 아님이
    성립한다.
    """
    refused = await session.call_tool(
        "resource_get",
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "namespace": NAMESPACE,
            "name": pod,
            "subresource": "log",
        },
    )
    assert refused.isError, refused
    message = refused.content[0].text
    for name in MULTI_CONTAINERS:
        assert name in message, f"거부 문면에 후보 {name!r} 이 없다: {message}"
    print(f"missing-container refusal ok: {message}")

    payload, _ = await _logs(session, pod, container=LOG_CONTAINER)
    assert _lines(payload["text"]), "이름을 준 호출까지 비어 있다 — 거부가 이름 때문이 아니다"
    print(f"named-container call ok: container={LOG_CONTAINER}")


async def test_resource_generic_ac5_scale_returns_current_replicas(
    session: ClientSession,
) -> None:
    """시나리오 5 — `subresource=scale` 이 현재 레플리카를 돌려준다.

    레플리카 수만으로 재면 객체 전문을 그대로 돌려주는 구현도 통과한다(Deployment 전문에도
    `spec.replicas` 가 있다). 그래서 돌아온 것이 `Scale` 이라는 것과 Deployment 전문에만 있는
    `spec.template` 이 **없다**는 것을 함께 본다.
    """
    replicas = await session.call_tool(
        "resource_get",
        {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": WORKLOAD,
            "subresource": "scale",
        },
    )
    assert replicas.isError is False, replicas
    payload = replicas.structuredContent
    assert payload is not None, replicas
    assert payload["subresource"] == "scale", payload
    assert payload["resource"] == "deployments/scale", payload

    obj = payload["object"]
    assert obj["kind"] == "Scale", obj
    assert "template" not in obj.get("spec", {}), (
        f"Deployment 전문이 그대로 왔다 — 서브리소스가 아니다: {sorted(obj['spec'])}"
    )
    assert obj["spec"]["replicas"] == FIXTURE_REPLICAS, obj["spec"]
    assert obj["status"]["replicas"] == FIXTURE_REPLICAS, obj["status"]
    print(f"scale ok: {WORKLOAD} replicas={obj['spec']['replicas']}")


async def test_resource_generic_ac5_reads_never_reach_the_approval_gate(
    session: ClientSession, pod: str
) -> None:
    """시나리오 5 — 이 시나리오의 호출은 `get` 만 행사하므로 승인 요청이 생기지 않는다.

    성공만 보면 공허하다 — 게이트를 아예 달지 않은 서버도 통과시킨다. 그래서 같은 세션에서
    게이트 대상(`v1/Secret` 읽기)이 **거부**되는 것을 나란히 봐서, 이 배포에 게이트가 실제로
    서 있는데도 위 호출들이 지나갔다는 것으로 만든다. 대조군은 **없는 이름**을 겨눈다 — 게이트는
    kubernetes 호출 수가 0인 채로 판정하므로(`internal/mcp/gate.go` 의 `genericPairs`) 대상이
    실재할 필요가 없고, 실재하면 「없어서 실패했다」와 섞인다.
    """
    payload, _ = await _logs(session, pod, container=LOG_CONTAINER)
    assert _lines(payload["text"]), payload

    try:
        await session.call_tool(
            "resource_get",
            {
                "apiVersion": "v1",
                "kind": "Secret",
                "namespace": NAMESPACE,
                "name": "resource-generic-ac5-no-such-secret",
            },
        )
    except McpError as exc:
        message = str(exc)
        assert "approval gate is not configured" in message, message
        print(f"gate control ok: 민감 종류 읽기는 거부된다 — {message}")
    else:
        raise AssertionError(
            "민감 종류 읽기가 거부되지 않았다 — 게이트가 서 있지 않으면 "
            "「승인 요청이 생기지 않음」이 아무것도 말하지 않는다"
        )


async def run() -> None:
    url = base_url()

    print("--- resource-generic/시나리오 5 (사전 조건: 다중 컨테이너 파드) ---")
    pod = test_resource_generic_ac5_precondition_multi_container_pod()
    ensure_workload_fixture_baseline()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- resource-generic/시나리오 5 (로그 반환) ---")
        await test_resource_generic_ac5_log_returns_recent_lines(session, pod)
        print("--- resource-generic/시나리오 5 (tail 절단) ---")
        await test_resource_generic_ac5_tail_lines_actually_truncates(session, pod)
        print("--- resource-generic/시나리오 5 (상한 초과는 거부) ---")
        await test_resource_generic_ac5_over_max_tail_is_rejected_not_clamped(session, pod)
        print("--- resource-generic/시나리오 5 (직전 인스턴스) ---")
        await test_resource_generic_ac5_previous_returns_the_terminated_instance(session)
        print("--- resource-generic/시나리오 5 (컨테이너 후보 제시) ---")
        await test_resource_generic_ac5_missing_container_is_refused_with_candidates(
            session, pod
        )
        print("--- resource-generic/시나리오 5 (스케일 조회) ---")
        await test_resource_generic_ac5_scale_returns_current_replicas(session)
        print("--- resource-generic/시나리오 5 (승인 요청이 생기지 않음) ---")
        await test_resource_generic_ac5_reads_never_reach_the_approval_gate(session, pod)
        print("ok: test-resource-generic.md#시나리오 5")


if __name__ == "__main__":
    asyncio.run(run())
