"""프록시는 경로를 숨기지 않는다 — 쌍은 같아도 승인 화면은 다르다.

검증 시나리오: test-resource-generic.md#시나리오 15
실행 대상: primary

Go 단위 아홉(``internal/mcp/proxy_test.go`` 넷 · ``internal/k8s/proxy_test.go`` 다섯)이 이
도구의 계약을 이미 단언한다 — 메서드→verb 다섯 쌍이 ``GET`` 을 포함해 전부 게이트를 타는 것,
``/exec`` 과 ``/healthz`` 가 **같은 쌍**으로 표현되는데도 승인 ``context`` 는 경로 전문과
고권한 표시로 둘을 가르는 것, 그 표시가 **막지 않는** 것, 경로 허용목록이 코드에 없는 것.
그 아홉은 전부 **가짜 게이트와 가짜 k8s 서비스** 위에서 돈다.

이 파일이 재는 것은 그 자리의 실물이다 — 실물 gatekeeper 가 사람에게 **실제로 보여 준 화면**에
무엇이 실렸는지, 그리고 승인된 프록시 호출이 apiserver 의 ``⟨kind⟩/proxy`` 를 지나 대상이
**정말 답하는지**. 가짜 게이트는 자기가 받은 ``context`` 문자열을 기록할 뿐이라, 화면을 만드는
코드와 화면을 **전달하는** 경로가 갈라져도 둘 다 통과한다. 가짜 k8s 서비스는 호출 횟수를 셀 뿐이라
왕복이 실제로 일어났는지는 말하지 못한다.

**네 호출의 쌍이 이 파일의 축이다.** Pod 의 ``GET`` 은 ``get on pods/proxy``, Service 의
``POST`` 는 ``create on services/proxy``, Node 의 ``/healthz`` 와 ``/exec`` 은 **둘 다**
``get on nodes/proxy``. 마지막 둘이 같은 쌍을 쓴다는 것이 시나리오의 논점이고, 그래서
``test_the_node_paths_share_a_pair_and_only_the_screen_tells_them_apart`` 는 두 화면에서 쌍을
**뽑아 서로 대조한다** — 각 화면에 쌍이 있다는 것만 재면 「같다」는 관측이 되지 않는다.

**대상 파드와 Service 는 이 파일이 스스로 세운다.** 기존 픽스처에 HTTP 를 서빙하는 파드가
없고(등재 문서가 이 행을 「이 렌즈 소관의 저작, 차단 요인 아님」으로 적은 자리다), 픽스처 YAML 과
``ci.yml`` 을 늘리는 대신 자매 ``resource_generic_ac14.py`` 가 파드를 세우는 방식 그대로 세운다.
러너가 파일을 자동 발견하므로 배선은 늘지 않는다. 이미지는 이미 클러스터에 있는 ``busybox:1.36``
이라 새 pull 이 없다. 파드가 **80 번을 듣는 것**은 편의가 아니다 — apiserver 의 파드 프록시는
이름에 포트를 적지 않으면 80 으로 간다.

**kubelet 의 ``/exec`` 응답 코드는 단언하지 않는다.** 그 엔드포인트는 스트리밍 URL 로의
리다이렉트로 답하고, 그 리다이렉트를 누가 어디까지 따라가는지는 apiserver 프록시와 Go 클라이언트
사이의 문제라 **이 시나리오가 말하는 계약이 아니다.** 여기서 재는 것은 그 호출이 우리 층에서
막히지 **않는다**는 것 — 경로 허용목록이 있었다면 승인 뒤에도 거부됐을 자리다. 「대상이 정말
답한다」는 쪽은 상태 코드가 계약인 두 호출이 든다: 파드의 ``/ac15-pod.txt`` 는 우리가 넣어 둔
본문을 그대로 돌려주고, 노드의 ``/healthz`` 는 kubelet 자신이 ``ok`` 로 답한다.

``병렬 레인:`` 을 선언하지 않는 것은 의도다. 러너는 레인 없는 파일을 레인 단계가 끝난 뒤 단독으로
돌리므로(``run_all.py`` 머리말), 이 파일이 세우고 지우는 파드·Service 가 같은 네임스페이스를 보는
다른 파일과 겹치지 않는다.
"""

