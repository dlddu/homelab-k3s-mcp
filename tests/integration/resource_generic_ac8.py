"""전체 교체와 스케일 — replicas 반영과 PUT 교체가 승인 뒤 apiserver 에서 일어난다.

검증 시나리오: test-resource-generic.md#시나리오 8
실행 대상: primary
병렬 레인: resource-generic

거부 셋은 두 층으로 갈린다. 음수·누락은 **승인 요청조차 만들기 전에** 거부된다
(인자 검증이 게이트보다 앞서다 — gate.go::updatePairs →
resource.go::parseUpdateTarget) — 그래서 「새 PENDING 요청 0건」을 나란히 단언해
거부가 어느 층에서 일어났는지를 고정한다. 반면 DaemonSet 의 레플리카 부재 거부는
승인 뒤 k8s 층에서 일어난다 — 좌표 해석은 좌표 자체만으로는 알 수 없는 사실을
묻는다. 승인을 받고 나서야 보이는 거부를 승인 없이 흉내 내면 반쪽 단정이다.

3·0·1 의 레플리카 수렴과 파드 기동은 apiserver 의 거동이라 단위 층이 볼 수 없는
부분이다 — status 를 폴링해 잰다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess
import time

from mcp.shared.exceptions import McpError

from _gatekeeper import decide, gatekeeper_url, list_requests, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz
from _workload import (
    DS_WORKLOAD,
    NAMESPACE,
    WORKLOAD,
    ensure_workload_fixture_baseline,
)

SCALE_ANNO = "gk-ac8-marker"

#: DaemonSet 거부의 사유. 권한이나 존재 여부가 아니라 레플리카 부재여야 한다 —
#: `internal/k8s/resource.go::scaleTargetRefFor` 가 고정하는 문면의 앵커다.
REPLICA_LESS_REASON = "has no replicas"


def _resource_version(kind: str, name: str) -> str:
    return subprocess.check_output(
        [
            "kubectl", "-n", NAMESPACE, "get", kind, name,
            "-o", "jsonpath={.metadata.resourceVersion}",
        ],
        text=True,
    ).strip()


def _wait_quiet(kind: str, name: str, settle: float = 3.0, timeout: float = 120.0) -> None:
    deadline = time.monotonic() + timeout
    last = _resource_version(kind, name)
    since = time.monotonic()
    while time.monotonic() < deadline:
        time.sleep(0.5)
        now = _resource_version(kind, name)
        if now != last:
            last, since = now, time.monotonic()
        elif time.monotonic() - since >= settle:
            return
    raise AssertionError(
        f"{kind}/{name} 의 resourceVersion 이 {timeout:.0f}초 안에 멎지 않았다"
    )


def _deployment() -> dict:
    out = subprocess.check_output(
        [
            "kubectl", "-n", NAMESPACE, "get", "deployment", WORKLOAD,
            "-o", "json",
        ],
        text=True,
    )
    return json.loads(out)


def _status_replicas() -> int:
    out = subprocess.check_output(
        [
            "kubectl", "-n", NAMESPACE, "get", "deployment", WORKLOAD,
            "-o", "jsonpath={.status.replicas}",
        ],
        text=True,
    )
    return int(out or "0")


def _wait_replicas(expected: int, timeout: float = 120.0) -> None:

    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if _status_replicas() == expected:
            return
        time.sleep(2)
    raise AssertionError(
        f"{WORKLOAD} status.replicas 가 {timeout:.0f}초 안에 {expected} 로 수렴하지 않았다 "
        f"(마지막: {_status_replicas()})"
    )


def _pending_count(gate: str, marker: str) -> int:
    return sum(
        1 for r in list_requests(gate, status="PENDING") if marker in r.get("context", "")
    )


async def _approved_scale(session, gate: str, replicas: int):
    task = asyncio.create_task(
        session.call_tool(
            "resource_update",
            {
                "apiVersion": "apps/v1",
                "kind": "Deployment",
                "namespace": NAMESPACE,
                "name": WORKLOAD,
                "subresource": "scale",
                "replicas": replicas,
            },
        )
    )
    row = await wait_for_pending(gate, WORKLOAD)
    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result
    return result


async def run() -> None:
    url = base_url()
    ensure_workload_fixture_baseline()
    _wait_quiet("deployment", WORKLOAD)
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- resource-generic/시나리오 8 (scale 3 → 0 → 1) ---")
            for target in (3, 0, 1):
                _wait_quiet("deployment", WORKLOAD)
                await _approved_scale(session, gate, target)
                _wait_replicas(target)
                print(f"scale ok: replicas={target}")

            print("--- resource-generic/시나리오 8 (음수·누락은 승인 전 거부) ---")
            before = _pending_count(gate, WORKLOAD)
            try:
                await session.call_tool(
                    "resource_update",
                    {
                        "apiVersion": "apps/v1",
                        "kind": "Deployment",
                        "namespace": NAMESPACE,
                        "name": WORKLOAD,
                        "subresource": "scale",
                        "replicas": -1,
                    },
                )
            except McpError as exc:
                assert "replicas must be >= 0" in str(exc), exc
            else:
                raise AssertionError("replicas=-1 이 거부되지 않았다")
            try:
                await session.call_tool(
                    "resource_update",
                    {
                        "apiVersion": "apps/v1",
                        "kind": "Deployment",
                        "namespace": NAMESPACE,
                        "name": WORKLOAD,
                        "subresource": "scale",
                    },
                )
            except McpError as exc:
                assert "replicas is required" in str(exc), exc
            else:
                raise AssertionError("replicas 누락이 거부되지 않았다")
            after = _pending_count(gate, WORKLOAD)
            assert before == after, (
                f"거부된 호출이 승인 요청을 만들었다: {before} → {after}"
            )

            print("--- resource-generic/시나리오 8 (DaemonSet 은 승인 뒤 거부) ---")
            _wait_quiet("daemonset", DS_WORKLOAD)
            task = asyncio.create_task(
                session.call_tool(
                    "resource_update",
                    {
                        "apiVersion": "apps/v1",
                        "kind": "DaemonSet",
                        "namespace": NAMESPACE,
                        "name": DS_WORKLOAD,
                        "subresource": "scale",
                        "replicas": 2,
                    },
                )
            )
            row = await wait_for_pending(gate, DS_WORKLOAD)
            await decide(gate, row["id"], "APPROVED")
            result = await task
            assert result.isError is True, result
            assert result.content, result
            message = result.content[0].text
            assert REPLICA_LESS_REASON in message, message

            print("--- resource-generic/시나리오 8 (서브리소스 없는 전체 교체) ---")
            _wait_quiet("deployment", WORKLOAD)
            manifest = _deployment()
            manifest["metadata"].setdefault("annotations", {})[SCALE_ANNO] = "applied"
            task = asyncio.create_task(
                session.call_tool(
                    "resource_update",
                    {
                        "apiVersion": "apps/v1",
                        "kind": "Deployment",
                        "namespace": NAMESPACE,
                        "name": WORKLOAD,
                        "manifest": manifest,
                    },
                )
            )
            row = await wait_for_pending(gate, SCALE_ANNO)
            await decide(gate, row["id"], "APPROVED")
            result = await task
            assert result.isError is False, result
            replaced = result.structuredContent["object"]
            assert replaced["metadata"]["annotations"][SCALE_ANNO] == "applied", replaced
            assert replaced["spec"]["replicas"] == manifest["spec"]["replicas"], (
                "전체 교체가 레플리카를 다르게 썼다"
            )
            _wait_replicas(manifest["spec"]["replicas"])
    print("ok: test-resource-generic.md#시나리오 8")


if __name__ == "__main__":
    asyncio.run(run())
