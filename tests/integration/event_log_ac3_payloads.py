"""Secret 본문·스트림 페이로드 비노출 — 응답에는 있고 레코드에는 없다, 같은 실행에서.

검증 시나리오: test-event-log.md#시나리오 5
실행 대상: gatekeeper-variant

시나리오의 요점은 「두 관측이 같은 실행에서 나온다」는 것이다 — 승인된 ``resource_get(kind=Secret)``
응답에 데이터 값이 실려 오고, ``resource_exec`` 응답에 명령의 stdout 이 실려 온 **바로 그 호출**의
레코드에 값도 명령도 본문도 없다. 응답 쪽 단언이 먼저인 이유는 자매 ``event_log_ac3_credentials.py``
와 같다 — 그것이 없으면 네거티브는 아무것도 돌려주지 않는 서버로도 통과한다.

**이 변형에서 도는 이유는 승인을 사람 없이 즉시 내기 위해서다.** 둘 다 게이트 대상 호출이라
승인이 필요한데, 이 배포는 `GATEKEEPER_USER_ID` 를 물고 있어 gatekeeper 의 `AUTO_APPROVE` 모드가
실제로 발화한다(``approval_gate_ac9.py`` 가 같은 손잡이로 세 모드를 잰다). primary 의 수동
댄스(`wait_for_pending` → `decide`)도 가능하지만 승인 2회를 사람 대신 폴링으로 맞추는 경주가
되고, 이 파일이 재는 것은 승인 절차가 아니라 승인 **뒤에** 쓰인 레코드다. 모드를 바꾸므로
`병렬 레인:` 은 선언하지 않는다(``resource_generic_ac17.py`` 와 같은 이유).

**대상 Secret 과 파드는 이 파일이 스스로 세운다** — 값은 레포 어디에도 없는 상수라 어느 표면에서
발견되면 그것이 이 Secret 에서 나온 것이다(``resource_generic_ac17.py`` 의 `TOKEN` 과 같은
방식). 파드는 그 Secret 을 파일로 얹고 잠들어 있기만 하면 되고, `exec` 의 `cat` 이 그 파일을
읽어 평문을 stdout 으로 돌려준다. 그래서 base64 값(읽기 응답)·평문(exec 응답)·명령 인자·마운트
경로 넷이 한 대상 위에서 바늘이 된다.

레코드는 이 변형 파드 stdout 의 ``msg="tool call"`` 줄이다. 이 호출의 레코드는 호출 전후 좌표별
(`tool`·`target.kind`·`target.name`) 레코드 수의 차로 집는다 — 이 그룹의 파일은 차례로 돌고 이
좌표는 이 파일만 쓴다.
"""

from __future__ import annotations

import asyncio
import base64
import json
import subprocess
import time

from _gatekeeper import gatekeeper_url, set_auto_response
from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE

VARIANT_NAMESPACE = "homelab-k3s-mcp-gatekeeper-variant"
VARIANT_DEPLOYMENT = "homelab-k3s-mcp-gatekeeper-variant"

RECORD_MARK = 'msg="tool call"'

SECRET = "el-ac3-token"
TOKEN = "el-ac3-token-7b2e94c1d0f5"
ENCODED_TOKEN = base64.b64encode(TOKEN.encode()).decode()

POD = "el-ac3-token-holder"
CONTAINER = "holder"
POD_READY_BUDGET = 180.0

MOUNT = "/var/run/secrets/el-ac3"
TOKEN_PATH = f"{MOUNT}/token"
EXEC_COMMAND = ["sh", "-c", f"cat {TOKEN_PATH}"]

#: 응답이 돌아온 뒤 kubelet 의 로그 읽기가 그 줄을 보여 주기까지의 창.
RECORD_BUDGET = 15.0


def _kubectl(*args: str, input: str | None = None, check: bool = True):
    return subprocess.run(
        ["kubectl", *args], capture_output=True, text=True, check=check, input=input
    )


def _apply(manifest: dict) -> None:
    _kubectl("apply", "-f", "-", input=json.dumps(manifest))


