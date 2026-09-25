"""포트 포워드는 단발 왕복이다 — 한 번 보내고, 상한까지 읽고, 터널을 닫는다.

검증 시나리오: test-resource-generic.md#시나리오 14
실행 대상: primary
병렬 레인: gate-tunnels

Go 단위 다섯(``internal/k8s/port_forward_test.go``)과 셋(``internal/mcp/port_forward_test.go``)이
이 도구의 계약을 이미 단언한다 — 한 번 쓰고 쓰기 반쪽을 닫는 것, 에러 스트림을 올리는 것,
바이트 상한, 창 만료, 인코딩 분기, 승인 화면의 포트·페이로드. 그 여덟은 전부 **가짜 SPDY
연결과 가짜 게이트** 위에서 돈다. 이 파일이 재는 것은 그 자리의 실물이다 — apiserver 의
``pods/portforward`` 를 지나 실제로 듣고 있는 서버가 답을 돌려주는지, 그 답이 상한에서
잘리는지, 그리고 **실물 gatekeeper** 가 사람에게 보여 준 화면이 포트와 페이로드를 그대로
싣는지.

**두 응답 파일을 미리 만들어 둔다** — 작은 것과 상한을 넘기는 것. 상한 케이스를 「끝없이 뱉는
서버」로 세우지 않는 것은 의도다: 그러면 잘림이 상한 때문인지 읽기 창이 만료해서인지 구별되지
않는다. 채택하지 않은 설계는 코드에서 복원되지 않는다.

**「같은 승인 id 로 두 번째 왕복」의 단언이 시나리오 문면과 글자가 다른 것은 빠뜨린 것이
아니다** — 이 층에서 관측 가능한 형태가 그 부정형뿐이다. 첫 응답의 ``tunnelClosed`` 는 서버의
주장이고, 이 케이스가 그 주장을 클러스터 쪽에서 되받는다.

같은 네임스페이스의 다른 레인과 겹쳐도 되는 것은 이 파일이 세우고 지우는 파드를 이름으로만
다루기 때문이다. 인자 거부 케이스의 「승인 요청 0건」도 그래서 전체 PENDING 이 아니라 그 호출의
마커를 담은 PENDING 으로 센다.
"""

from __future__ import annotations

import asyncio
import json
import re
import subprocess
import time

from mcp.shared.exceptions import McpError

from _gatekeeper import count_requests, decide, gatekeeper_url, get_request, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE

POD = "rg-ac14-http"
PORT = 8080

SMALL_BODY = "hello-from-ac14"

#: 상한을 넘기려고 준비하는 본문 크기. 상한(256 KiB)보다 크되 왕복이 읽기 창 안에 끝날 만큼만
#: 크다 — 창이 아니라 상한이 끊었다는 것이 이 숫자가 지키는 판별식이다.
BIG_BYTES = 400_000

#: ``internal/k8s/exec.go::streamMaxOutputBytes`` 의 사본. 서버가 상한을 옮기면 이 파일이 먼저
#: 거짓말을 한다.
MAX_OUTPUT_BYTES = 256 * 1024

POD_READY_BUDGET = 180.0

SERVE = (
    "mkdir -p /www"
    f" && printf '{SMALL_BODY}' > /www/small.txt"
    f" && yes 0123456789abcdef | head -c {BIG_BYTES} > /www/big.txt"
    f" && exec httpd -f -p {PORT} -h /www"
)


def _request(path: str, marker: str) -> str:
    return (
        f"GET {path} HTTP/1.0\r\n"
        f"Host: {POD}\r\n"
        f"X-Ac14-Marker: {marker}\r\n"
        "\r\n"
    )


def _kubectl(*args: str, input: str | None = None, check: bool = True):
    return subprocess.run(
        ["kubectl", "-n", NAMESPACE, *args],
        capture_output=True,
        text=True,
        check=check,
        input=input,
    )


def _serving_pod() -> None:
    """파드를 매니페스트 기준으로 되돌린다 — 지우고, 다시 세우고, Ready 를 기다린다.

    지우고 다시 세우는 것은 재실행 편의가 아니다. 남아 있는 파드는 앞선 실행이 남긴 ``/www``
    를 그대로 들고 있을 수 있고, 그러면 상한 케이스가 재려는 크기 조건이 이 파일의 선언이
    아니라 **지난 실행의 잔여**로 결정된다.
    """
    _kubectl("delete", "pod", POD, "--ignore-not-found", "--wait=true")
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
    deadline = time.monotonic() + POD_READY_BUDGET
    while time.monotonic() < deadline:
        phase = _kubectl(
            "get", "pod", POD, "-o", "jsonpath={.status.containerStatuses[0].ready}",
            check=False,
        )
        if phase.stdout.strip() == "true":
            return
        time.sleep(2)
    raise AssertionError(
        f"{POD} 가 {POD_READY_BUDGET:.0f}초 안에 Ready 가 되지 않았다: "
        f"{_kubectl('describe', 'pod', POD, check=False).stdout[-800:]}"
    )


