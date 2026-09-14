"""없는 종류는 후보와 함께 거부되고, 나중에 선 CRD 는 재기동 없이 곧바로 조회된다.

검증 시나리오: test-resource-generic.md#시나리오 20
실행 대상: primary

**매니페스트를 `tests/k8s/kind/` 에 파일로 두지 않고 아래 문자열로 들고 있다.** 그 디렉터리에
있으면 언젠가 ci.yml 에 배선되고, 그 순간 시나리오의 사전 조건인 「설치 전」이 소리 없이
사라지는데 케이스는 그대로 통과하고 게이트도 초록이다 — 위치 자체가 그 사고의 방지선이다.
(전용 프로브 CRD 를 런타임에 세우고 지우는 이유 자체는 시나리오 문서 「자동화」 절에 있다.)

**관측 순서가 곧 단언이다.** 오타 호출을 CRD 설치 **전에** 먼저 태우는 것은 후보 제시를 보기
위해서이기도 하지만 서버의 RESTMapper 캐시를 **데우기** 위해서이기도 하다. 그래야 설치 뒤의
`resource_list` 가 「캐시가 무효화됐다」를 재고, 데우지 않으면 「캐시를 처음 만들었다」를 재게
되어 재기동 불필요의 근거가 되지 못한다(`internal/k8s/resource.go` 의 `resolve` 는 NoMatch 에
mapper 를 정확히 한 번 무효화한다).

**「재기동 불필요」는 파드 정체로 잰다.** 설치 후 discovery 가 새 종류를 실어 나르기까지는
apiserver 쪽 전파 시간이 있어 폴링이 필요한데, 폴링만 두면 「서버가 재기동되어 다시 읽었다」와
구별되지 않는다. 그래서 처음과 끝에 서버 파드의 정체를 떠서 같은지 본다 — 폴링 창 전체를 덮는
단언이라 재기동이 끼어들 자리가 없다. 파드 uid 만으로는 부족해 컨테이너 재시작까지 함께 뜬다
(캐시는 프로세스 안에 있어 파드가 살아 있어도 컨테이너가 죽으면 비워진다).

**범위 밖**: 기대 결과의 마지막 절(`api_resources` 와 RBAC 대조)은 **강한 부정형으로 관측할 수
없다.** 그러려면 「discovery 에는 보이지만 이 서버의 부여 밖이라 목록을 못 받는 종류」가
필요한데, #83 이 `k8s/rbac.yaml` 을 `cluster-admin` 바인딩 하나로 바꾼 뒤로 그런 종류가
존재하지 않는다. 아래 케이스는 관측 가능한 최강형까지만 간다 — `api_resources` 가 **인스턴스가
하나도 없는 시점의 새 종류**를 discovery 메타데이터(`namespaced`·`verbs`)로 답한다는 것이다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess
import time

from mcp import ClientSession

from _helpers import base_url, open_session, wait_for_healthz

SERVER_NAMESPACE = "homelab-k3s-mcp"
SERVER_SELECTOR = "app.kubernetes.io/name=homelab-k3s-mcp,app.kubernetes.io/component=server"

#: 프로브 CR 을 둘 네임스페이스. `tests/k8s/kind/test-deployment.yaml` 이 세운다.
PROBE_NAMESPACE = "workload-test"

PROBE_GROUP = "probe.homelab-k3s-mcp.test"
PROBE_KIND = "KindResolutionProbe"
PROBE_PLURAL = "kindresolutionprobes"
PROBE_CRD = f"{PROBE_PLURAL}.{PROBE_GROUP}"
PROBE_API_VERSION = f"{PROBE_GROUP}/v1"
PROBE_OBJECT = "kind-resolution-probe"

TYPO_KIND = "Deploymnt"
TYPO_API_VERSION = "apps/v1"
EXPECTED_CANDIDATE = "apps/v1/Deployment"

#: apiserver 가 CRD 를 established 로 올린 뒤에도 discovery 문서에 실릴 때까지 짧은 전파
#: 지연이 있다. 재기동과 구별되지 않는 구간이므로 아래 파드 정체 단언이 함께 있어야 한다.
DISCOVERY_TIMEOUT = 60.0

PROBE_CRD_MANIFEST = f"""
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: {PROBE_CRD}
  labels:
    app.kubernetes.io/part-of: homelab-k3s-mcp-tests
spec:
  group: {PROBE_GROUP}
  scope: Namespaced
  names:
    kind: {PROBE_KIND}
    listKind: {PROBE_KIND}List
    plural: {PROBE_PLURAL}
    singular: kindresolutionprobe
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                note:
                  type: string
      # 프린터 컬럼을 하나 준다. 기본 컬럼(NAME/AGE)만 있으면 목록이 **이 종류의** Table 로
      # 온 것인지 아무 종류에나 붙는 최소 표로 온 것인지 구별되지 않는다.
      additionalPrinterColumns:
        - name: Note
          type: string
          jsonPath: .spec.note
