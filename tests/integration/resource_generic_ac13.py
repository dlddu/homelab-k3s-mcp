"""실행 중 컨테이너 stdio 접속 — 기존 프로세스의 스트림에 붙고, 창은 상한을 넘지 않는다.

검증 시나리오: test-resource-generic.md#시나리오 13
실행 대상: primary

Go 단위(``internal/mcp/attach_test.go``)가 인자 파싱·쌍·`readSeconds` 상한·stdin 전달·승인
``context`` 를 이미 덮는다. 이 파일이 더하는 것은 **실물 kubelet 과 실물 gatekeeper 에서 같은
계약이 성립하는가**이고, 시나리오가 단위로 관측할 수 없다고 못박은 두 가지가 여기에 있다.

1. **새 프로세스가 뜨지 않았다.** attach 가 돌려준 줄이 **파드 로그에도 그대로** 있으면 그것은
   컨테이너 stdout 으로 흘러간 줄, 즉 원래 살아 있던 프로세스의 스트림이다. exec 였다면 새
   프로세스의 출력은 그 호출의 스트림에만 있고 로그에는 없다. 티커가 번호를 매기는 것은 같은
   사실의 두 번째 증인이다 — 접속이 프로세스를 띄웠다면 번호가 1 부터 다시 시작한다.
2. **stdin 이 대상 프로세스에 닿았다.** 되울림 줄 역시 컨테이너 stdout 으로 나오므로 응답과
   파드 로그 양쪽에 있다. 페이로드가 어딘가에서 삼켜졌다면 둘 다 비어 있다.

``readSeconds=31`` 거부는 **승인 요청이 생기기 전에** 와야 한다(``attachPairs`` 가 쌍 해석보다
인자 검사를 먼저 두는 이유가 그것이다). 그래서 거부를 확인하는 것만으로는 부족하고, 호출 앞뒤의
gatekeeper 요청 수가 같은지를 함께 본다 — 승인을 한 번 태우고 나서 오는 거부는 시나리오가 요구한
그 거부가 아니다.

**대상 파드는 이 파일이 세운다.** ``stdin: true`` 가 필요한 파드는 이 시나리오 전용이라
``tests/k8s/kind/`` 에 두면 아무도 쓰지 않는 픽스처가 CI 에 상주하고, 이 레포는 이미 「각 파일이
자기 선행 조건을 스스로 성립시킨다」를 택했다(``resource_generic_ac1.py`` 와 같은 자리).
"""

from __future__ import annotations

import asyncio
import json
import subprocess
import uuid

from mcp.shared.exceptions import McpError

from _gatekeeper import decide, gatekeeper_url, list_requests, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"
POD = "rg-ac13-talker"
CONTAINER = "talker"

TICK_PREFIX = "rg-ac13 tick"
STDIN_PREFIX = "rg-ac13 stdin"

READ_WINDOW = 3
OVER_LIMIT_WINDOW = 31

#: PID 1. 백그라운드 티커와 전경 stdin 리더가 **같은 컨테이너 stdout** 을 공유한다 — 그래야
#: attach 가 본 줄과 파드 로그가 같은 스트림이 된다.
#:
#: 리더가 `read` 실패에서도 `$line` 을 흘려보내는 이유: SUT 는 stdin 을 한 번 써 넣고 닫으므로
#: (``strings.NewReader``), 개행이 오기 전에 EOF 가 겹치면 `read` 는 읽은 만큼을 담고 실패를
#: 돌려준다. 실패 갈래에서 버리면 그 왕복이 조용히 사라진다. `sleep` 은 EOF 뒤 바쁜 회전을
#: 막는다.
#:
#: 산술이 `expr` 인 이유는 ``tests/k8s/kind/resource-generic-fixture.yaml`` 머리말에 있다.
SCRIPT = f"""
n=0
while :; do
  n=`expr $n + 1`
  echo "{TICK_PREFIX} $n"
  sleep 1
done &
while :; do
  if read -r line; then
    [ -n "$line" ] && echo "{STDIN_PREFIX} $line"
  else
    [ -n "$line" ] && echo "{STDIN_PREFIX} $line"
    sleep 1
  fi
done
"""


def _manifest() -> dict:
    return {
        "apiVersion": "v1",
        "kind": "Pod",
        "metadata": {
            "name": POD,
            "namespace": NAMESPACE,
            "labels": {"app.kubernetes.io/part-of": "homelab-k3s-mcp-tests"},
        },
        "spec": {
            "restartPolicy": "Always",
            "terminationGracePeriodSeconds": 0,
            "containers": [
                {
                    "name": CONTAINER,
                    "image": "busybox:1.36",
                    # 이 둘이 attach 의 선행 조건이다. stdin 이 열려 있지 않으면 kubelet 에
                    # 붙을 스트림 자체가 없고, stdinOnce 를 켜면 첫 접속이 끊길 때 stdin 이
                    # 닫혀 시나리오의 세 번째 단계(두 번째 접속)가 성립하지 않는다.
                    "stdin": True,
                    "stdinOnce": False,
                    "tty": False,
                    "command": ["sh", "-c", SCRIPT],
                    "resources": {
                        "requests": {"cpu": "1m", "memory": "4Mi"},
                        "limits": {"cpu": "50m", "memory": "16Mi"},
                    },
                }
            ],
        },
    }