from __future__ import annotations

import asyncio
import json
import re
import subprocess
import time

from mcp.shared.exceptions import McpError

from _gatekeeper import decide, gatekeeper_url, list_requests, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE

POD = "rg-ac15-http"
SERVICE = "rg-ac15-http"

PORT = 80

POD_BODY = "ac15-pod-body"
SERVICE_BODY = "ac15-service-body"

POD_PATH = "/ac15-pod.txt"
SERVICE_PATH = "/ac15-service.txt"
HEALTHZ_PATH = "/healthz"

POD_READY_BUDGET = 180.0

SERVE = (
    "mkdir -p /www"
    f" && printf '{POD_BODY}' > /www{POD_PATH}"
    f" && printf '{SERVICE_BODY}' > /www{SERVICE_PATH}"
    f" && exec httpd -f -p {PORT} -h /www"
)


def _kubectl(*args: str, input: str | None = None, check: bool = True):
    return subprocess.run(
        ["kubectl", "-n", NAMESPACE, *args],
        capture_output=True,
        text=True,
        check=check,
        input=input,
    )


def _node_name() -> str:
    """이 클러스터의 노드 하나. 시나리오의 사전 조건 「노드 1개」가 가리키는 그 노드다."""
    result = subprocess.run(
        ["kubectl", "get", "nodes", "-o", "jsonpath={.items[0].metadata.name}"],
        capture_output=True,
        text=True,
        check=True,
    )
    name = result.stdout.strip()
    assert name, "노드 이름을 읽지 못했다 — 클러스터가 비어 있다"
    return name


def _serving_pod_and_service() -> None:
    """파드와 Service 를 매니페스트 기준으로 되돌린다.

    지우고 다시 세우는 것은 재실행 편의가 아니다. 남아 있는 파드는 앞선 실행이 남긴 ``/www`` 를
    그대로 들고 있을 수 있고, 그러면 본문 단언이 이 파일의 선언이 아니라 **지난 실행의 잔여**로
    참이 된다.
    """
    _kubectl("delete", "pod", POD, "--ignore-not-found", "--wait=true")
    _kubectl("delete", "service", SERVICE, "--ignore-not-found", "--wait=true")
    _kubectl(
        "apply", "-f", "-",
        input=json.dumps(
            {
                "apiVersion": "v1",
                "kind": "Pod",
                "metadata": {"name": POD, "namespace": NAMESPACE, "labels": {"app": POD}},
                "spec": {
                    "restartPolicy": "Never",
                    "containers": [
                        {
                            "name": "httpd",
                            "image": "busybox:1.36",
                            "command": ["sh", "-c", SERVE],
                            "ports": [{"containerPort": PORT}],
                        }
                    ],
                },
            }
        ),
    )
    _kubectl(
        "apply", "-f", "-",
        input=json.dumps(
            {
                "apiVersion": "v1",
                "kind": "Service",
                "metadata": {"name": SERVICE, "namespace": NAMESPACE},
                # 포트를 하나만 두는 것은 의도다 — apiserver 의 Service 프록시는 포트가 여럿이면
                # 이름으로 골라 달라고 하고, 그 골라 주는 문법은 이 시나리오가 재는 것이 아니다.
                "spec": {
                    "selector": {"app": POD},
                    "ports": [{"port": PORT, "targetPort": PORT}],
                },
            }
        ),
    )
    deadline = time.monotonic() + POD_READY_BUDGET
    while time.monotonic() < deadline:
        ready = _kubectl(
            "get", "pod", POD, "-o", "jsonpath={.status.containerStatuses[0].ready}",
            check=False,
        )
        if ready.stdout.strip() == "true":
            return
        time.sleep(2)
    raise AssertionError(
        f"{POD} 가 {POD_READY_BUDGET:.0f}초 안에 Ready 가 되지 않았다: "
        f"{_kubectl('describe', 'pod', POD, check=False).stdout[-800:]}"
    )


