"""컨테이너 안에서 명령 실행 — stdout/stderr 분리, 컨테이너 지목, 상한과 잘림 표시.

검증 시나리오: test-resource-generic.md#시나리오 11
실행 대상: primary
병렬 레인: gate-streams

**픽스처 파드는 기다려서 집는다.** `app.kubernetes.io/name` 은
`resource-generic-fixture.yaml` 이 **Deployment 메타데이터**에만 다는 값이고 파드 템플릿이
다는 것은 `app` 하나다. 파드를 그 이름으로 찾으면 클러스터가 아무리 건강해도 목록은
**항상 비어** 있다.

같은 파드를 `resource_generic_sc5.py`(`resource-generic` 레인)도 읽지만 레인을 갈라도 되는
것은, 그 파일이 재는 컨테이너 로그에 exec 스트림의 출력이 실리지 않기 때문이다. 이 파일이
`chatty` 의 **로그에** 무언가를 남기는 명령을 쓰게 되면 두 파일을 한 레인으로 모은다.

⚠️ `yes` 왕복에 `isError` 를 단언하지 않는 것은 의도다 — 잘린 스트림은 종료 코드를 보고한
적이 없어 `success` 가 거짓이고 핸들러의 `isError` 는 `!success` 다. 이 렌즈가 재는 것은
「잘렸다는 사실이 응답 본문으로 돌아오는가」이고, AC12 는 그 플래그가 어떤 `isError` 를
달고 오는지 규정하지 않는다.
"""

from __future__ import annotations

import asyncio
import subprocess
import time

from _gatekeeper import decide, gatekeeper_url, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"
MULTI_WORKLOAD = "resource-generic-multi"
CHATTY = "chatty"
QUIET = "quiet"

STDOUT_MARK = "rg-ac11-on-stdout"
STDERR_MARK = "rg-ac11-on-stderr"

#: ``internal/k8s/exec.go::streamMaxOutputBytes``.
MAX_OUTPUT_BYTES = 256 * 1024


def _pod_name(timeout: float = 180.0) -> str:
    """두 컨테이너가 다 Ready 인 픽스처 파드 이름을 돌려준다(없으면 기다린다).

    `check_output` 이 아니라 `run(check=False)` 를 쓰는 이유: 빈 목록에 걸린
    `jsonpath={.items[0]…}` 은 `array index out of bounds` 로 rc=1 이라, 그대로 두면
    아래 단언에 **닿기 전에** `CalledProcessError` 가 먼저 튄다 — 직전 판이 실제로 그렇게
    깨졌고, 호출자는 「픽스처가 아직 없다」 대신 kubectl 인자 덤프를 읽게 된다.
    """
    deadline = time.monotonic() + timeout
    last = "<no probe yet>"
    while True:
        probe = subprocess.run(
            [
                "kubectl", "-n", NAMESPACE, "get", "pods",
                "-l", f"app={MULTI_WORKLOAD}",
                "--field-selector=status.phase=Running",
                "-o", "jsonpath={.items[0].metadata.name}|"
                "{range .items[0].status.containerStatuses[*]}{.name}={.ready},{end}",
            ],
            capture_output=True,
            text=True,
        )
        last = probe.stdout.strip() or probe.stderr.strip()
        if probe.returncode == 0 and probe.stdout.count("|") == 1:
            name, statuses = probe.stdout.split("|")
            ready = dict(
                entry.split("=") for entry in statuses.split(",") if "=" in entry
            )
            if name and ready == {CHATTY: "true", QUIET: "true"}:
                print(f"precondition ok: {name} containers={sorted(ready)} (둘 다 Ready)")
                return name
        if time.monotonic() >= deadline:
            raise AssertionError(
                f"{NAMESPACE} 의 app={MULTI_WORKLOAD} 파드가 {timeout:.0f}s 안에 "
                f"컨테이너 {CHATTY}·{QUIET} 둘 다 Ready 인 Running 상태가 되지 않았다 "
                f"(마지막 관측: {last!r})"
            )
        time.sleep(3)


