"""삭제는 단건만 — 지정 객체만 사라지고 유예 기간이 그대로 반영된다.

검증 시나리오: test-resource-generic.md#시나리오 10
실행 대상: primary
병렬 레인: gate-objects

「셀렉터 전용 호출 경로가 존재하지 않아 인자 검증에서 거부됨」과 「subresource
경로가 없다」는 단위 층이 이미 덮는다(``TestDeleteIsSingleObjectOnly`` — 그
거부는 승인 요청이 만들어지기 전에 일어난다). 이 파일은 거부가 승인 요청을 만들지
않았음을 PENDING 카운트로 나란히 고정하고, 단위 층이 볼 수 없는 apiserver 의
거동 두 가지를 잰다: 지정 객체만 사라지고 같은 레이블의 다른 객체는 남는 것,
그리고 ``gracePeriodSeconds`` 가 유예로 반영되는 것.

유예 관측의 파드는 SIGTERM 을 무시하는 컨테이너를 쓴다 — `pause` 는 시그널을
받자마자 끝나 유예가 0초짜리로 관측되기 때문이다. 기준선 워크로드는 건드리지
않고 일회용 파드를 kubectl 로 세운다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess
import time

from mcp.shared.exceptions import McpError

from _gatekeeper import decide, gatekeeper_url, list_requests, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE

LABEL = "gk-ac10"
TARGET_MAP = "gk-ac10-target"
SURVIVOR_MAP = "gk-ac10-survivor"
GRACE_POD = "gk-ac10-grace-pod"
#: 시나리오가 부르는 값. 「반영」의 관측은 이 값이 apiserver 에 닿았다는 것 — 기본
#: 유예(30초)가 흘렀다면 SIGTERM 을 무시하는 아래 컨테이너는 30초를 버텼을 것이다.
GRACE_SECONDS = 0
#: 위 관측의 예산. 기본 유예보다 짧아야 판별식이 성립한다.
GRACE_OBSERVATION_BUDGET = 15
#: SIGTERM 을 무시하는 컨테이너 — 유예 0 이 SIGKILL 로 오는 것과 기본 유예가 흐르는
#: 것을 구별하려면 컨테이너가 TERM 에 스스로 끝나지 않아야 한다.
GRACE_IMAGE = "busybox:1.36"


def _kubectl(*args: str, input: str | None = None) -> str:
    proc = subprocess.run(
        ["kubectl", "-n", NAMESPACE, *args],
        capture_output=True,
        text=True,
        check=True,
        input=input,
    )
    return proc.stdout


def _kubectl_ok(*args: str) -> bool:
    return (
        subprocess.run(
            ["kubectl", "-n", NAMESPACE, *args], capture_output=True, text=True
        ).returncode
        == 0
    )


def _config_map(name: str) -> None:
    _kubectl(
        "apply", "-f", "-",
        input=json.dumps(
            {
                "apiVersion": "v1",
                "kind": "ConfigMap",
                "metadata": {"name": name, "namespace": NAMESPACE, "labels": {LABEL: "yes"}},
                "data": {"key": "value"},
            }
        ),
    )


def _grace_pod() -> None:
    _kubectl(
        "run", GRACE_POD,
        "--image", GRACE_IMAGE,
        "--restart", "Never",
        "--command", "--",
        "sh", "-c", 'trap "" TERM; sleep 600',
    )
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        out = _kubectl("get", "pod", GRACE_POD, "-o", "jsonpath={.status.phase}").strip()
        if out == "Running":
            return
        time.sleep(2)
    raise AssertionError(f"{GRACE_POD} 가 Running 이 되지 않았다")


def _pending_count(gate: str, marker: str) -> int:
    return sum(
        1 for r in list_requests(gate, status="PENDING") if marker in r.get("context", "")
    )


def _map_exists(name: str) -> bool:
    return _kubectl_ok("get", "configmap", name)


async def run() -> None:
    url = base_url()
    _config_map(TARGET_MAP)
    _config_map(SURVIVOR_MAP)
    _grace_pod()
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- resource-generic/시나리오 10 (셀렉터·서브리소스는 승인 전 거부) ---")
            for args, fragment in (
                ({"labelSelector": f"{LABEL}=yes"}, "labelSelector does not apply"),
                ({"fieldSelector": f"metadata.name={TARGET_MAP}"}, "fieldSelector does not apply"),
                ({"name": TARGET_MAP, "subresource": "status"}, "subresource does not apply"),
            ):
                before = _pending_count(gate, TARGET_MAP)
                try:
                    await session.call_tool(
                        "resource_delete",
                        {
                            "apiVersion": "v1",
                            "kind": "ConfigMap",
                            "namespace": NAMESPACE,
                            **args,
                        },
                    )
                except McpError as exc:
                    assert fragment in str(exc), (fragment, exc)
                else:
                    raise AssertionError(f"{args} 가 거부되지 않았다")
                assert _pending_count(gate, TARGET_MAP) == before, (
                    "거부가 승인 요청을 만들었다"
                )

            print("--- resource-generic/시나리오 10 (지정 객체만 삭제) ---")
            task = asyncio.create_task(
                session.call_tool(
                    "resource_delete",
                    {
                        "apiVersion": "v1",
                        "kind": "ConfigMap",
                        "namespace": NAMESPACE,
                        "name": TARGET_MAP,
                    },
                )
            )
            row = await wait_for_pending(gate, TARGET_MAP)
            await decide(gate, row["id"], "APPROVED")
            result = await task
            assert result.isError is False, result
            payload = result.structuredContent
            assert payload["accepted"] is True, payload
            deadline = time.monotonic() + 60
            while time.monotonic() < deadline and _map_exists(TARGET_MAP):
                time.sleep(2)
            assert not _map_exists(TARGET_MAP), "지정 객체가 사라지지 않았다"
            assert _map_exists(SURVIVOR_MAP), "같은 레이블의 다른 객체까지 사라졌다"

            print("--- resource-generic/시나리오 10 (gracePeriodSeconds=0 반영) ---")
            task = asyncio.create_task(
                session.call_tool(
                    "resource_delete",
                    {
                        "apiVersion": "v1",
                        "kind": "Pod",
                        "namespace": NAMESPACE,
                        "name": GRACE_POD,
                        "gracePeriodSeconds": GRACE_SECONDS,
                    },
                )
            )
            row = await wait_for_pending(gate, GRACE_POD)
            await decide(gate, row["id"], "APPROVED")
            result = await task
            assert result.isError is False, result
            assert result.structuredContent["gracePeriodSeconds"] == GRACE_SECONDS

            started = time.monotonic()
            deadline = started + GRACE_OBSERVATION_BUDGET
            while time.monotonic() < deadline and _kubectl_ok("get", "pod", GRACE_POD):
                time.sleep(1)
            assert not _kubectl_ok("get", "pod", GRACE_POD), (
                f"gracePeriodSeconds=0 이 반영되지 않았다 — {GRACE_OBSERVATION_BUDGET}초 "
                "안에도 파드가 남아 있다(기본 유예가 흘렀을 수 있다)"
            )
            print(f"grace ok: 유예 0 의 파드가 {time.monotonic() - started:.0f}초 만에 사라졌다")
    print("ok: test-resource-generic.md#시나리오 10")


if __name__ == "__main__":
    asyncio.run(run())
