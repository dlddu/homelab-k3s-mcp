"""임의 종류를 좌표로 조회한다 — 일곱 종류, 셀렉터 축소, 이벤트 좁힘, 스코프 거부.

검증 시나리오: test-resource-generic.md#시나리오 1
실행 대상: primary

종류마다 **셀렉터를 붙인 목록과 붙이지 않은 목록을 같은 호출로 둘 다 떠서 비교**한다.
셀렉터 붙인 쪽만 보면 「줄었다」가 성립하지 않기 때문이다 — 셀렉터를 통째로 무시하는 구현도
그 단독 관측은 통과한다. 그래서 단정은 개수의 **엄격한 감소**이고, 픽스처가 대조군을 갖는
네 종류(Service·ConfigMap·Ingress·CRD)에서는 **어느 객체가 남고 어느 객체가 빠졌는지**까지
이름으로 본다.

`v1/Event` 만 레이블이 아니라 `fieldSelector` 로 좁힌다. 이벤트에는 대상별 레이블이 없고,
시나리오가 이벤트에 요구하는 것도 「대상별로 좁혀짐」이라 축이 다르다. 여기서 폐기된
`pod_describe` 의 이벤트 절이 이 경로로 대체됐다는 문면이 실제로 성립하는지가 갈린다 —
그래서 개수만 보지 않고 **남은 행이 전부 그 파드를 가리키는지**를 Object 칸으로 확인한다.

클러스터 스코프 + `namespace` 는 「무시되지 않고 거부」가 요점이라 **거부 사실만으로는
부족하다** — 같은 종류를 `namespace` 없이 부르면 성공한다는 것을 나란히 보여야, 거부가
스코프 때문이지 그 종류를 못 읽어서가 아님이 성립한다.

픽스처 CRD 는 서버가 뜬 **뒤에** 설치될 수 있다. 그래도 재기동이 필요 없는 것은 종류 해석이
미지의 종류에 대해 디스커버리를 정확히 한 번 다시 타기 때문이다(`internal/k8s/resource.go`
의 `resolve`). 이 파일은 그 경로에 기대지 않고 선행 조건을 kubectl 로 먼저 성립시킨다 —
도구가 못 찾는 것이 「아직 안 섰다」인지 「해석이 틀렸다」인지 구분되어야 해서다.
"""

from __future__ import annotations

import asyncio
import subprocess
import time

from mcp import ClientSession

from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE, WORKLOAD, ensure_workload_fixture_baseline

#: `tests/k8s/kind/resource-generic-fixture.yaml` 이 세우는 좁히기 레이블과 두 객체 이름.
SELECTED = "resource-generic-selected"
OTHER = "resource-generic-other"
SELECTOR = f"app.kubernetes.io/name={SELECTED}"

#: 같은 파일이 세우는 픽스처 CRD 좌표.
CRD_NAME = "resourcegenericsamples.homelab-k3s-mcp.test"
CRD_API_VERSION = "homelab-k3s-mcp.test/v1"
CRD_KIND = "ResourceGenericSample"

#: 픽스처 전체에 걸린 레이블. 클러스터 스코프 종류(Namespace)를 좁히는 데 쓴다.
TESTS_SELECTOR = "app.kubernetes.io/part-of=homelab-k3s-mcp-tests"

#: 시나리오가 「일곱 종류」로 세는 좌표와, 그 종류를 좁히는 셀렉터.
#: `v1/Event` 만 셀렉터가 None 이다 — 좁히는 축이 fieldSelector 라 별도 케이스로 간다.
KINDS = (
    ("v1", "Namespace", None, TESTS_SELECTOR),
    ("v1", "Service", NAMESPACE, SELECTOR),
    ("v1", "ConfigMap", NAMESPACE, SELECTOR),
    ("apps/v1", "Deployment", NAMESPACE, f"app.kubernetes.io/name={WORKLOAD}"),
    ("networking.k8s.io/v1", "Ingress", NAMESPACE, SELECTOR),
    ("v1", "Event", NAMESPACE, None),
    (CRD_API_VERSION, CRD_KIND, NAMESPACE, SELECTOR),
)

