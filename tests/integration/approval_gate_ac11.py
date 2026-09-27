"""게이트의 읽기가 선언되고 값에 닿지 않는다 — 사전 읽기는 메타데이터뿐이다.

검증 시나리오: test-approval-gate.md#시나리오 11
실행 대상: primary
병렬 레인: gate-apiserver-audit

**이 파일은 러너가 건네는 배포를 쓰지 않는다.** 시나리오가 재는 것은 게이트가 승인 전에
apiserver 로 *어떤 요청을 보냈는지*이고, 그것이 왜 기록 프록시로만 관측되는지는
`tests/k8s/kind/apiserver-audit-variant.yaml` 머리말이 든다. 이 파일은 그 변형을 따로 띄우고
자기 포트포워드로 그 배포와 그 프록시의 admin API 에 붙는다
(`approval_gate_ac1.py`·`resource_generic_ac18.py` 가 각자의 변형에 붙는 것과 같은 형태).

**부재만 세면 공전한다.** "전체 객체 `get` 이 한 번도 없다"는 기록 경로가 죽어 있어도 참이다.
그래서 같은 창에서 **비민감 종류의 게이트 읽기**(스케일의 Deployment)를 함께 태운다 — 그것은
정의상 전체 객체 읽기이므로, 그 기록이 보이는 것이 곧 "이 프록시는 전체 객체 읽기를 잡는다"의
양성 대조다. (b) 의 `get` 증거와 그 대조가 같은 기록 하나다.

**난수 토큰이 새지 않는지는 세 표면에서 센다** — 프록시 기록 전체, SUT 컨테이너 로그, 그리고
거부된 호출의 응답. 프록시는 설계상 본문을 기록하지 않으므로(식별자만) 기록 쪽 0 은 약한
증거다. 강한 증거는 ``Accept`` 헤더 쪽이고, 토큰 grep 은 그 위에 얹는 교차 확인이다.
값 가림 자체는 이 시나리오가 아니라 시나리오 10 이 잰다.

**(c) 의 대조는 무엇인가.** `prd-approval-gate.md` AC11 은 `prd-resource-generic`
AC19 의 RBAC 대조가 결번이 된 뒤에도 **선언은 남긴다**고 적으므로, 여기서 성립하는 대조는
① 선언이 AC11 표의 두 쌍과 정확히 같다 ② 그 두 쌍이 `k8s/rbac.yaml` 이 바인딩하는 ClusterRole
아래 덮인다 — 둘의 논리곱이다. `cluster-admin` 은 ②를 항상 만족시키므로 ② 하나로는 공전하고,
①이 "선언을 뺀 변형"을 잡는 자리다. 음성 대조는 파싱된 선언과 `rbac.yaml` 사본을 각각
훼손해 같은 비교 함수에 먹여 rc 가 뒤집히는지로 센다.
"""

from __future__ import annotations

import asyncio
import json
import re
import subprocess
import time
from pathlib import Path

import httpx
from mcp.shared.exceptions import McpError

from _auth_variant import API_KEY
from _gatekeeper import _ephemeral_port, decide, gatekeeper_url, wait_for_pending
from _helpers import base_url, open_session, port_forward, wait_for_healthz
from _workload import NAMESPACE

REPO_ROOT = Path(__file__).resolve().parents[2]

VARIANT_NAMESPACE = "homelab-k3s-mcp-apiserver-audit"
VARIANT_DEPLOYMENT = "homelab-k3s-mcp-apiserver-audit"
VARIANT_SERVICE = VARIANT_DEPLOYMENT

PREFIX = "gk-ac11"
SECRET = f"{PREFIX}-needle"
DEPLOYMENT = f"{PREFIX}-scale"
COLLECTION_LABEL = f"{PREFIX}-collection"
COLLECTION = (f"{PREFIX}-doomed-a", f"{PREFIX}-doomed-b")

#: 레포 어디에도 없는 바늘. 세 표면의 grep 이 곧 판정이다.
TOKEN = "gk-ac11-needle-6b3d90f4c2ae"