def _fixtures() -> None:
    _apply(
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {"name": SECRET, "namespace": NAMESPACE},
            "stringData": {"token": TOKEN},
        }
    )
    _kubectl("-n", NAMESPACE, "delete", "pod", POD, "--ignore-not-found", "--wait=true")
    _apply(
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "metadata": {"name": POD, "namespace": NAMESPACE, "labels": {"app": POD}},
            "spec": {
                "restartPolicy": "Never",
                "volumes": [{"name": "token", "secret": {"secretName": SECRET}}],
                "containers": [
                    {
                        "name": CONTAINER,
                        "image": "busybox:1.36",
                        "command": ["sh", "-c", "sleep 3600"],
                        "volumeMounts": [
                            {"name": "token", "mountPath": MOUNT, "readOnly": True}
                        ],
                    }
                ],
            },
        }
    )
    deadline = time.monotonic() + POD_READY_BUDGET
    while time.monotonic() < deadline:
        ready = _kubectl(
            "-n", NAMESPACE, "get", "pod", POD,
            "-o", "jsonpath={.status.containerStatuses[0].ready}",
            check=False,
        )
        if ready.stdout.strip() == "true":
            return
        time.sleep(2)
    raise AssertionError(
        f"{POD} 가 {POD_READY_BUDGET:.0f}초 안에 Ready 가 되지 않았다: "
        f"{_kubectl('-n', NAMESPACE, 'describe', 'pod', POD, check=False).stdout[-800:]}"
    )


def _records() -> list[str]:
    log = subprocess.check_output(
        ["kubectl", "-n", VARIANT_NAMESPACE, "logs", f"deploy/{VARIANT_DEPLOYMENT}", "--tail=-1"],
        text=True,
    )
    return [line for line in log.splitlines() if RECORD_MARK in line]


def _for(records: list[str], tool: str, kind: str, name: str) -> list[str]:
    return [
        line
        for line in records
        if f"tool={tool} " in line
        and f"target.kind={kind} " in line
        and f"target.name={name} " in line
    ]


def _one_fresh(before: list[str], after: list[str], what: str) -> str:
    assert len(after) - len(before) == 1, (
        f"{what} 의 레코드가 {len(before)} → {len(after)} — 호출당 정확히 하나여야 한다"
    )
    line = after[-1]
    assert "result=success" in line, f"{what} 의 레코드가 성공이 아니다: {line}"
    return line


async def test_the_values_are_in_the_responses_and_not_in_the_records(session) -> None:
    records = _records()
    read_before = _for(records, "resource_get", "Secret", SECRET)
    exec_before = _for(records, "resource_exec", "Pod", POD)

    read = await session.call_tool(
        "resource_get",
        {"apiVersion": "v1", "kind": "Secret", "namespace": NAMESPACE, "name": SECRET},
    )
    assert read.isError is False, read
    read_body = "".join(getattr(block, "text", "") for block in read.content)
    assert ENCODED_TOKEN in read_body, (
        "승인된 Secret 읽기가 값을 돌려주지 않았다 — 이 대조군이 없으면 아래 단언은 아무것도 "
        f"흐르지 않는 서버로도 통과한다:\n{read_body[:400]}"
    )

    executed = await session.call_tool(
        "resource_exec",
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "namespace": NAMESPACE,
            "name": POD,
            "container": CONTAINER,
            "command": EXEC_COMMAND,
        },
    )
    assert executed.isError is False, executed
    payload = executed.structuredContent
    assert payload["success"] is True and payload["exitCode"] == 0, payload
    assert TOKEN in payload["stdout"], f"exec 응답에 평문이 없다: {payload}"

    deadline = time.monotonic() + RECORD_BUDGET
    while True:
        records = _records()
        if (
            len(_for(records, "resource_exec", "Pod", POD)) > len(exec_before)
            or time.monotonic() >= deadline
        ):
            break
        time.sleep(0.5)
    read_line = _one_fresh(read_before, _for(records, "resource_get", "Secret", SECRET), "Secret 읽기")
    exec_line = _one_fresh(exec_before, _for(records, "resource_exec", "Pod", POD), "exec")

    text = read_line + "\n" + exec_line
    needles = {
        "Secret 평문": TOKEN,
        "Secret base64 값": ENCODED_TOKEN,
        "exec 명령": " ".join(EXEC_COMMAND),
        "마운트 경로": MOUNT,
        "exec stdout": payload["stdout"].strip(),
        "Secret 데이터 키": '"data"',
        "명령 필드": "command",
        "출력 필드": "stdout",
    }
    for label, needle in needles.items():
        assert needle not in text, f"레코드에 {label}이 실렸다:\n{text}"


async def run() -> None:
    url = base_url()
    _fixtures()
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        set_auto_response(gate, "AUTO_APPROVE")
        try:
            async with open_session(url) as session:
                print("--- event-log/시나리오 5 (Secret 읽기 + exec 왕복 → 같은 실행의 레코드) ---")
                await test_the_values_are_in_the_responses_and_not_in_the_records(session)
        finally:
            set_auto_response(gate, "NONE")
    print("ok: test-event-log.md#시나리오 5")


if __name__ == "__main__":
    asyncio.run(run())