"""

# CRD 와 그 CR 을 한 매니페스트에 두지 않는 이유는 `docs/test-resource-generic.md` 「픽스처」 절에 있다.
PROBE_OBJECT_MANIFEST = f"""
apiVersion: {PROBE_API_VERSION}
kind: {PROBE_KIND}
metadata:
  name: {PROBE_OBJECT}
  namespace: {PROBE_NAMESPACE}
  labels:
    app.kubernetes.io/part-of: homelab-k3s-mcp-tests
spec:
  note: installed-after-start-up
"""


def _kubectl(*args: str, stdin: str | None = None) -> str:
    return subprocess.run(
        ["kubectl", *args],
        input=stdin,
        capture_output=True,
        text=True,
        check=True,
    ).stdout


def remove_probe_crd() -> None:
    """프로브 CRD 를 지운다 — CR 은 함께 사라진다. 멱등하다."""
    _kubectl("delete", "crd", PROBE_CRD, "--ignore-not-found", "--wait=true")


def install_probe_crd() -> None:
    """CRD 를 세우고 established 를 기다린 뒤 CR 을 하나 둔다."""
    _kubectl("apply", "-f", "-", stdin=PROBE_CRD_MANIFEST)
    _kubectl(
        "wait", "--for=condition=Established", f"crd/{PROBE_CRD}", "--timeout=60s"
    )
    _kubectl("apply", "-f", "-", stdin=PROBE_OBJECT_MANIFEST)


def server_pod_identity() -> list[tuple]:
    """서버 파드의 정체 — (이름, uid, 컨테이너별 재시작 횟수·기동 시각)."""
    raw = _kubectl(
        "get", "pods", "-n", SERVER_NAMESPACE, "-l", SERVER_SELECTOR, "-o", "json"
    )
    pods = json.loads(raw)["items"]
    identity = []
    for pod in pods:
        containers = tuple(
            (
                status["name"],
                status["restartCount"],
                status.get("state", {}).get("running", {}).get("startedAt"),
            )
            for status in sorted(
                pod["status"].get("containerStatuses", []),
                key=lambda status: status["name"],
            )
        )
        identity.append((pod["metadata"]["name"], pod["metadata"]["uid"], containers))
    return sorted(identity)


async def api_resource_kinds(session: ClientSession) -> dict[str, dict]:
    """`api_resources` 의 답을 `group/version/kind` → 행 으로."""
    result = await session.call_tool("api_resources", {})
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload is not None, result
    items = payload["items"]
    assert items, f"api_resources 가 빈 목록을 돌려줬다: {payload}"
    return {f"{item['group']}/{item['version']}/{item['kind']}": item for item in items}


async def test_resource_generic_ac20_probe_kind_is_absent_before_install(
    session: ClientSession,
) -> list[tuple]:
    """시나리오 20 사전 조건 — 설치 「전」 상태를 실제로 세우고 관측한다.

    직전 실행이 남긴 것이 있으면 먼저 지운다. 그래야 이 파일이 멱등하고, 「없다」가
    「처음부터 없었다」가 아니라 **지금 없다**는 관측이 된다.
    """
    remove_probe_crd()
    before = server_pod_identity()
    assert before, f"{SERVER_NAMESPACE} 에 서버 파드가 없다 — 사전 조건 불성립"

    kinds = await api_resource_kinds(session)
    assert f"{PROBE_API_VERSION}/{PROBE_KIND}" not in kinds, (
        f"프로브 종류가 설치 전에 이미 있다 — 「전/후」가 성립하지 않는다: "
        f"{sorted(k for k in kinds if PROBE_GROUP in k)}"
    )
    # 대조군: 같은 답에 클러스터가 반드시 갖는 종류는 들어 있다. 이게 없으면 위의 「없다」는
    # api_resources 가 빈 답을 준 것과 구별되지 않는다.
    assert "apps/v1/Deployment" in kinds, (
        f"api_resources 에 apps/v1/Deployment 가 없다 — 답 자체가 온전하지 않다: "
        f"{len(kinds)} 종"
    )

    print(f"before ok: {len(kinds)} kinds, no {PROBE_KIND}; server pods {before}")
    return before


async def test_resource_generic_ac20_typo_is_refused_with_candidates(
    session: ClientSession,
) -> None:
    """시나리오 20 — 오타 호출이 거부되고 후보로 `Deployment` 가 제시된다.

    이 호출은 후보 제시를 보는 자리인 동시에 서버의 RESTMapper 캐시를 **데우는** 자리다
    (`resolve` 가 NoMatch 에 캐시를 한 번 무효화하고 다시 만든다). 설치 뒤의 목록 조회가
    「무효화됐다」를 재려면 그 전에 캐시가 차 있어야 한다.
    """
    result = await session.call_tool(
        "resource_list", {"apiVersion": TYPO_API_VERSION, "kind": TYPO_KIND}
    )
    assert result.isError is True, (
        f"없는 종류 {TYPO_KIND!r} 가 거부되지 않았다: {result}"
    )
    assert result.content, result
    message = result.content[0].text

    assert "did you mean " in message, (
        f"거부는 됐으나 후보가 하나도 제시되지 않았다: {message!r}"
    )
    listed = message.split("did you mean ", 1)[1].split("?", 1)[0]
    candidates = [item.strip() for item in listed.split(",")]

    assert EXPECTED_CANDIDATE in candidates, (
        f"후보에 {EXPECTED_CANDIDATE} 가 없다 — 시나리오가 지목한 오타가 실존 종류로 "
        f"이어지지 않는다: {candidates}"
    )
    # 가장 가까운 것이 먼저 와야 한다. 다섯 개로 잘리므로 순서가 무너지면 실제로 뜻한 종류가
    # 목록에서 밀려난다 — `Deploymnt` 와 `Deployment` 는 한 글자 차이라 단독 최근접이다.
    assert candidates[0] == EXPECTED_CANDIDATE, (
        f"최근접 후보가 {candidates[0]!r} 다 — {EXPECTED_CANDIDATE} 가 먼저여야 한다: "
        f"{candidates}"
    )

    print(f"typo ok: {TYPO_KIND} -> {candidates}")


async def test_resource_generic_ac20_new_crd_shows_up_in_discovery(
    session: ClientSession,
) -> None:
    """시나리오 20 — CRD 를 세우면 `api_resources` 에 나타난다."""
    install_probe_crd()

    deadline = time.monotonic() + DISCOVERY_TIMEOUT
    coordinate = f"{PROBE_API_VERSION}/{PROBE_KIND}"
    entry = None
    while time.monotonic() < deadline:
        kinds = await api_resource_kinds(session)
        entry = kinds.get(coordinate)
        if entry is not None:
            break
        time.sleep(1)
    assert entry is not None, (
        f"CRD 를 세운 뒤 {DISCOVERY_TIMEOUT:.0f}s 동안 {coordinate} 가 "
        f"api_resources 에 나타나지 않았다"
    )

    # 아래 목록 조회 전이라 이 종류의 객체는 도구 경로로 한 번도 읽히지 않았다.
    assert entry["namespaced"] is True, entry
    assert entry["name"] == PROBE_PLURAL, entry
    assert "list" in entry["verbs"], entry

    print(f"after ok: {coordinate} verbs={entry['verbs']}")


async def test_resource_generic_ac20_new_kind_is_listable_right_away(
    session: ClientSession,
) -> None:
    """시나리오 20 — 나타난 종류가 곧바로 목록으로 온다(데워진 캐시가 무효화됐다)."""
    result = await session.call_tool(
        "resource_list",
        {
            "apiVersion": PROBE_API_VERSION,
            "kind": PROBE_KIND,
            "namespace": PROBE_NAMESPACE,
        },
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload is not None, result

    columns = [column["name"] for column in payload["columns"]]
    rows = payload["rows"]
    assert rows, (
        f"새 종류의 목록이 비었다 — 조회는 됐으나 객체가 오지 않았다: {payload}"
    )
    assert "Name" in columns, f"apiserver 의 열이 아니다: {columns}"
    names = {row[columns.index("Name")] for row in rows}
    assert PROBE_OBJECT in names, f"프로브 객체가 목록에 없다: {sorted(names)}"

    # 이 종류가 **자기** Table 정의로 왔다는 것. 기본 컬럼만 오면 CRD 의 프린터 컬럼이
    # 이 경로에서 관측되지 않는다.
    assert "Note" in columns, f"CRD 의 프린터 컬럼이 표에 없다: {columns}"

    print(f"list ok: columns={columns} rows={len(rows)}")


async def test_resource_generic_ac20_server_never_restarted(
    session: ClientSession,
    before: list[tuple],
) -> None:
    """시나리오 20 — 위의 모든 관측 동안 서버 파드가 그대로였다.

    이 단언이 없으면 「재기동 불필요」는 문면에만 남는다 — discovery 전파를 기다리는 폴링은
    재기동 뒤 다시 읽은 경우에도 결국 통과하기 때문이다.
    """
    after = server_pod_identity()
    assert after == before, (
        f"서버 파드가 바뀌었다 — 재기동 없이 관측했다는 전제가 깨졌다: "
        f"{before} -> {after}"
    )
    print(f"no-restart ok: {after}")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        try:
            print("--- resource-generic/시나리오 20 (설치 전) ---")
            before = await test_resource_generic_ac20_probe_kind_is_absent_before_install(
                session
            )
            print("--- resource-generic/시나리오 20 (오타 거부와 후보) ---")
            await test_resource_generic_ac20_typo_is_refused_with_candidates(session)
            print("--- resource-generic/시나리오 20 (설치 후 discovery) ---")
            await test_resource_generic_ac20_new_crd_shows_up_in_discovery(session)
            print("--- resource-generic/시나리오 20 (새 종류 목록) ---")
            await test_resource_generic_ac20_new_kind_is_listable_right_away(session)
            print("--- resource-generic/시나리오 20 (재기동 없음) ---")
            await test_resource_generic_ac20_server_never_restarted(session, before)
        finally:
            # 프로브 종류는 클러스터 전역에 보이므로 뒤따르는 파일에 남기지 않는다.
            remove_probe_crd()
        print("ok: test-resource-generic.md#시나리오 20")


if __name__ == "__main__":
    asyncio.run(run())
