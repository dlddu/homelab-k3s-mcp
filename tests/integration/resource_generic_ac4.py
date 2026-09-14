"""단건 조회는 잡음을 걷고 오되 이름을 요구한다.

검증 시나리오: test-resource-generic.md#시나리오 4
실행 대상: primary

`workload-fixture` 로 족한 것은 CI 가 `kubectl apply -f tests/k8s/kind/test-deployment.yaml`
로 세워 잡음 두 필드가 다 붙기 때문이다. 그래도 **사전 조건 자체를 kubectl 로 먼저 단언한다**
— 두 필드가 애초에 없으면 「걷어냈다」가 공전하므로, 잡음이 실재했음을 확인하고 나서
사라졌음을 확인한다.

「나머지 `spec`/`status` 는 온전」은 포함 관계가 아니라 **등가**로 잰다: `spec` 은 원본과
완전히 같아야 하고, `metadata` 의 키 집합은 원본에서 **그 둘만** 뺀 것과 같아야 한다.
포함으로 재면 과잉 제거(잡음 아닌 필드까지 걷어낸 경우)가 통과한다. 값 비교에서
`resourceVersion` 처럼 흔들리는 필드를 피하려고 metadata 는 키 집합으로 보고,
`annotations` 만 값까지 대조한다 — 「그 어노테이션 하나만 빠졌다」가 이 시나리오의 요지라서다.

`name` 누락 케이스는 도구가 **k8s 에 닿기 전에** 인자 검증으로 거부하므로 도구 결과가 아니라
`McpError`(-32602) 로 온다. 메시지가 `resource_list` 를 가리키는지까지 보는 이유는, 시나리오가
요구하는 것이 거부 자체가 아니라 **대상 해석 경로가 존재하지 않는다**는 사실과 그 안내이기
때문이다(셀렉터를 여기서 풀면 list verb 를 행사하게 되어 verb 1:1 이 깨진다).
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from mcp import ClientSession
from mcp.shared.exceptions import McpError

from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE, WORKLOAD, ensure_workload_fixture_baseline

LAST_APPLIED = "kubectl.kubernetes.io/last-applied-configuration"


def _normalized(value):
    """정수값 실수를 정수로 맞춘 사본.

    두 경로가 같은 apiserver JSON 을 보지만 서로 다른 디코더를 지난다(kubectl 은 Go 의
    타입 있는 스킴, 도구는 unstructured). 숫자 표현 차이 하나로 `spec` 등가 단언이 흔들리면
    이 케이스가 말하려는 것(잡음 제거가 spec 을 건드리지 않았다)과 무관한 실패가 된다.
    """
    if isinstance(value, bool):
        return value
    if isinstance(value, float) and value.is_integer():
        return int(value)
    if isinstance(value, dict):
        return {key: _normalized(item) for key, item in value.items()}
    if isinstance(value, list):
        return [_normalized(item) for item in value]
    return value


def _kubectl_whole_object() -> dict:
    """잡음이 붙은 원본 객체 — 도구를 거치지 않은 대조군.

    ⚠️ `--show-managed-fields` 가 필수다. kubectl 은 v1.21 부터 **출력에서**
    `managedFields` 를 기본으로 지운다 — 객체에 없는 것이 아니라 프린터가 감추는 것이라,
    플래그 없이 뜨면 대조군이 이미 잡음 하나를 잃은 상태가 된다. 그러면 아래 사전 조건
    단언이 「원본에 managedFields 가 없다」로 실패하고(첫 CI 에서 실제로 그랬다),
    설령 그 단언이 없었다면 **키 집합 등가 비교가 조용히 헐거워졌을** 자리다.
    """
    raw = subprocess.check_output(
        [
            "kubectl", "get", "deployment", WORKLOAD,
            "-n", NAMESPACE, "-o", "json", "--show-managed-fields",
        ],
        text=True,
    )
    return json.loads(raw)


def test_resource_generic_ac4_precondition_has_both_noise_fields() -> dict:
    """시나리오 4 사전 조건 — 원본에 잡음 두 필드가 실제로 붙어 있다."""
    whole = _kubectl_whole_object()
    metadata = whole["metadata"]
    assert metadata.get("managedFields"), (
        f"원본에 managedFields 가 없다 — 사전 조건 불성립: {sorted(metadata)}"
    )
    assert LAST_APPLIED in metadata.get("annotations", {}), (
        f"원본에 {LAST_APPLIED} 가 없다 — kubectl apply 로 세워지지 않았다: "
        f"{sorted(metadata.get('annotations', {}))}"
    )
    print(
        "precondition ok: managedFields "
        f"{len(metadata['managedFields'])}건 + {LAST_APPLIED}"
    )
    return whole


async def test_resource_generic_ac4_get_strips_only_the_noise(
    session: ClientSession, whole: dict
) -> None:
    """시나리오 4 — 잡음 둘만 사라지고 spec/status 는 온전하다."""
    result = await session.call_tool(
        "resource_get",
        {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": WORKLOAD,
        },
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload is not None, result
    obj = payload["object"]

    metadata = obj["metadata"]
    assert "managedFields" not in metadata, f"managedFields 가 남아 있다: {sorted(metadata)}"
    assert LAST_APPLIED not in metadata.get("annotations", {}), (
        f"{LAST_APPLIED} 가 남아 있다: {sorted(metadata.get('annotations', {}))}"
    )

    raw_metadata = whole["metadata"]
    expected_annotations = {
        key: value
        for key, value in raw_metadata.get("annotations", {}).items()
        if key != LAST_APPLIED
    }
    expected_keys = set(raw_metadata) - {"managedFields"}
    if not expected_annotations:
        # 어노테이션이 last-applied 뿐이었으면 빈 맵을 남기지 않고 키째 지운다.
        expected_keys.discard("annotations")
        assert "annotations" not in metadata, (
            f"비워진 annotations 맵이 남아 있다: {metadata.get('annotations')}"
        )
    else:
        assert metadata.get("annotations") == expected_annotations, (
            f"어노테이션이 last-applied 외에도 달라졌다: "
            f"{metadata.get('annotations')} != {expected_annotations}"
        )
    assert set(metadata) == expected_keys, (
        f"metadata 키 집합이 「원본 − 잡음」과 다르다: "
        f"{sorted(set(metadata) ^ expected_keys)}"
    )

    assert _normalized(obj["spec"]) == _normalized(whole["spec"]), (
        "spec 이 원본과 다르다 — 잡음 제거가 spec 을 건드렸다: "
        f"도구 {json.dumps(obj['spec'], sort_keys=True)[:400]} vs "
        f"원본 {json.dumps(whole['spec'], sort_keys=True)[:400]}"
    )
    assert obj.get("status"), f"status 가 비었다: {obj.get('status')}"

    print(
        f"strip ok: metadata keys {len(metadata)} (원본 {len(raw_metadata)}), "
        f"spec 동일, status 보존"
    )


async def test_resource_generic_ac4_name_is_required(session: ClientSession) -> None:
    """시나리오 4 — `name` 없이 셀렉터만 준 호출은 거부되고 `resource_list` 를 안내한다."""
    try:
        await session.call_tool(
            "resource_get",
            {
                "apiVersion": "apps/v1",
                "kind": "Deployment",
                "namespace": NAMESPACE,
                "labelSelector": f"app.kubernetes.io/name={WORKLOAD}",
            },
        )
    except McpError as exc:
        message = str(exc)
        assert "name is required" in message, message
        assert "resource_list" in message, (
            f"거부 메시지가 resource_list 로 먼저 찾으라고 안내하지 않는다: {message}"
        )
        print(f"name-required ok: {message}")
    else:
        raise AssertionError("expected McpError for resource_get without name")


async def run() -> None:
    url = base_url()
    ensure_workload_fixture_baseline()
    wait_for_healthz(url)

    print("--- resource-generic/시나리오 4 (사전 조건: 잡음이 붙어 있다) ---")
    whole = test_resource_generic_ac4_precondition_has_both_noise_fields()

    async with open_session(url) as session:
        print("--- resource-generic/시나리오 4 (잡음 둘만 제거) ---")
        await test_resource_generic_ac4_get_strips_only_the_noise(session, whole)
        print("--- resource-generic/시나리오 4 (이름 필수) ---")
        await test_resource_generic_ac4_name_is_required(session)
        print("ok: test-resource-generic.md#시나리오 4")


if __name__ == "__main__":
    asyncio.run(run())