#: 픽스처의 `replicas` 와 달라야 "움직이지 않았다"를 잴 수 있다.
SCALE_TARGET_REPLICAS = 2
FIXTURE_REPLICAS = 0

#: 판정까지의 상한. 이 배포의 `GATEKEEPER_TIMEOUT_SECONDS` 는 기본 300 초이고, 이 파일은
#: 만료를 기다리지 않고 거부를 **내린다** — 시나리오 11 (a) 가 요구하는 것이 거절이다.
DECISION_BUDGET = 60.0

METADATA_MARK = "as=PartialObjectMetadata;"
METADATA_LIST_MARK = "as=PartialObjectMetadataList;"

#: `main.go` 가 기동 시 찍는 AC11 선언 줄.
DECLARATION_LOG_MARK = "approval gate exercises these pairs before any verdict"
DECLARATION_RE = re.compile(r'pairs="?([^"\n]+?)"?\s*$')

PRD_PATH = "docs/prd-approval-gate.md"
PRD_PAIR_RE = re.compile(r"^\s*\|\s*`(get|list)` on ⟨kind⟩\s*\|")

RBAC_PATH = "k8s/rbac.yaml"

#: 이름으로 덮음을 판정하는 좁은 규약이다 — 임의의 ClusterRole 을 해석하지 않는다
#: (그 해석은 apiserver 의 몫이다).
COVER_ALL_CLUSTER_ROLES = {"cluster-admin"}


def _kubectl(*args: str, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["kubectl", *args], capture_output=True, text=True, check=check
    )


def _apply(manifest: dict) -> None:
    subprocess.run(
        ["kubectl", "apply", "-f", "-"],
        input=json.dumps(manifest),
        capture_output=True,
        text=True,
        check=True,
    )


def _in_namespace(*args: str, check: bool = True) -> subprocess.CompletedProcess:
    return _kubectl("-n", NAMESPACE, *args, check=check)


def _config_map(name: str, labels: dict[str, str] | None = None) -> dict:
    meta: dict = {"name": name, "namespace": NAMESPACE}
    if labels:
        meta["labels"] = labels
    return {"apiVersion": "v1", "kind": "ConfigMap", "metadata": meta, "data": {"k": "v"}}


def _fixtures() -> None:
    _apply(
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {"name": SECRET, "namespace": NAMESPACE},
            "stringData": {"token": TOKEN},
        }
    )
    for name in COLLECTION:
        _apply(_config_map(name, {COLLECTION_LABEL: "yes"}))
    _apply(
        {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {"name": DEPLOYMENT, "namespace": NAMESPACE},
            "spec": {
                # 0 레플리카: 게이트가 읽을 객체만 있으면 되고, 파드를 띄우면 이미지
                # 당김이 이 파일의 전제가 된다.
                "replicas": FIXTURE_REPLICAS,
                "selector": {"matchLabels": {"app": DEPLOYMENT}},
                "template": {
                    "metadata": {"labels": {"app": DEPLOYMENT}},
                    "spec": {
                        "containers": [
                            {"name": "pause", "image": "registry.k8s.io/pause:3.9"}
                        ]
                    },
                },
            },
        }
    )


def _cleanup() -> None:
    _in_namespace("delete", "secret", SECRET, "--ignore-not-found", check=False)
    _in_namespace(
        "delete", "configmap", *COLLECTION, "--ignore-not-found", check=False
    )
    _in_namespace("delete", f"deploy/{DEPLOYMENT}", "--ignore-not-found", check=False)


def audit_records(url: str) -> list[dict]:
    response = httpx.get(f"{url}/requests", timeout=10.0)
    response.raise_for_status()
    return response.json()["requests"]


def _watermark(url: str) -> int:
    records = audit_records(url)
    return max((r["seq"] for r in records), default=0)


def _since(url: str, watermark: int) -> list[dict]:
    return [r for r in audit_records(url) if r["seq"] > watermark]


def _reads_of(records: list[dict], path: str) -> list[dict]:
    return [r for r in records if r["method"] == "GET" and r["path"] == path]


