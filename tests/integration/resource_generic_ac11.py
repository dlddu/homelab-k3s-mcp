"""컨테이너 안에서 명령 실행 — stdout/stderr 분리, 컨테이너 지목, 상한과 잘림 표시.

검증 시나리오: test-resource-generic.md#시나리오 11
실행 대상: primary

Go 단위 ``internal/mcp/resource_test.go::TestExecStreamsAndCaps`` 가 인자 전달과 상한
표기를 이미 단언한다. 이 파일이 재는 것은 **실물 kubelet 왕복**이다 — SPDY 스트림이
실제로 두 갈래로 갈라져 오는지, 다중 컨테이너 파드에서 `container` 를 빼면 **apiserver
자신이** 후보 이름을 실어 거절하는지, 그리고 끝없이 뱉는 명령이 바이트 상한에서 잘리고
그 사실이 응답에 표시되는지.

대상은 기존 픽스처 ``tests/k8s/kind/resource-generic-fixture.yaml`` 의
`resource-generic-multi` 파드다(컨테이너 `chatty`=busybox · `quiet`=pause). 이 렌즈의
산출물은 e2e 뿐이라 픽스처를 새로 만들지 않고 이미 있는 다중 컨테이너 워크로드를 쓴다.

`resource_exec` 은 **언제나** 승인을 거치므로 모든 케이스가 승인 댄스를 탄다. 컨테이너
누락 케이스도 마찬가지다 — 그 거절은 인자 검증이 아니라 승인 뒤 kubelet 이 내는
답이기 때문이다(그것이 「후보 이름이 제시된다」는 기대의 출처다).
"""

from __future__ import annotations

import asyncio
import subprocess

from _gatekeeper import decide, gatekeeper_url, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"
MULTI_WORKLOAD = "resource-generic-multi"
CHATTY = "chatty"
QUIET = "quiet"

STDOUT_MARK = "rg-ac11-on-stdout"
STDERR_MARK = "rg-ac11-on-stderr"

#: ``internal/k8s/exec.go::execMaxOutputBytes``.
MAX_OUTPUT_BYTES = 256 * 1024


def _pod_name() -> str:
    name = subprocess.check_output(
        [
            "kubectl", "-n", NAMESPACE, "get", "pods",
            "-l", f"app.kubernetes.io/name={MULTI_WORKLOAD}",
            "--field-selector=status.phase=Running",
            "-o", "jsonpath={.items[0].metadata.name}",
        ],
        text=True,
    ).strip()
    assert name, f"{MULTI_WORKLOAD} 의 Running 파드를 찾지 못했다"
    return name


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
