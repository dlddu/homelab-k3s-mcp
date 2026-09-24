"""생성은 덮어쓰지 않는다 — 409, 문서별 승인, 거절 시 무생성, 부분 실패의 무롤백.

검증 시나리오: test-resource-generic.md#시나리오 7
실행 대상: primary
병렬 레인: gate-objects

Go 단위(``internal/k8s/create_test.go`` · ``internal/mcp/create_test.go``)가 HTTP 경계와
디스패처를 이미 덮는다. 이 파일이 더하는 것은 **실물 apiserver 와 실물 gatekeeper 에서
같은 계약이 성립하는가**이며, 특히 두 가지는 여기서만 관측된다.

1. **409 를 apiserver 자신이 낸다.** 스텁이 아니라 실 apiserver 가 같은 이름의 두 번째
   생성을 거절하고, 그 거절이 기존 객체를 **건드리지 않은 채** 돌아온다.
2. **거절과 부분 실패가 남기는 흔적이 서로 다르다.** 둘째 문서의 승인을 거절하면
   첫째 문서의 객체조차 생기지 않고(승인을 전부 받은 뒤에 첫 생성을 시작하므로),
   둘 다 승인받고 둘째가 실행에서 409 면 첫째 객체는 **남는다**(되돌리려면 승인 없는
   delete 가 필요하므로 되돌리지 않는다). 그 차이를 실물에서 눈으로 가른다.

문서가 둘인 호출은 승인 요청도 둘이고, 게이트는 문서 순서대로 하나씩 묻는다. 그래서
댄스는 `wait_for_pending(첫째 이름)` → 판정 → `wait_for_pending(둘째 이름)` → 판정이다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from mcp.shared.exceptions import McpError

from _gatekeeper import decide, gatekeeper_url, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"

SINGLE_MAP = "rg-ac7-configmap"
PAIR_A = ("rg-ac7-dep-a", "rg-ac7-svc-a")
PAIR_B = ("rg-ac7-dep-b", "rg-ac7-svc-b")
PARTIAL_DEPLOYMENT = "rg-ac7-dep-c"

LABEL = "app.kubernetes.io/name"


def _delete(kind: str, name: str) -> None:
    subprocess.run(
        ["kubectl", "-n", NAMESPACE, "delete", kind, name, "--ignore-not-found"],
        check=False,
        capture_output=True,
    )


def _exists(kind: str, name: str) -> bool:
    return (
        subprocess.run(
            ["kubectl", "-n", NAMESPACE, "get", kind, name], capture_output=True
        ).returncode
        == 0
    )


def _get(kind: str, name: str, path: str) -> str:
    return subprocess.run(
        ["kubectl", "-n", NAMESPACE, "get", kind, name, "-o", f"jsonpath={{{path}}}"],
        text=True,
        check=True,
        capture_output=True,
    ).stdout.strip()


def _deployment(name: str) -> dict:
    return {
        "apiVersion": "apps/v1",
        "kind": "Deployment",
        "metadata": {"name": name, "namespace": NAMESPACE, "labels": {LABEL: name}},
        "spec": {
            "replicas": 1,
            "selector": {"matchLabels": {LABEL: name}},
            "template": {
                "metadata": {"labels": {LABEL: name}},
                "spec": {
                    "terminationGracePeriodSeconds": 0,
                    "containers": [{"name": "quiet", "image": "registry.k8s.io/pause:3.9"}],
                },
            },
        },
    }


def _service(name: str) -> dict:
    return {
        "apiVersion": "v1",
        "kind": "Service",
        "metadata": {"name": name, "namespace": NAMESPACE},
        "spec": {
            "selector": {LABEL: name},
            "ports": [{"name": "http", "port": 80, "targetPort": 80}],
        },
    }


def _config_map(name: str, value: str) -> dict:
    return {
        "apiVersion": "v1",
        "kind": "ConfigMap",
        "metadata": {"name": name, "namespace": NAMESPACE},
        "data": {"key": value},
    }


def _stream(*documents: dict) -> str:
    """여러 문서를 `---` 로 이은 매니페스트 스트림. JSON 은 YAML 의 부분집합이다."""
    return "\n---\n".join(json.dumps(document) for document in documents)


def _clean() -> None:
    _delete("configmap", SINGLE_MAP)
    for deployment, service in (PAIR_A, PAIR_B):
        _delete("deployment", deployment)
        _delete("service", service)
    _delete("deployment", PARTIAL_DEPLOYMENT)


async def _decide_each(gate, markers: list[str], verdicts: list[str]) -> list[dict]:
    rows = []
    for marker, verdict in zip(markers, verdicts):
        row = await wait_for_pending(gate, marker)
        await decide(gate, row["id"], verdict)
        rows.append(row)
    return rows


async def test_the_first_create_lands_and_the_second_is_refused_with_409(session, gate) -> None:
    task = asyncio.create_task(
        session.call_tool("resource_create", {"manifest": _config_map(SINGLE_MAP, "first")})
    )
    await _decide_each(gate, [SINGLE_MAP], ["APPROVED"])
    first = await task
    assert first.isError is False, first
    assert _get("configmap", SINGLE_MAP, ".data.key") == "first", "첫 생성이 착지하지 않았다"

    task = asyncio.create_task(
        session.call_tool("resource_create", {"manifest": _config_map(SINGLE_MAP, "second")})
    )
    await _decide_each(gate, [SINGLE_MAP], ["APPROVED"])
    second = await task
    assert second.isError is True, f"같은 이름의 두 번째 생성이 성공했다: {second}"
    failed = second.structuredContent["failed"]
    assert failed and failed["name"] == SINGLE_MAP, second.structuredContent
    assert "409" in failed["error"] or "already exists" in failed["error"], failed
    assert _get("configmap", SINGLE_MAP, ".data.key") == "first", (
        "생성 승인이 갱신까지 해 버렸다 — 기존 값이 덮였다"
    )


async def test_a_two_document_manifest_asks_twice(session, gate) -> None:
    deployment, service = PAIR_A
    task = asyncio.create_task(
        session.call_tool(
            "resource_create",
            {"manifest": _stream(_deployment(deployment), _service(service))},
        )
    )
    rows = await _decide_each(gate, [deployment, service], ["APPROVED", "APPROVED"])
    result = await task

    assert result.isError is False, result
    assert rows[0]["id"] != rows[1]["id"], f"두 문서가 승인 요청 하나를 나눠 썼다: {rows}"
    created = {item["name"] for item in result.structuredContent["created"]}
    assert created == {deployment, service}, result.structuredContent
    assert _exists("deployment", deployment) and _exists("service", service)


async def test_a_rejection_creates_nothing_at_all(session, gate) -> None:
    deployment, service = PAIR_B
    task = asyncio.create_task(
        session.call_tool(
            "resource_create",
            {"manifest": _stream(_deployment(deployment), _service(service))},
        )
    )
    await _decide_each(gate, [deployment, service], ["APPROVED", "REJECTED"])
    try:
        await task
    except McpError as exc:
        assert "reject" in str(exc).lower(), exc
    else:
        raise AssertionError("둘째 문서가 거절됐는데 호출이 성공했다")

    assert not _exists("deployment", deployment), (
        "거절된 호출이 첫째 문서의 객체를 남겼다 — 승인 전량 선행이 깨졌다"
    )
    assert not _exists("service", service), "거절된 호출이 둘째 문서의 객체를 만들었다"


async def test_a_partial_failure_is_reported_and_not_rolled_back(session, gate) -> None:
    existing_service = PAIR_A[1]
    task = asyncio.create_task(
        session.call_tool(
            "resource_create",
            {
                "manifest": _stream(
                    _deployment(PARTIAL_DEPLOYMENT), _service(existing_service)
                )
            },
        )
    )
    await _decide_each(gate, [PARTIAL_DEPLOYMENT, existing_service], ["APPROVED", "APPROVED"])
    result = await task

    assert result.isError is True, f"둘째 문서가 409 인데 호출이 성공으로 보고됐다: {result}"
    payload = result.structuredContent
    created = {item["name"] for item in payload["created"]}
    assert created == {PARTIAL_DEPLOYMENT}, payload
    assert payload["failed"]["name"] == existing_service, payload
    assert payload["unattempted"] == [], payload
    assert _exists("deployment", PARTIAL_DEPLOYMENT), (
        "부분 실패가 이미 만들어진 객체를 되돌렸다 — 되돌리려면 승인 없는 delete 가 필요하다"
    )


async def run() -> None:
    url = base_url()
    _clean()
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- resource-generic/시나리오 7 (단건 생성과 409) ---")
            await test_the_first_create_lands_and_the_second_is_refused_with_409(session, gate)
            print("--- resource-generic/시나리오 7 (2문서 = 승인 2건) ---")
            await test_a_two_document_manifest_asks_twice(session, gate)
            print("--- resource-generic/시나리오 7 (거절은 아무것도 만들지 않는다) ---")
            await test_a_rejection_creates_nothing_at_all(session, gate)
            print("--- resource-generic/시나리오 7 (부분 실패는 되돌리지 않는다) ---")
            await test_a_partial_failure_is_reported_and_not_rolled_back(session, gate)
    print("ok: test-resource-generic.md#시나리오 7")


if __name__ == "__main__":
    asyncio.run(run())