#: 픽스처가 대조군(`-other`)을 갖는 종류 — 이름 단위 단정까지 가능한 집합.
PAIRED_KINDS = ("Service", "ConfigMap", "Ingress", CRD_KIND)

#: 세는 목록이 잘리면 개수 비교가 무의미해진다. 도구 상한이 500 이므로 그 값으로 뜬다.
PAGE = 500


def wait_for_fixture(timeout: float = 120.0) -> None:
    """픽스처 CRD 와 그 인스턴스가 설 때까지 기다린다(멱등 폴링).

    ci.yml 이 CRD → established 대기 → 인스턴스 순으로 적용하므로, 마지막에 적용되는
    인스턴스가 보이면 이 파일이 쓰는 픽스처는 전부 서 있다. 도구가 아니라 kubectl 로
    재는 것은 이 전제를 검증 대상 자신으로 세우지 않기 위해서다.
    """
    deadline = time.monotonic() + timeout
    last = "<no probe yet>"
    while time.monotonic() < deadline:
        probe = subprocess.run(
            ["kubectl", "-n", NAMESPACE, "get", CRD_NAME, SELECTED, "-o", "name"],
            capture_output=True,
            text=True,
        )
        if probe.returncode == 0 and probe.stdout.strip():
            return
        last = probe.stderr.strip() or probe.stdout.strip()
        time.sleep(2)
    raise RuntimeError(
        f"픽스처 CRD 인스턴스가 {timeout:.0f}s 안에 서지 않았다 (마지막 관측: {last!r})"
    )


def fixture_pod() -> str:
    out = subprocess.check_output(
        [
            "kubectl", "-n", NAMESPACE, "get", "pod", "-l", f"app={WORKLOAD}",
            "-o", "jsonpath={.items[0].metadata.name}",
        ],
        text=True,
    ).strip()
    assert out, f"{NAMESPACE} 에 app={WORKLOAD} 파드가 없다"
    return out