def _forward_args(path: str, marker: str, **extra) -> dict:
    return {
        "apiVersion": "v1",
        "kind": "Pod",
        "namespace": NAMESPACE,
        "name": POD,
        "port": PORT,
        "payload": _request(path, marker),
        **extra,
    }


async def _pending(session, gate: str, args: dict, marker: str):
    """도구 호출을 띄우고 그 호출이 만든 PENDING 요청을 집는다."""
    task = asyncio.create_task(session.call_tool("resource_port_forward", args))
    row = await wait_for_pending(gate, marker)
    return task, row


#: 첫 왕복과 두 번째 왕복이 **글자 그대로 같은 호출**이어야 두 번째 케이스가 공허하지 않다 —
#: 마커가 다르면 「새 요청이 생겼다」는 관측이 마커가 달라서 참이 되어 버린다.
ROUND_TRIP_MARKER = "rg-ac14-round-trip"


async def test_one_round_trip_answers_and_closes(session, gate: str) -> dict:
    marker = ROUND_TRIP_MARKER
    args = _forward_args("/small.txt", marker, readSeconds=10)
    task, row = await _pending(session, gate, args, marker)

    context = row["context"]
    assert re.search(r'"port"\s*:\s*8080', context), f"포트가 화면에 없다:\n{context}"
    verbatim = json.dumps(args["payload"])[1:-1]
    assert verbatim in context, f"페이로드 전문이 화면에 없다:\n{context}"
    assert "create on pods/portforward" in context, context

    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload["port"] == PORT, payload
    assert payload["pod"] == POD, payload
    assert payload["bytesSent"] == len(args["payload"]), payload
    assert payload["responseEncoding"] == "utf-8", payload
    assert payload["responseTruncated"] is False, payload
    assert payload["tunnelClosed"] is True, payload
    # 본문까지 재는 이유: 「응답이 왔다」는 쓰기 반쪽을 닫지 않는 구현에서도 빈 문자열로
    # 참이 된다. 서버가 실제로 요청을 끝까지 읽고 답했다는 것은 이 문자열이 말한다.
    assert "200 OK" in payload["response"], payload["response"][:400]
    assert SMALL_BODY in payload["response"], payload["response"][:400]
    return row


async def test_a_second_round_trip_needs_its_own_approval(
    session, gate: str, first: dict
) -> None:
    assert get_request(gate, first["id"])["status"] == "APPROVED", (
        "첫 승인이 PENDING 으로 남아 있다 — 아래 관측이 「새 요청」을 가리지 못한다"
    )
    args = _forward_args("/small.txt", ROUND_TRIP_MARKER, readSeconds=10)
    task, row = await _pending(session, gate, args, ROUND_TRIP_MARKER)
    assert row["id"] != first["id"], (
        "두 번째 왕복이 첫 승인 요청을 그대로 다시 썼다 — 터널이 닫히지 않았다는 뜻이다"
    )

    await decide(gate, row["id"], "REJECTED")
    try:
        await task
    except McpError as exc:
        assert "approval rejected" in str(exc), exc
    else:
        raise AssertionError(
            "거절된 두 번째 왕복이 그대로 실행됐다 — 1승인 1실행이 터널에 적용되지 않았다"
        )


async def test_an_answer_over_the_cap_is_cut_and_says_so(session, gate: str) -> None:
    marker = "rg-ac14-cap"
    task, row = await _pending(
        session, gate, _forward_args("/big.txt", marker, readSeconds=20), marker
    )
    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload["responseTruncated"] is True, payload
    assert len(payload["response"]) == MAX_OUTPUT_BYTES, len(payload["response"])
    assert payload["tunnelClosed"] is True, payload


async def test_arguments_over_the_cap_are_refused_before_anyone_is_asked(
    session, gate: str
) -> None:
    marker = "rg-ac14-refused"
    before = count_requests(gate, marker, "PENDING")
    try:
        await session.call_tool(
            "resource_port_forward",
            _forward_args("/small.txt", marker, readSeconds=999),
        )
    except McpError as exc:
        assert "readSeconds must be between 1 and 30" in str(exc), exc
    else:
        raise AssertionError("상한을 넘긴 readSeconds 가 거부되지 않았다")
    assert count_requests(gate, marker, "PENDING") == before, (
        "인자 거부가 승인 요청을 만들었다"
    )


async def run() -> None:
    url = base_url()
    _serving_pod()
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- resource-generic/시나리오 14 (단발 왕복과 승인 화면) ---")
            first = await test_one_round_trip_answers_and_closes(session, gate)
            print("--- resource-generic/시나리오 14 (두 번째 왕복은 새 승인을 받는다) ---")
            await test_a_second_round_trip_needs_its_own_approval(session, gate, first)
            print("--- resource-generic/시나리오 14 (읽기 상한 절단) ---")
            await test_an_answer_over_the_cap_is_cut_and_says_so(session, gate)
            print("--- resource-generic/시나리오 14 (창 상한 초과 인자 거부) ---")
            await test_arguments_over_the_cap_are_refused_before_anyone_is_asked(session, gate)
    print("ok: test-resource-generic.md#시나리오 14")


if __name__ == "__main__":
    asyncio.run(run())