async def _rejected(session, gate: str, tool: str, args: dict, marker: str) -> str:
    """게이트 대상 호출을 띄우고, 승인 요청이 뜬 상태에서 **거절**한다."""
    task = asyncio.create_task(session.call_tool(tool, args))
    row = await wait_for_pending(gate, marker)
    await decide(gate, row["id"], "REJECTED")
    try:
        result = await asyncio.wait_for(task, timeout=DECISION_BUDGET)
    except asyncio.TimeoutError as exc:
        raise AssertionError(
            f"{tool}: 거절된 호출이 {DECISION_BUDGET:.0f}초 안에 돌아오지 않았다"
        ) from exc
    except McpError as exc:
        return str(exc)
    assert getattr(result, "isError", False), (
        f"{tool}: 거절당한 호출이 성공으로 돌아왔다: {result}"
    )
    return str(result)


def _server_logs() -> str:
    return _kubectl(
        "-n", VARIANT_NAMESPACE, "logs", f"deploy/{VARIANT_DEPLOYMENT}",
        "-c", "server", "--tail=-1",
    ).stdout


def _secret_path() -> str:
    return f"/api/v1/namespaces/{NAMESPACE}/secrets/{SECRET}"


def _deployment_path() -> str:
    return f"/apis/apps/v1/namespaces/{NAMESPACE}/deployments/{DEPLOYMENT}"


def _configmap_collection_path() -> str:
    return f"/api/v1/namespaces/{NAMESPACE}/configmaps"


async def test_a_rejected_sensitive_read_never_fetched_the_whole_object(
    session, gate: str, audit: str
) -> dict:
    """(a) 거절된 Secret 승인은 메타데이터만 읽었고 값에 닿지 않았다."""
    mark = _watermark(audit)
    refusal_text = await _rejected(
        session, gate, "resource_get",
        {"apiVersion": "v1", "kind": "Secret", "namespace": NAMESPACE, "name": SECRET},
        SECRET,
    )
    records = _since(audit, mark)
    reads = _reads_of(records, _secret_path())
    assert reads, (
        f"프록시가 {_secret_path()} 의 GET 을 하나도 못 봤다 — 게이트가 사전 읽기를 "
        f"하지 않았거나 관측 경로가 이 배포를 지나지 않는다 (창 기록 {len(records)}건)"
    )
    whole = [r for r in reads if METADATA_MARK not in r["accept"]]
    assert not whole, (
        "게이트가 승인 전에 Secret 전체 객체를 읽었다 — 거절된 승인이 값을 읽은 것이다: "
        f"{whole}"
    )
    return {"records": records, "reads": reads, "refusal": refusal_text}


def test_a_the_token_is_nowhere(audit_window: dict) -> None:
    """(a) 난수 토큰이 세 표면 어디에도 없다."""
    dumped = json.dumps(audit_window["records"], ensure_ascii=False)
    assert TOKEN not in dumped, "프록시 기록에 토큰이 들어갔다"
    logs = _server_logs()
    assert TOKEN not in logs, "SUT 로그에 토큰이 들어갔다"
    refusal = audit_window["refusal"]
    assert refusal, "거부 문면을 못 잡았다 — 응답 표면의 토큰 부재가 공전한다"
    assert TOKEN not in refusal, "거부 응답에 토큰이 들어갔다"


