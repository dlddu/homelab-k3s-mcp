"""부분 수정과 롤링 재시작 — 네 patchType 과 재시작 패치가 각각 승인 뒤 적용된다.

검증 시나리오: test-resource-generic.md#시나리오 9
실행 대상: primary

네 patchType 은 각각 별도의 승인을 요구한다 — 내용으로 예외를 주지 않는 것이
시나리오의 계약이고, 그래서 각 적용 앞에 승인 댄스가 온다. 재시작 패치는
strategic patch 로 가며, 파드 교체와 spec.replicas 보존은 apiserver 의 거동이라
단위 층이 볼 수 없다 — 이 파일이 관측한다. 「그 어노테이션 외 어떤 필드도
달라지지 않음」은 패치 전 spec 전문과의 비교로 잰다. 파드 이름은 apiserver 가
붙이는 난수라 **uid 로** 비교한다 — 이름이 같아도 교체는 일어날 수 있다.

json patch 의 값은 승인 화면에서 가려진다(AC10 의 마스킹이 json patch 의 값을
본다) — 그래서 승인 요청 격리 마커는 값이 아니라 **path** 에 둔다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess
import time

from _gatekeeper import decide, gatekeeper_url, get_request, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE, WORKLOAD, ensure_workload_fixture_baseline

BASELINE_REPLICAS = 2
RESTART_ANNOTATION = "kubectl.kubernetes.io/restartedAt"

MERGE_KEY = "gk-ac9-merge"
STRATEGIC_KEY = "gk-ac9-strategic"
JSON_PATH = "/metadata/annotations/gk-ac9-json"
APPLY_KEY = "gk-ac9-apply"
APPLY_FIELD_MANAGER = "gk-ac9-e2e"


def _kubectl(*args: str, check: bool = True) -> str:
    proc = subprocess.run(
        ["kubectl", "-n", NAMESPACE, *args],
        capture_output=True,
        text=True,
        check=check,
    )
    return proc.stdout


def _deployment() -> dict:
    return json.loads(_kubectl("get", "deployment", WORKLOAD, "-o", "json"))


def _set_baseline_replicas() -> None:
    _kubectl("scale", "deployment", WORKLOAD, f"--replicas={BASELINE_REPLICAS}")
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        if _deployment()["spec"]["replicas"] == BASELINE_REPLICAS:
            return
        time.sleep(2)
    raise AssertionError("기준선 레플리카 세팅이 수렴하지 않았다")


def _pod_uids() -> set[str]:
    out = _kubectl("get", "pods", "-l", f"app={WORKLOAD}", "-o", "json")
    return {item["metadata"]["uid"] for item in json.loads(out)["items"]}


def _wait_pod_replacement(before: set[str], timeout: float = 180.0) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        now = _pod_uids()
        if before.isdisjoint(now) and now:
            return
        time.sleep(3)
    raise AssertionError(f"파드가 {timeout:.0f}초 안에 교체되지 않았다")


async def _approved_patch(session, gate: str, args: dict, marker: str) -> dict:
    task = asyncio.create_task(session.call_tool("resource_patch", args))
    row = await wait_for_pending(gate, marker)
    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result
    return result.structuredContent


async def run() -> None:
    url = base_url()
    ensure_workload_fixture_baseline()
    _set_baseline_replicas()
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- resource-generic/시나리오 9 (merge·strategic·json·apply) ---")
            spec_before = _deployment()["spec"]
            for args, marker in (
                (
                    {
                        "apiVersion": "apps/v1", "kind": "Deployment",
                        "namespace": NAMESPACE, "name": WORKLOAD,
                        "patchType": "merge",
                        "patch": {"metadata": {"annotations": {MERGE_KEY: "v1"}}},
                    },
                    MERGE_KEY,
                ),
                (
                    {
                        "apiVersion": "apps/v1", "kind": "Deployment",
                        "namespace": NAMESPACE, "name": WORKLOAD,
                        "patchType": "strategic",
                        "patch": {"metadata": {"annotations": {STRATEGIC_KEY: "v1"}}},
                    },
                    STRATEGIC_KEY,
                ),
                (
                    {
                        "apiVersion": "apps/v1", "kind": "Deployment",
                        "namespace": NAMESPACE, "name": WORKLOAD,
                        "patchType": "json",
                        "patch": [{"op": "add", "path": JSON_PATH, "value": "v1"}],
                    },
                    "gk-ac9-json",
                ),
                (
                    {
                        "apiVersion": "apps/v1", "kind": "Deployment",
                        "namespace": NAMESPACE, "name": WORKLOAD,
                        "patchType": "apply",
                        "fieldManager": APPLY_FIELD_MANAGER,
                        "patch": {"metadata": {"annotations": {APPLY_KEY: "v1"}}},
                    },
                    APPLY_KEY,
                ),
            ):
                payload = await _approved_patch(session, gate, args, marker)
                annotations = payload["object"]["metadata"]["annotations"]
                key = marker if marker != "gk-ac9-json" else JSON_PATH.rsplit("/", 1)[-1]
                assert annotations.get(key) == "v1", (key, annotations)

            print("--- resource-generic/시나리오 9 (재시작 패치와 파드 교체) ---")
            pods_before = _pod_uids()
            stamp = str(int(time.time()))
            args = {
                "apiVersion": "apps/v1", "kind": "Deployment",
                "namespace": NAMESPACE, "name": WORKLOAD,
                "patchType": "strategic",
                "patch": {
                    "spec": {
                        "template": {
                            "metadata": {"annotations": {RESTART_ANNOTATION: stamp}}
                        }
                    }
                },
            }
            task = asyncio.create_task(session.call_tool("resource_patch", args))
            row = await wait_for_pending(gate, RESTART_ANNOTATION)
            await decide(gate, row["id"], "APPROVED")
            result = await task
            assert result.isError is False, result
            restarted = result.structuredContent["object"]

            assert restarted["spec"]["template"]["metadata"]["annotations"][
                RESTART_ANNOTATION
            ] == stamp
            _wait_pod_replacement(pods_before)

            after = _deployment()
            assert after["spec"]["replicas"] == BASELINE_REPLICAS, (
                f"재시작 뒤 spec.replicas 가 보존되지 않았다: {after['spec']['replicas']}"
            )

            def _strip(spec: dict) -> dict:
                stripped = json.loads(json.dumps(spec))
                annotations = stripped["template"]["metadata"].get("annotations", {})
                annotations.pop(RESTART_ANNOTATION, None)
                if not annotations:
                    stripped["template"]["metadata"].pop("annotations", None)
                return stripped

            assert _strip(spec_before) == _strip(after["spec"]), (
                "재시작 패치가 그 어노테이션 외의 필드를 바꿨다"
            )

            record = get_request(gate, row["id"])
            assert record["context"].count(RESTART_ANNOTATION) >= 1, (
                "재시작 패치의 본문이 승인 화면(context)에 그대로 담기지 않았다"
            )
    print("ok: test-resource-generic.md#시나리오 9")


if __name__ == "__main__":
    asyncio.run(run())