async def _approved_exec(session, gate, pod: str, args: dict):
    task = asyncio.create_task(
        session.call_tool(
            "resource_exec",
            {"apiVersion": "v1", "kind": "Pod", "namespace": NAMESPACE, "name": pod, **args},
        )
    )
    row = await wait_for_pending(gate, pod)
    await decide(gate, row["id"], "APPROVED")
    return await task


async def test_stdout_and_stderr_come_back_separately(session, gate, pod: str) -> None:
    result = await _approved_exec(
        session, gate, pod,
        {"container": CHATTY, "command": ["echo", STDOUT_MARK]},
    )
    # 성공 갈래 — #137 이 닫은 첫 자리다. 이 세 줄이 붉어지면 0 으로 끝난 명령의 종료
    # 코드가 다시 응답에 실리지 않는 것이고, 그 아래 「거부」 단언들은 모두 공허해진다.
    assert result.isError is False, f"성공한 exec 가 에러로 보고됐다: {result}"
    payload = result.structuredContent
    assert payload["success"] is True, payload
    assert payload["exitCode"] == 0, payload
    assert STDOUT_MARK in payload["stdout"], payload
    assert payload["stderr"] == "", f"조용한 명령이 stderr 를 남겼다: {payload}"

    result = await _approved_exec(
        session, gate, pod,
        {
            "container": CHATTY,
            "command": ["sh", "-c", f"echo {STDOUT_MARK}; echo {STDERR_MARK} 1>&2"],
        },
    )
    payload = result.structuredContent
    assert STDOUT_MARK in payload["stdout"], payload
    assert STDERR_MARK in payload["stderr"], payload
    assert STDERR_MARK not in payload["stdout"], (
        f"두 갈래가 한 스트림으로 합쳐져 왔다: {payload}"
    )
    assert STDOUT_MARK not in payload["stderr"], payload


async def test_an_omitted_container_is_refused_with_the_candidates(session, gate, pod: str) -> None:
    result = await _approved_exec(session, gate, pod, {"command": ["echo", "no-container"]})
    assert result.isError is True, f"다중 컨테이너 파드에서 container 누락이 실행됐다: {result}"
    text = result.content[0].text
    assert CHATTY in text and QUIET in text, (
        f"거절이 후보 컨테이너 이름을 제시하지 않았다: {text[:400]}"
    )


async def test_endless_output_is_cut_at_the_cap_and_says_so(session, gate, pod: str) -> None:
    result = await _approved_exec(
        session, gate, pod,
        {"container": CHATTY, "command": ["yes", "rg-ac11-flood"]},
    )
    payload = result.structuredContent
    assert payload is not None, (
        f"잘린 왕복이 구조화 본문 없이 돌아왔다 — 잘림 표시를 실을 자리가 없다: {result}"
    )
    assert payload["stdoutTruncated"] is True, (
        f"끝없는 출력이 잘렸다는 표시 없이 돌아왔다 — 호출자는 이것을 명령이 스스로 "
        f"끝난 것으로 읽는다: {({k: v for k, v in payload.items() if k != 'stdout'})}"
    )
    assert len(payload["stdout"].encode()) <= MAX_OUTPUT_BYTES, (
        f"출력이 상한 {MAX_OUTPUT_BYTES}B 를 넘겨 돌아왔다: {len(payload['stdout'].encode())}B"
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    pod = _pod_name()

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- resource-generic/시나리오 11 (stdout·stderr 분리) ---")
            await test_stdout_and_stderr_come_back_separately(session, gate, pod)
            print("--- resource-generic/시나리오 11 (container 누락) ---")
            await test_an_omitted_container_is_refused_with_the_candidates(session, gate, pod)
            print("--- resource-generic/시나리오 11 (출력 상한과 잘림 표시) ---")
            await test_endless_output_is_cut_at_the_cap_and_says_so(session, gate, pod)
    print("ok: test-resource-generic.md#시나리오 11")


if __name__ == "__main__":
    asyncio.run(run())