async def _list(session: ClientSession, api_version: str, kind: str, **extra) -> dict:
    result = await session.call_tool(
        "resource_list",
        {"apiVersion": api_version, "kind": kind, "limit": PAGE, **extra},
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload is not None, result
    # 잘린 목록으로 개수를 비교하면 「줄었다」가 페이지 경계의 그림자일 수 있다.
    assert payload["truncated"] is False, payload["truncated"]
    return payload


def _names(payload: dict) -> list[str]:
    index = _column(payload, "Name")
    return [row[index] for row in payload["rows"]]


def _column(payload: dict, name: str) -> int:
    columns = [column["name"] for column in payload["columns"]]
    assert name in columns, f"{name} 칸이 없다: {columns}"
    return columns.index(name)


async def test_resource_generic_ac1_seven_kinds_answer_by_coordinate(
    session: ClientSession,
) -> None:
    """시나리오 1 — 일곱 종류가 전부 좌표만으로 목록을 돌려준다."""
    seen = []
    for api_version, kind, namespace, _ in KINDS:
        extra = {"namespace": namespace} if namespace else {}
        payload = await _list(session, api_version, kind, **extra)
        assert payload["columns"], f"{kind} 응답에 컬럼이 없다: {payload}"
        assert payload["rows"], f"{kind} 목록이 비었다 — 픽스처가 서지 않았다: {payload}"
        seen.append(f"{api_version}/{kind}={len(payload['rows'])}")
    assert len(seen) == 7, seen
    print("seven kinds ok:", ", ".join(seen))


async def test_resource_generic_ac1_label_selector_actually_narrows(
    session: ClientSession,
) -> None:
    """시나리오 1 — 셀렉터가 결과 수를 실제로 줄인다."""
    narrowed = []
    for api_version, kind, namespace, selector in KINDS:
        if selector is None:
            continue
        extra = {"namespace": namespace} if namespace else {}
        whole = _names(await _list(session, api_version, kind, **extra))
        subset = _names(
            await _list(session, api_version, kind, labelSelector=selector, **extra)
        )
        assert subset, f"{kind} 를 {selector} 로 좁혔더니 0건이다: {whole}"
        assert len(subset) < len(whole), (
            f"{kind} 가 {selector} 로 줄지 않았다 — 셀렉터가 무시됐을 수 있다: "
            f"{len(whole)} → {len(subset)}"
        )
        if kind in PAIRED_KINDS:
            assert SELECTED in subset and OTHER not in subset, (
                f"{kind} 가 레이블이 아니라 다른 기준으로 갈렸다: {subset}"
            )
            assert OTHER in whole, f"{kind} 대조군이 애초에 없다: {whole}"
        narrowed.append(f"{kind} {len(whole)}→{len(subset)}")
    assert len(narrowed) == 6, narrowed
    print("selector narrowing ok:", ", ".join(narrowed))


async def test_resource_generic_ac1_events_narrow_to_their_object(
    session: ClientSession, pod: str
) -> None:
    """시나리오 1 — 이벤트가 `fieldSelector` 로 대상별로 좁혀진다."""
    whole = await _list(session, "v1", "Event", namespace=NAMESPACE)
    subset = await _list(
        session,
        "v1",
        "Event",
        namespace=NAMESPACE,
        fieldSelector=f"involvedObject.name={pod}",
    )
    assert subset["rows"], f"{pod} 를 가리키는 이벤트가 없다"
    assert len(subset["rows"]) < len(whole["rows"]), (
        f"이벤트가 좁혀지지 않았다: {len(whole['rows'])} → {len(subset['rows'])}"
    )

    index = _column(subset, "Object")
    strays = [row[index] for row in subset["rows"] if not str(row[index]).endswith(pod)]
    assert not strays, f"좁힌 목록에 다른 대상의 이벤트가 있다: {strays}"
    print(
        f"event narrowing ok: {len(whole['rows'])} → {len(subset['rows'])} "
        f"(전부 {pod})"
    )


async def test_resource_generic_ac1_cluster_scoped_kind_rejects_namespace(
    session: ClientSession,
) -> None:
    """시나리오 1 — 클러스터 스코프 종류에 `namespace` 를 붙이면 거부된다."""
    refused = await session.call_tool(
        "resource_list",
        {"apiVersion": "v1", "kind": "Namespace", "namespace": NAMESPACE},
    )
    assert refused.isError, refused
    message = refused.content[0].text
    assert "cluster-scoped" in message, message

    # 거부가 스코프 때문임을 보이는 대조군: 같은 종류가 namespace 없이는 읽힌다.
    accepted = await _list(session, "v1", "Namespace")
    assert NAMESPACE in _names(accepted), _names(accepted)
    print(f"cluster-scope rejection ok: {message.strip()[:120]}")


async def run() -> None:
    url = base_url()
    ensure_workload_fixture_baseline()
    wait_for_fixture()
    pod = fixture_pod()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- resource-generic/시나리오 1 (일곱 종류) ---")
        await test_resource_generic_ac1_seven_kinds_answer_by_coordinate(session)
        print("--- resource-generic/시나리오 1 (셀렉터 축소) ---")
        await test_resource_generic_ac1_label_selector_actually_narrows(session)
        print("--- resource-generic/시나리오 1 (이벤트 좁힘) ---")
        await test_resource_generic_ac1_events_narrow_to_their_object(session, pod)
        print("--- resource-generic/시나리오 1 (클러스터 스코프 + namespace) ---")
        await test_resource_generic_ac1_cluster_scoped_kind_rejects_namespace(session)
        print("ok: test-resource-generic.md#시나리오 1")


if __name__ == "__main__":
    asyncio.run(run())