def _ensure_pod() -> None:
    """대상 파드를 멱등 생성하고 Ready 까지 기다린다."""
    subprocess.run(
        ["kubectl", "apply", "-f", "-"],
        input=json.dumps(_manifest()),
        text=True,
        check=True,
        capture_output=True,
    )
    subprocess.run(
        [
            "kubectl", "-n", NAMESPACE, "wait", "--for=condition=Ready",
            f"pod/{POD}", "--timeout=120s",
        ],
        check=True,
        capture_output=True,
    )


def _logs() -> str:
    return subprocess.check_output(
        ["kubectl", "-n", NAMESPACE, "logs", POD, "-c", CONTAINER, "--tail=2000"],
        text=True,
    )


def _tick_numbers(text: str) -> list[int]:
    numbers = []
    for line in text.splitlines():
        line = line.strip()
        if line.startswith(TICK_PREFIX):
            tail = line[len(TICK_PREFIX):].strip()
            if tail.isdigit():
                numbers.append(int(tail))
    return numbers


#: 게이트 API 의 로컬 URL. `run()` 이 포트포워드를 연 뒤 채운다 — 댄스가 케이스마다 같은
#: 모양이라 케이스 인자로 물려 내리는 것보다 한 자리에 두는 편이 읽기 쉽다.
_GATE = ""


async def _attach(session, arguments: dict):
    """승인 댄스를 태워 attach 를 돌린다. 마커는 파드 이름 — context 가 좌표를 싣는다."""
    task = asyncio.create_task(session.call_tool("resource_attach", arguments))
    row = await wait_for_pending(_GATE, POD)
    await decide(_GATE, row["id"], "APPROVED")
    return await task


async def test_the_window_returns_the_running_process_stream(session) -> None:
    result = await _attach(
        session,
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "namespace": NAMESPACE,
            "name": POD,
            "container": CONTAINER,
            "readSeconds": READ_WINDOW,
        },
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload["readSeconds"] == READ_WINDOW, payload
    assert payload["stdinWritten"] is False, payload

    ticks = _tick_numbers(payload["stdout"])
    assert ticks, f"{READ_WINDOW}초 창이 아무 출력도 돌려주지 않았다: {payload['stdout']!r}"
    assert min(ticks) > 1, (
        f"틱 번호가 1 부터 시작한다({ticks}) — 접속이 새 프로세스를 띄웠다는 뜻이다"
    )

    logs = _logs()
    for number in ticks:
        line = f"{TICK_PREFIX} {number}"
        assert line in logs, (
            f"attach 가 돌려준 {line!r} 이 파드 로그에 없다 — 컨테이너 stdout 이 아닌 "
            "다른 스트림, 즉 새로 뜬 프로세스의 출력이다"
        )


async def test_an_over_limit_window_is_refused_before_any_approval(session) -> None:
    before = len(list_requests(_GATE))
    try:
        await session.call_tool(
            "resource_attach",
            {
                "apiVersion": "v1",
                "kind": "Pod",
                "namespace": NAMESPACE,
                "name": POD,
                "container": CONTAINER,
                "readSeconds": OVER_LIMIT_WINDOW,
            },
        )
    except McpError as exc:
        assert "readSeconds" in str(exc), exc
        assert "30" in str(exc), f"거부가 상한을 말하지 않는다: {exc}"
    else:
        raise AssertionError(f"readSeconds={OVER_LIMIT_WINDOW} 호출이 성공했다")

    assert len(list_requests(_GATE)) == before, (
        "상한 밖 창이 승인 요청을 만들었다 — 거부가 쌍 해석보다 뒤에 왔다는 뜻이다"
    )


async def test_stdin_reaches_the_attached_process(session) -> None:
    token = f"ping-{uuid.uuid4().hex[:8]}"
    result = await _attach(
        session,
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "namespace": NAMESPACE,
            "name": POD,
            "container": CONTAINER,
            "readSeconds": READ_WINDOW,
            "stdin": f"{token}\n",
        },
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload["stdinWritten"] is True, payload

    echo = f"{STDIN_PREFIX} {token}"
    assert echo in payload["stdout"], (
        f"stdin 의 반응 {echo!r} 이 응답에 없다: {payload['stdout']!r}"
    )
    assert echo in _logs(), (
        f"{echo!r} 이 파드 로그에 없다 — 되울림이 대상 프로세스가 아닌 곳에서 나왔다"
    )


async def run() -> None:
    global _GATE
    url = base_url()
    _ensure_pod()
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        _GATE = gate
        async with open_session(url) as session:
            print("--- resource-generic/시나리오 13 (3초 창은 기존 스트림이다) ---")
            await test_the_window_returns_the_running_process_stream(session)
            print("--- resource-generic/시나리오 13 (readSeconds=31 은 승인 앞에서 거부된다) ---")
            await test_an_over_limit_window_is_refused_before_any_approval(session)
            print("--- resource-generic/시나리오 13 (stdin 이 대상 프로세스에 닿는다) ---")
            await test_stdin_reaches_the_attached_process(session)
    print("ok: test-resource-generic.md#시나리오 13")


if __name__ == "__main__":
    asyncio.run(run())