async def test_b_scale_exercises_get_on_the_whole_object(
    session, gate: str, audit: str
) -> list[dict]:
    """(b) 스케일의 사전 읽기는 `get` 이다 — 그리고 이것이 기록 경로의 양성 대조다."""
    before = _in_namespace(
        "get", f"deploy/{DEPLOYMENT}", "-o", "jsonpath={.spec.replicas}"
    ).stdout.strip()
    assert before == str(FIXTURE_REPLICAS), before
    mark = _watermark(audit)
    await _rejected(
        session, gate, "resource_update",
        {
            "apiVersion": "apps/v1", "kind": "Deployment", "namespace": NAMESPACE,
            "name": DEPLOYMENT, "subresource": "scale",
            "replicas": SCALE_TARGET_REPLICAS,
        },
        DEPLOYMENT,
    )
    after = _in_namespace(
        "get", f"deploy/{DEPLOYMENT}", "-o", "jsonpath={.spec.replicas}"
    ).stdout.strip()
    assert after == before, f"스케일이 거절당했는데 {before} → {after} 로 움직였다"

    reads = _reads_of(_since(audit, mark), _deployment_path())
    assert reads, f"프록시가 {_deployment_path()} 의 GET 을 못 봤다"
    whole = [r for r in reads if METADATA_MARK not in r["accept"]]
    assert whole, (
        "비민감 종류의 사전 읽기가 전체 객체가 아니었다 — 이 파일의 양성 대조가 서지 "
        f"않으므로 (a) 의 「전체 객체 0」은 공전한다: {reads}"
    )
    return reads


async def test_b_delete_collection_exercises_list(
    session, gate: str, audit: str
) -> list[dict]:
    """(b) 일괄 삭제의 사전 읽기는 `list` 다 — 컬렉션 경로의 메타데이터 목록."""
    mark = _watermark(audit)
    await _rejected(
        session, gate, "resource_delete_collection",
        {
            "apiVersion": "v1", "kind": "ConfigMap", "namespace": NAMESPACE,
            "labelSelector": f"{COLLECTION_LABEL}=yes",
        },
        f"{COLLECTION_LABEL}=yes",
    )
    for name in COLLECTION:
        assert _in_namespace(
            "get", "configmap", name, "-o", "name", check=False
        ).returncode == 0, f"{name} 이 거절당한 일괄 삭제에 지워졌다"

    lists = [
        r
        for r in _reads_of(_since(audit, mark), _configmap_collection_path())
        if f"{COLLECTION_LABEL}%3Dyes" in r["query"]
        or f"{COLLECTION_LABEL}=yes" in r["query"]
    ]
    assert lists, (
        f"프록시가 {_configmap_collection_path()} 의 셀렉터 목록 요청을 못 봤다"
    )
    assert all(METADATA_LIST_MARK in r["accept"] for r in lists), (
        f"일괄 삭제의 목록 읽기가 메타데이터 목록이 아니었다: {lists}"
    )
    return lists


def declared_pairs() -> list[str]:
    """기동 로그가 찍은 게이트 선언. 런타임이 스스로 말하는 값이다."""
    for line in _server_logs().splitlines():
        if DECLARATION_LOG_MARK not in line:
            continue
        match = DECLARATION_RE.search(line)
        assert match, f"선언 줄의 pairs 를 파싱하지 못했다: {line}"
        return [part.strip() for part in match.group(1).split(",") if part.strip()]
    raise AssertionError(
        f"기동 로그에 {DECLARATION_LOG_MARK!r} 줄이 없다 — AC11 선언이 찍히지 않았다"
    )


def contract_pairs(prd_text: str) -> list[str]:
    """`prd-approval-gate.md` AC11 표가 드는 두 쌍."""
    return [
        f"{m.group(1)} on ⟨kind⟩"
        for m in (PRD_PAIR_RE.match(line) for line in prd_text.splitlines())
        if m
    ]


def bound_cluster_role(rbac_text: str) -> str:
    """`k8s/rbac.yaml` 이 SUT 의 SA 에 바인딩하는 ClusterRole 이름."""
    match = re.search(
        r"roleRef:.*?kind:\s*ClusterRole.*?name:\s*(\S+)", rbac_text, re.S
    )
    assert match, "rbac.yaml 에서 roleRef 의 ClusterRole 이름을 찾지 못했다"
    return match.group(1)