def _proxy_args(kind: str, name: str, method: str, path: str, **extra) -> dict:
    args: dict = {
        "apiVersion": "v1",
        "kind": kind,
        "name": name,
        "method": method,
        "path": path,
        **extra,
    }
    if kind != "Node":
        args["namespace"] = NAMESPACE
    return args


async def _pending(session, gate: str, args: dict, marker: str):
    """도구 호출을 띄우고 그 호출이 만든 PENDING 요청을 집는다.

    마커는 **경로**다. 이 파일의 네 호출 중 둘(``/healthz`` 와 ``/exec``)이 같은 객체를 같은
    쌍으로 가리키므로, 대상 이름으로는 두 화면이 갈리지 않는다.
    """
    task = asyncio.create_task(session.call_tool("resource_proxy", args))
    row = await wait_for_pending(gate, marker)
    return task, row


#: 승인 화면에서 쌍을 뽑는 자리. ``approvalContext`` 가 ``rbac:`` 줄로 쓴다.
RBAC_LINE_RE = re.compile(r"^rbac: (.+)$", re.MULTILINE)

HIGH_POWER_MARK = "kubelet high-power endpoint"


def _pair(context: str) -> str:
    match = RBAC_LINE_RE.search(context)
    assert match, f"승인 화면에 rbac 줄이 없다:\n{context}"
    return match.group(1).strip()


async def test_a_pod_get_spends_get_and_the_target_answers(session, gate: str) -> None:
    args = _proxy_args("Pod", POD, "GET", POD_PATH)
    task, row = await _pending(session, gate, args, POD_PATH)

    context = row["context"]
    assert _pair(context) == "get on pods/proxy", context
    assert f"GET {POD_PATH}" in context, f"메서드와 경로가 화면에 없다:\n{context}"
    # 파드의 경로는 kubelet 의 것이 아니다 — 같은 철자라도 표시가 붙으면 안 된다는 쪽을 단위가
    # 이미 잰다. 여기서는 그 판정이 실물 화면까지 그대로 왔는지만 되받는다.
    assert HIGH_POWER_MARK not in context, context

    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload["verb"] == "get", payload
    assert payload["path"] == POD_PATH, payload
    assert payload["status"] == 200, payload
    # 본문까지 재는 이유: 「왕복이 돌아왔다」는 apiserver 가 대신 낸 오류 응답으로도 참이 된다.
    # 대상이 실제로 답했다는 것은 우리가 넣어 둔 이 문자열만 말한다.
    assert payload["body"] == POD_BODY, payload
    assert payload["bodyEncoding"] == "utf-8", payload
    assert payload["truncated"] is False, payload


async def test_a_service_post_spends_create_and_carries_the_body(session, gate: str) -> None:
    args = _proxy_args(
        "Service", SERVICE, "POST", SERVICE_PATH,
        body=SERVICE_BODY, contentType="text/plain",
    )
    task, row = await _pending(session, gate, args, SERVICE_PATH)

    context = row["context"]
    assert _pair(context) == "create on services/proxy", context
    assert f"POST {SERVICE_PATH}" in context, context
    # 마커가 아니라 **전문**을 찾는다 — 본문의 앞 몇 글자만 실어도 부분 문자열 단언은 통과하고,
    # 운영자가 보지 못한 나머지가 그대로 나간다.
    assert json.dumps(SERVICE_BODY)[1:-1] in context, f"본문 전문이 화면에 없다:\n{context}"

    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload["verb"] == "create", payload
    assert payload["path"] == SERVICE_PATH, payload
    # 상태 코드는 대상이 POST 를 어떻게 다루는지의 문제라 단언하지 않는다. 단언하는 것은
    # **답이 돌아왔다**는 것이다 — 우리 층이 막았다면 여기에 올 응답 자체가 없다.
    assert payload["status"] >= 200, payload