def compare_declaration(declared: list[str], prd_text: str, rbac_text: str) -> list[str]:
    """선언 ↔ 계약 문면 ↔ 부여의 대조. 불일치 사유를 모아 돌려준다(빈 목록 = 통과)."""
    problems: list[str] = []
    contract = contract_pairs(prd_text)
    if not contract:
        problems.append(f"{PRD_PATH} AC11 표에서 쌍을 하나도 읽지 못했다")
    if sorted(declared) != sorted(contract):
        problems.append(
            f"선언 {declared} 과 계약 문면 {contract} 가 다르다"
        )
    role = bound_cluster_role(rbac_text)
    if role not in COVER_ALL_CLUSTER_ROLES:
        problems.append(
            f"{RBAC_PATH} 가 바인딩하는 ClusterRole {role!r} 이 선언된 쌍을 덮는다고 "
            "이 파일이 판정할 수 있는 이름이 아니다"
        )
    return problems


def test_c_the_declaration_matches_the_contract_and_the_grant() -> None:
    """(c) 정적 대조: 선언 = AC11 표, 그리고 그 쌍이 `rbac.yaml` 의 부여 아래 덮인다."""
    prd_text = (REPO_ROOT / PRD_PATH).read_text(encoding="utf-8")
    rbac_text = (REPO_ROOT / RBAC_PATH).read_text(encoding="utf-8")
    declared = declared_pairs()
    problems = compare_declaration(declared, prd_text, rbac_text)
    assert not problems, f"대조가 실패했다: {problems}"

    # 음성 대조 ①: 선언을 뺀 변형.
    for dropped in range(len(declared)):
        variant = [p for i, p in enumerate(declared) if i != dropped]
        assert compare_declaration(variant, prd_text, rbac_text), (
            f"선언에서 {declared[dropped]!r} 을 뺀 변형이 대조를 통과했다 — 이 대조는 "
            "선언 누락을 잡지 못한다"
        )
    # 음성 대조 ②: 계약 문면에서 한 행을 지운 변형.
    for pair in declared:
        verb = pair.split(" ", 1)[0]
        mutated = "\n".join(
            line
            for line in prd_text.splitlines()
            if not PRD_PAIR_RE.match(line) or f"`{verb}` on" not in line
        )
        assert compare_declaration(declared, mutated, rbac_text), (
            f"계약 문면에서 {verb} 행을 지운 변형이 대조를 통과했다"
        )
    # 음성 대조 ③: 부여를 덮지 않는 ClusterRole 로 바꾼 변형.
    mutated_rbac = rbac_text.replace("name: cluster-admin", "name: view")
    assert mutated_rbac != rbac_text, "rbac.yaml 사본 훼손이 아무것도 바꾸지 못했다"
    assert compare_declaration(declared, prd_text, mutated_rbac), (
        "부여를 `view` 로 바꾼 변형이 대조를 통과했다"
    )


async def run() -> None:
    wait_for_healthz(base_url())
    _fixtures()

    print("--- approval-gate/시나리오 11 (사전 읽기는 메타데이터뿐) ---")
    with gatekeeper_url() as gate, port_forward(
        VARIANT_NAMESPACE, VARIANT_SERVICE, 80, _ephemeral_port(), ready_path="/healthz"
    ) as url, port_forward(
        VARIANT_NAMESPACE, VARIANT_SERVICE, 8099, _ephemeral_port(),
        ready_path="/healthz",
    ) as audit:
        wait_for_healthz(url)
        try:
            async with open_session(
                url, headers={"Authorization": f"Bearer {API_KEY}"}
            ) as session:
                print("(a) get (kind=Secret) — 거절")
                window = await test_a_rejected_sensitive_read_never_fetched_the_whole_object(
                    session, gate, audit
                )
                print("(b) update/scale — get (양성 대조)")
                await test_b_scale_exercises_get_on_the_whole_object(session, gate, audit)
                print("(b) deletecollection — list")
                await test_b_delete_collection_exercises_list(session, gate, audit)

            print("(a) 토큰 부재 (기록 · 로그 · 응답)")
            test_a_the_token_is_nowhere(window)
            print("(c) 선언 ↔ 계약 문면 ↔ 부여 (음성 대조 포함)")
            test_c_the_declaration_matches_the_contract_and_the_grant()
        finally:
            _cleanup()

    print("ok: test-approval-gate.md#시나리오 11")


if __name__ == "__main__":
    asyncio.run(run())