async def test_the_node_paths_share_a_pair_and_only_the_screen_tells_them_apart(
    session, gate: str, node: str
) -> None:
    node_args = _proxy_args("Node", node, "GET", HEALTHZ_PATH)
    task, healthz_row = await _pending(session, gate, node_args, HEALTHZ_PATH)
    healthz_context = healthz_row["context"]
    assert _pair(healthz_context) == "get on nodes/proxy", healthz_context
    assert HEALTHZ_PATH in healthz_context, healthz_context
    # 모든 것에 붙는 표시는 아무것도 표시하지 않는다 — 관측 엔드포인트에 붙지 않는 쪽이 이 표시를
    # 믿을 수 있게 만드는 절반이다.
    assert HIGH_POWER_MARK not in healthz_context, healthz_context

    await decide(gate, healthz_row["id"], "APPROVED")
    healthz = await task
    assert healthz.isError is False, healthz
    assert healthz.structuredContent["status"] == 200, healthz.structuredContent
    # kubelet 자신이 답했다는 증거. 여기가 초록이어야 아래 ``/exec`` 이 「닿지도 못했다」가
    # 아니라 「닿았고 막히지 않았다」로 읽힌다.
    assert "ok" in healthz.structuredContent["body"], healthz.structuredContent

    exec_path = f"/exec/{NAMESPACE}/{POD}/httpd?command=id"
    exec_args = _proxy_args("Node", node, "GET", exec_path)
    task, exec_row = await _pending(session, gate, exec_args, "/exec/")
    exec_context = exec_row["context"]

    assert _pair(exec_context) == _pair(healthz_context), (
        f"/healthz 와 /exec 이 다른 쌍으로 표현됐다 — 이 시나리오의 전제가 깨졌다:\n"
        f"{healthz_context}\n---\n{exec_context}"
    )
    # 쿼리까지 전문이어야 한다. 경로만 싣고 ``?command=id`` 를 떨구면 운영자가 보는 것은
    # 「파일 하나 읽기」이고 실제로 일어나는 것은 명령 실행이다.
    assert exec_path in exec_context, f"경로 전문이 화면에 없다:\n{exec_context}"
    assert HIGH_POWER_MARK in exec_context, f"고권한 표시가 없다:\n{exec_context}"

    await decide(gate, exec_row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload["verb"] == "get", payload
    assert payload["path"] == exec_path, payload


async def test_a_get_without_approval_is_refused(session, gate: str) -> None:
    args = _proxy_args("Pod", POD, "GET", POD_PATH)
    before = len(list_requests(gate, status="PENDING"))
    task, row = await _pending(session, gate, args, POD_PATH)
    await decide(gate, row["id"], "REJECTED")
    try:
        await task
    except McpError as exc:
        assert "approval rejected" in str(exc), exc
    else:
        raise AssertionError(
            "승인 없는 GET 이 그대로 실행됐다 — 읽기라는 이유로 게이트를 비껴갔다"
        )
    # 거부가 요청을 남기고 가지 않는지까지 본다. 남은 PENDING 이 늘어 있으면 다음 파일의
    # ``wait_for_pending`` 이 이 파일의 잔여를 집어 헛통과할 수 있다.
    assert len(list_requests(gate, status="PENDING")) == before, (
        "거부 뒤에도 PENDING 승인 요청이 남았다"
    )


async def run() -> None:
    url = base_url()
    node = _node_name()
    _serving_pod_and_service()
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- resource-generic/시나리오 15 (파드 GET — get on pods/proxy) ---")
            await test_a_pod_get_spends_get_and_the_target_answers(session, gate)
            print("--- resource-generic/시나리오 15 (Service POST — create on services/proxy) ---")
            await test_a_service_post_spends_create_and_carries_the_body(session, gate)
            print("--- resource-generic/시나리오 15 (노드의 두 경로는 같은 쌍을 쓴다) ---")
            await test_the_node_paths_share_a_pair_and_only_the_screen_tells_them_apart(
                session, gate, node
            )
            print("--- resource-generic/시나리오 15 (승인 없는 GET 도 거부) ---")
            await test_a_get_without_approval_is_refused(session, gate)
    print("ok: test-resource-generic.md#시나리오 15")


if __name__ == "__main__":
    asyncio.run(run())
