"""게이트 대상만 막히고 나머지는 지나간다 — 승인 부재로 끝난 호출은 아무것도 실행하지 않는다.

검증 시나리오: test-approval-gate.md#시나리오 1
실행 대상: primary
병렬 레인: gate-refusal

세 갈래를 한 파일이 진다. (a) 쓰기 게이트 표의 모든 도구와 민감 종류의 읽기가 **판정 없이**
거부되고 그 도구의 실행 호출이 남긴 것이 0 이다. (b) 같은 조건에서 비게이트 호출은 승인 요청
없이 정상 수행된다. (c) 자기가 행사할 쌍을 선언하지 않은 도구를 등록한 기동 변형은 뜨지 않는다.

**이 파일은 러너가 건네는 배포를 쓰지 않는다.** (a) 는 「승인받지 못한 채
`GATEKEEPER_TIMEOUT_SECONDS` 가 지난다」를 열네 번 관측하는데, primary 의 기본 타임아웃은
300 초라 그 열넷이 한 시간을 넘는다. 타임아웃은 env 라 배포당이므로, 5 초짜리 배포를
`tests/k8s/kind/gate-refusal-variant.yaml` 에 따로 세우고 이 파일이 자기 포트포워드로 붙는다
(`resource_generic_ac18.py` 가 좁은 RBAC 변형에 붙는 것과 같은 형태). 그 배포의 게이트
시크릿에는 `GATEKEEPER_USER_ID` 가 없다 — 담당자가 없으면 gatekeeper 의 자동 응답 모드가
걸리지 않아 아무도 판정하지 않는다. 시나리오의 「자동 응답을 끄고 판정하지 않는다」가 이
구성이다.

**「실행 호출 0」을 무엇으로 재는가.** e2e 에는 단위 층의 가짜 k8s 서비스 카운터가 없으므로,
도구마다 *성공했다면 남았을 흔적*을 골라 그것이 없음을 센다 — 객체 쪽 도구는 대상의
`resourceVersion`(또는 존재 여부), `resource_exec` 는 명령이 남겼을 파일,
`resource_port_forward`·`resource_proxy` 는 대상 파드의 CGI 적중 기록,
`resource_attach` 는 stdin 을 받아 적는 컨테이너의 기록이다. **부재만 세면 공전한다** —
기록 경로 자체가 죽어 있어도 0 이 나오기 때문이다. 그래서 각 기록마다 같은 파일이
`kubectl` 로 **양성 대조**를 한 줄 넣어 그 경로가 살아 있음을 먼저 보인다.

민감 읽기(`kind=Secret` 의 `get`·`watch`)는 흔적을 남기지 않는 호출이라 그 둘만은 거부와
만료 기록이 증거의 전부다. 값이 새지 않는지는 이 시나리오가 아니라 시나리오 10 이 잰다.

(c) 는 `tests/k8s/kind/gate-refusal-variant.yaml` 의 둘째 배포다. 등록 표와 광고면이 둘 다
컴파일 시점 상수라 env 나 매니페스트로는 그 변형을 만들 수 없어, `e2e_undeclared_tool` 빌드
태그로 세운 별도 이미지가 그 변형의 유일한 형태다(`internal/mcp/undeclared_tool_probe.go`).
관측은 `platform_auth_safety_ac8.py` 의 (d) 와 같다 — 파드가 Ready 가 되지 못하고
`lastState.terminated` 와 로그가 남는다. 같은 네임스페이스·같은 시크릿을 쓰는 (a) 의 배포가
정상이라는 것이 「죽은 이유가 태그다」의 대조군이다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess
import time

from mcp.shared.exceptions import McpError

from _auth_variant import API_KEY
from _gatekeeper import (
    _ephemeral_port,
    count_requests,
    gatekeeper_url,
    get_request,
    wait_for_pending,
)
from _helpers import base_url, open_session, port_forward, wait_for_healthz
from _workload import NAMESPACE

VARIANT_NAMESPACE = "homelab-k3s-mcp-gate-refusal"
VARIANT_DEPLOYMENT = "homelab-k3s-mcp-gate-refusal"
VARIANT_SERVICE = VARIANT_DEPLOYMENT
UNDECLARED_DEPLOYMENT = "homelab-k3s-mcp-undeclared-tool"
UNDECLARED_SELECTOR = f"app.kubernetes.io/name={UNDECLARED_DEPLOYMENT}"

#: 변형 배포의 `GATEKEEPER_TIMEOUT_SECONDS`(gate-refusal-variant.yaml)와, 거부가 그것을
#: 실제로 기다렸는지 가르는 하한. 최초 응답만 보고 끝낸 구현은 즉시 돌아온다.
VARIANT_TIMEOUT_SECONDS = 5
LATENCY_FLOOR = VARIANT_TIMEOUT_SECONDS - 0.5
REFUSAL_BUDGET = VARIANT_TIMEOUT_SECONDS + 10

#: 비게이트 호출의 상한. 게이트를 탔다면 승인이 없어 최소 `VARIANT_TIMEOUT_SECONDS` 는
#: 걸렸을 것이므로, 그 아래로 돌아온 성공은 「승인 요청 없이 수행됐다」와 같은 말이다.
#: 동시에 도는 다른 레인 때문에 gatekeeper 의 전역 요청 수 증분은 단언의 재료가 못 된다 —
#: 대신 이 파일의 접두사를 담은 요청 수를 센다.
UNGATED_CEILING = VARIANT_TIMEOUT_SECONDS - 1.0

PREFIX = "ag-ac1"
CM_CREATED = f"{PREFIX}-created"
CM_REPLACED = f"{PREFIX}-replaced"
CM_PATCHED = f"{PREFIX}-patched"
CM_DOOMED = f"{PREFIX}-doomed"
CM_CONTROL = f"{PREFIX}-control"
COLLECTION = (f"{PREFIX}-collection-one", f"{PREFIX}-collection-two")
COLLECTION_LABEL = f"{PREFIX}-collection"
COLLECTION_VALUE = "yes"
SECRET = f"{PREFIX}-secret"
SECRET_LABEL = f"{PREFIX}-secret-watch"
DEPLOYMENT = f"{PREFIX}-workload"
POD = f"{PREFIX}-target"
POD_PORT = 80
POD_READY_BUDGET = 180.0

RESTART_ANNOTATION = "kubectl.kubernetes.io/restartedAt"
RESTART_STAMP = f"{PREFIX}-restart-2026-09-26T00:00:00Z"
SCALE_TARGET_REPLICAS = 3
GRACE_PERIOD_SECONDS = 7

#: 파드 안의 기록 경로. `httpd` 컨테이너의 CGI 가 `HITS` 에 적고, `sink` 컨테이너가
#: 자기 stdin 을 `ATTACH_LOG` 에 적는다. 둘 다 없으면 「적중 0」이다.
HITS = "/www/hits.log"
ATTACH_LOG = "/out/attach.log"
CGI_PATH = "/cgi-bin/hit"

EXEC_MARKER = f"{PREFIX}-exec-ran"
EXEC_MARKER_PATH = f"/www/{EXEC_MARKER}"
EXEC_COMMAND = ["sh", "-c", f"echo ran > {EXEC_MARKER_PATH}"]
ATTACH_MARKER = f"{PREFIX}-attach-ran"
ATTACH_STDIN = f"{ATTACH_MARKER}\n"
ATTACH_READ_SECONDS = 5
FORWARD_MARKER = f"{PREFIX}-forward-ran"
FORWARD_PAYLOAD = f"GET {CGI_PATH}?{FORWARD_MARKER} HTTP/1.0\r\n\r\n"
PROXY_MARKER = f"{PREFIX}-proxy-ran"
PROXY_QUERY = f"{CGI_PATH}?{PROXY_MARKER}"

CONTROL_MARKERS = (
    f"{PREFIX}-exec-control",
    f"{PREFIX}-attach-control",
    f"{PREFIX}-hits-control",
)

#: (c) 변형의 로그에 있어야 하는 두 조각. 앞은 main 이 기동을 포기할 때 쓰는 말이고,
#: 뒤는 `validateDeclarations` 가 이 도구 하나에 대해 쓰는 말이다. 뒤를 함께 보는 것이
#: 「무엇이 막았는가」를 레지스트리 이름 집합 불일치와 가르는 유일한 자리다.
UNDECLARED_TOOL = "e2e_undeclared_probe"
STARTUP_REFUSAL = "refusing to start"
DECLARATION_REFUSAL = (
    f"{UNDECLARED_TOOL} declares no (verb, resource) pair"
    " and carries no no-resource-permission marker"
)


def _kubectl(*args: str, input: str | None = None, check: bool = True):
    return subprocess.run(
        ["kubectl", *args], capture_output=True, text=True, check=check,
        input=input, timeout=120,
    )


def _apply(manifest: dict) -> None:
    _kubectl("apply", "-f", "-", input=json.dumps(manifest))


def _in_namespace(*args: str, check: bool = True):
    return _kubectl("-n", NAMESPACE, *args, check=check)


def _config_map(name: str, labels: dict[str, str] | None = None) -> dict:
    metadata: dict = {"name": name, "namespace": NAMESPACE}
    if labels:
        metadata["labels"] = labels
    return {
        "apiVersion": "v1",
        "kind": "ConfigMap",
        "metadata": metadata,
        "data": {"key": name},
    }


def _resource_version(kind: str, name: str) -> str:
    out = _in_namespace(
        "get", kind, name, "-o", "jsonpath={.metadata.resourceVersion}"
    ).stdout.strip()
    assert out, f"{kind}/{name} 의 resourceVersion 을 읽지 못했다"
    return out


def _exists(kind: str, name: str) -> bool:
    return _in_namespace("get", kind, name, check=False).returncode == 0


def _pod_file(container: str, path: str) -> str:
    """파드 안 파일의 내용. 없으면 빈 문자열 — 「아직 아무것도 적히지 않았다」와 같다."""
    proc = _in_namespace(
        "exec", POD, "-c", container, "--", "sh", "-c", f"cat {path} 2>/dev/null || true",
        check=False,
    )
    return proc.stdout


def _write_in_pod(container: str, path: str, marker: str) -> None:
    _in_namespace(
        "exec", POD, "-c", container, "--", "sh", "-c",
        f"mkdir -p $(dirname {path}) && echo {marker} >> {path}",
    )


def _fixtures() -> None:
    """이 파일이 가리키는 객체를 세운다.

    `CM_CREATED` 만은 세우지 않는다 — create 호출이 만들려다 거부당하는 이름이라, 미리
    있으면 「만들어지지 않았다」를 이름의 존재로 잴 수 없다.
    """
    _in_namespace("delete", "configmap", CM_CREATED, "--ignore-not-found", "--wait=true")
    for name in (CM_REPLACED, CM_PATCHED, CM_DOOMED, CM_CONTROL):
        _apply(_config_map(name))
    for name in COLLECTION:
        _apply(_config_map(name, {COLLECTION_LABEL: COLLECTION_VALUE}))
    _apply(
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {
                "name": SECRET,
                "namespace": NAMESPACE,
                "labels": {SECRET_LABEL: "yes"},
            },
            "stringData": {"token": f"{PREFIX}-secret-value"},
        }
    )
    _apply(
        {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {"name": DEPLOYMENT, "namespace": NAMESPACE},
            "spec": {
                "replicas": 1,
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

    # CGI 한 줄이 이 파일의 관측면이다: `httpd` 로 들어온 요청은 질의 문자열을 HITS 에
    # 남긴다. 정적 파일만 서빙하면 프록시·포트포워드가 **성공해도** 흔적이 없어 「적중 0」이
    # 공전한다.
    cgi = (
        "mkdir -p /www/cgi-bin && "
        "printf '#!/bin/sh\\n"
        "printf \"Content-Type: text/plain\\\\r\\\\n\\\\r\\\\nok\\\\n\"\\n"
        "echo \"$QUERY_STRING\" >> %s\\n' > /www/cgi-bin/hit && "
        "chmod +x /www/cgi-bin/hit && "
        "exec httpd -f -p %d -h /www"
    ) % (HITS, POD_PORT)
    _in_namespace("delete", "pod", POD, "--ignore-not-found", "--wait=true")
    _apply(
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "metadata": {"name": POD, "namespace": NAMESPACE, "labels": {"app": POD}},
            "spec": {
                "restartPolicy": "Never",
                "containers": [
                    {
                        "name": "httpd",
                        "image": "busybox:1.36",
                        "command": ["sh", "-c", cgi],
                        "ports": [{"containerPort": POD_PORT}],
                    },
                    {
                        "name": "sink",
                        "image": "busybox:1.36",
                        # attach 가 흘려보낸 stdin 을 받아 적는 자리. 루프인 이유는 cat 이
                        # EOF 로 끝나도 컨테이너가 죽지 않아야 파드가 Ready 로 남기 때문이다.
                        "command": [
                            "sh", "-c",
                            f"mkdir -p $(dirname {ATTACH_LOG}); "
                            f"while true; do cat >> {ATTACH_LOG}; sleep 1; done",
                        ],
                        "stdin": True,
                        "stdinOnce": False,
                    },
                ],
            },
        }
    )
    deadline = time.monotonic() + POD_READY_BUDGET
    while time.monotonic() < deadline:
        ready = _in_namespace(
            "get", "pod", POD, "-o",
            "jsonpath={.status.containerStatuses[*].ready}", check=False,
        ).stdout.split()
        if ready and all(value == "true" for value in ready):
            return
        time.sleep(2)
    raise AssertionError(
        f"{POD} 가 {POD_READY_BUDGET:.0f}초 안에 Ready 가 되지 않았다: "
        f"{_in_namespace('describe', 'pod', POD, check=False).stdout[-800:]}"
    )


async def _refused(session, gate: str, tool: str, args: dict, marker: str) -> dict:
    """게이트 대상 호출을 띄우고, 판정 없이 만료로 거부되는 것을 관측한다.

    거부 문면은 단언하지 않는다 — 클라이언트의 폴링 마감과 gatekeeper 의 만료가 같은
    `GATEKEEPER_TIMEOUT_SECONDS` 에서 파생해 어느 쪽이 먼저 닫는지가 경주이기 때문이다
    (`approval_gate_ac4.py` (b) 와 같은 이유). 계약은 셋이 진다: 승인 요청이 실제로
    만들어졌다 · 거부가 타임아웃을 기다린 뒤에 왔다 · 그 기록이 EXPIRED 다.
    """
    started = time.monotonic()
    task = asyncio.create_task(session.call_tool(tool, args))
    row = await wait_for_pending(gate, marker)
    try:
        result = await asyncio.wait_for(task, timeout=REFUSAL_BUDGET)
    except asyncio.TimeoutError as exc:
        raise AssertionError(
            f"{tool}: 판정 없는 호출이 {REFUSAL_BUDGET}초 안에도 돌아오지 않았다"
        ) from exc
    except McpError:
        pass
    else:
        assert getattr(result, "isError", False), (
            f"{tool}: 승인 없이 호출이 성공으로 돌아왔다: {result}"
        )
    wall = time.monotonic() - started
    assert wall >= LATENCY_FLOOR, (
        f"{tool}: 거부가 {wall:.1f}초에 왔다 — 타임아웃 {VARIANT_TIMEOUT_SECONDS}초를 "
        "기다리지 않았다"
    )
    record = get_request(gate, row["id"])
    assert record["status"] == "EXPIRED", (
        f"{tool}: 판정 없는 요청의 기록이 {record['status']} 다"
    )
    return record


async def _ungated(session, tool: str, args: dict):
    """비게이트 호출: 성공하고, 승인 대기를 탔다면 불가능한 시간 안에 돌아온다."""
    started = time.monotonic()
    result = await session.call_tool(tool, args)
    wall = time.monotonic() - started
    assert result.isError is False, f"{tool}: 비게이트 호출이 실패했다: {result}"
    assert wall < UNGATED_CEILING, (
        f"{tool}: {wall:.1f}초 걸렸다 — 승인 대기를 탔다는 뜻이다 "
        f"(이 배포의 타임아웃은 {VARIANT_TIMEOUT_SECONDS}초)"
    )
    return result


async def test_create_is_refused_and_creates_nothing(session, gate: str) -> None:
    await _refused(
        session, gate, "resource_create", {"manifest": _config_map(CM_CREATED)}, CM_CREATED
    )
    assert not _exists("configmap", CM_CREATED), (
        f"{CM_CREATED} 가 승인 없이 만들어졌다"
    )


async def test_update_is_refused_and_replaces_nothing(session, gate: str) -> None:
    before = _resource_version("configmap", CM_REPLACED)
    manifest = _config_map(CM_REPLACED)
    manifest["data"] = {"key": "must-not-apply"}
    await _refused(
        session, gate, "resource_update",
        {
            "apiVersion": "v1", "kind": "ConfigMap", "namespace": NAMESPACE,
            "name": CM_REPLACED, "manifest": manifest,
        },
        CM_REPLACED,
    )
    assert _resource_version("configmap", CM_REPLACED) == before


async def test_scale_is_refused_and_moves_no_replica(session, gate: str) -> None:
    before = _in_namespace(
        "get", f"deploy/{DEPLOYMENT}", "-o", "jsonpath={.spec.replicas}"
    ).stdout.strip()
    assert before.isdigit() and before != str(SCALE_TARGET_REPLICAS), before
    await _refused(
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
    assert after == before, f"스케일이 승인 없이 {before} → {after} 로 움직였다"


async def test_patch_is_refused_and_changes_nothing(session, gate: str) -> None:
    before = _resource_version("configmap", CM_PATCHED)
    await _refused(
        session, gate, "resource_patch",
        {
            "apiVersion": "v1", "kind": "ConfigMap", "namespace": NAMESPACE,
            "name": CM_PATCHED, "patchType": "merge",
            "patch": {"data": {"key": "must-not-apply"}},
        },
        CM_PATCHED,
    )
    assert _resource_version("configmap", CM_PATCHED) == before


async def test_restart_patch_is_refused_and_restarts_nothing(session, gate: str) -> None:
    await _refused(
        session, gate, "resource_patch",
        {
            "apiVersion": "apps/v1", "kind": "Deployment", "namespace": NAMESPACE,
            "name": DEPLOYMENT, "patchType": "merge",
            "patch": {
                "spec": {
                    "template": {
                        "metadata": {"annotations": {RESTART_ANNOTATION: RESTART_STAMP}}
                    }
                }
            },
        },
        RESTART_STAMP,
    )
    annotations = _in_namespace(
        "get", f"deploy/{DEPLOYMENT}", "-o",
        "jsonpath={.spec.template.metadata.annotations}",
    ).stdout
    assert RESTART_STAMP not in annotations, (
        f"재시작 어노테이션이 승인 없이 붙었다: {annotations}"
    )


async def test_delete_is_refused_and_removes_nothing(session, gate: str) -> None:
    await _refused(
        session, gate, "resource_delete",
        {
            "apiVersion": "v1", "kind": "ConfigMap", "namespace": NAMESPACE,
            "name": CM_DOOMED, "gracePeriodSeconds": GRACE_PERIOD_SECONDS,
        },
        CM_DOOMED,
    )
    assert _exists("configmap", CM_DOOMED), f"{CM_DOOMED} 가 승인 없이 지워졌다"


async def test_delete_collection_is_refused_and_removes_nothing(session, gate: str) -> None:
    await _refused(
        session, gate, "resource_delete_collection",
        {
            "apiVersion": "v1", "kind": "ConfigMap", "namespace": NAMESPACE,
            "labelSelector": f"{COLLECTION_LABEL}={COLLECTION_VALUE}",
        },
        f"{COLLECTION_LABEL}={COLLECTION_VALUE}",
    )
    for name in COLLECTION:
        assert _exists("configmap", name), f"{name} 이 승인 없이 지워졌다"


async def test_exec_is_refused_and_runs_nothing(session, gate: str) -> None:
    await _refused(
        session, gate, "resource_exec",
        {
            "apiVersion": "v1", "kind": "Pod", "namespace": NAMESPACE, "name": POD,
            "container": "httpd", "command": EXEC_COMMAND,
        },
        EXEC_MARKER,
    )
    assert _pod_file("httpd", EXEC_MARKER_PATH) == "", (
        "exec 명령이 승인 없이 파드 안에서 돌았다"
    )


async def test_attach_is_refused_and_writes_no_stdin(session, gate: str) -> None:
    await _refused(
        session, gate, "resource_attach",
        {
            "apiVersion": "v1", "kind": "Pod", "namespace": NAMESPACE, "name": POD,
            "container": "sink", "stdin": ATTACH_STDIN,
            "readSeconds": ATTACH_READ_SECONDS,
        },
        ATTACH_MARKER,
    )
    assert ATTACH_MARKER not in _pod_file("sink", ATTACH_LOG), (
        "attach 의 stdin 이 승인 없이 컨테이너에 닿았다"
    )


async def test_port_forward_is_refused_and_opens_no_tunnel(session, gate: str) -> None:
    await _refused(
        session, gate, "resource_port_forward",
        {
            "apiVersion": "v1", "kind": "Pod", "namespace": NAMESPACE, "name": POD,
            "port": POD_PORT, "payload": FORWARD_PAYLOAD,
            "readSeconds": ATTACH_READ_SECONDS,
        },
        FORWARD_MARKER,
    )
    assert FORWARD_MARKER not in _pod_file("httpd", HITS), (
        "포트포워드 페이로드가 승인 없이 파드에 닿았다"
    )


async def test_proxy_get_is_refused_and_reaches_nothing(session, gate: str) -> None:
    """프록시는 `GET` 도 막힌다 — 메서드가 아니라 경로가 호출을 한정하기 때문이다(AC15)."""
    await _refused(
        session, gate, "resource_proxy",
        {
            "apiVersion": "v1", "kind": "Pod", "namespace": NAMESPACE, "name": POD,
            "method": "GET", "path": PROXY_QUERY,
        },
        PROXY_MARKER,
    )
    assert PROXY_MARKER not in _pod_file("httpd", HITS), (
        "프록시 GET 이 승인 없이 파드에 닿았다"
    )


async def test_sensitive_get_is_refused(session, gate: str) -> None:
    await _refused(
        session, gate, "resource_get",
        {
            "apiVersion": "v1", "kind": "Secret", "namespace": NAMESPACE, "name": SECRET,
        },
        SECRET,
    )


async def test_sensitive_watch_is_refused(session, gate: str) -> None:
    await _refused(
        session, gate, "resource_watch",
        {
            "apiVersion": "v1", "kind": "Secret", "namespace": NAMESPACE,
            "labelSelector": f"{SECRET_LABEL}=yes", "watchSeconds": 5,
        },
        f"{SECRET_LABEL}=yes",
    )


async def test_ungated_calls_go_through_without_an_approval(session, gate: str) -> None:
    """(b) 같은 승인 불가 조건에서 비게이트 호출은 승인 요청 없이 정상 수행된다."""
    listed = await _ungated(
        session, "resource_list",
        {"apiVersion": "v1", "kind": "Secret", "namespace": NAMESPACE},
    )
    assert SECRET in listed.content[0].text, (
        f"Secret 목록에 대상이 없다: {listed.content[0].text[:400]}"
    )
    await _ungated(
        session, "resource_get",
        {
            "apiVersion": "v1", "kind": "ConfigMap", "namespace": NAMESPACE,
            "name": CM_CONTROL,
        },
    )
    await _ungated(
        session, "resource_watch",
        {
            "apiVersion": "v1", "kind": "ConfigMap", "namespace": NAMESPACE,
            "labelSelector": f"{COLLECTION_LABEL}={COLLECTION_VALUE}", "watchSeconds": 2,
        },
    )
    await _ungated(session, "api_resources", {})
    assert count_requests(gate, CM_CONTROL) == 0, (
        f"비게이트 대조 객체 {CM_CONTROL} 에 대한 승인 요청이 만들어졌다"
    )


def test_the_hit_records_would_have_caught_an_execution() -> None:
    """양성 대조 — 위의 「흔적 0」 셋이 공전이 아님을 같은 경로로 보인다.

    기록 경로가 죽어 있어도 부재 단언은 전부 초록이므로, 여기서 세 자리에 직접 한 줄씩
    적어 읽힌다는 것을 보인다. `kubectl exec` 는 SUT 를 지나지 않으므로 이 주입은 게이트와
    무관하다 — 재는 것은 「적히면 보인다」 하나다.
    """
    exec_control, attach_control, hits_control = CONTROL_MARKERS
    _write_in_pod("httpd", EXEC_MARKER_PATH, exec_control)
    _write_in_pod("sink", ATTACH_LOG, attach_control)
    _write_in_pod("httpd", HITS, hits_control)
    assert exec_control in _pod_file("httpd", EXEC_MARKER_PATH), (
        f"{EXEC_MARKER_PATH} 에 적은 것이 읽히지 않는다 — exec 의 부재 단언은 공전이었다"
    )
    assert attach_control in _pod_file("sink", ATTACH_LOG), (
        f"{ATTACH_LOG} 에 적은 것이 읽히지 않는다 — attach 의 부재 단언은 공전이었다"
    )
    assert hits_control in _pod_file("httpd", HITS), (
        f"{HITS} 에 적은 것이 읽히지 않는다 — 프록시·포트포워드의 부재 단언은 공전이었다"
    )


def test_this_files_requests_were_seen_by_the_gatekeeper(gate: str) -> None:
    """양성 대조 — (b) 의 「승인 요청 0」이 세는 법 자체의 공백이 아님을 보인다."""
    mine = count_requests(gate, PREFIX)
    assert mine >= 1, (
        f"마커 {PREFIX!r} 를 담은 승인 요청이 하나도 없다 — (b) 의 0 은 세는 법이 "
        "비어 있다는 뜻이지 요청이 없었다는 뜻이 아니다"
    )


def _undeclared_pod_status(timeout: float = 180.0) -> dict:
    """(c) 변형 컨테이너가 **한 번이라도 죽은** 뒤의 상태."""
    deadline = time.monotonic() + timeout
    last_seen: dict = {}
    while time.monotonic() < deadline:
        proc = _kubectl(
            "-n", VARIANT_NAMESPACE, "get", "pods", "-l", UNDECLARED_SELECTOR,
            "-o", "json", check=False,
        )
        if proc.returncode == 0:
            for pod in json.loads(proc.stdout).get("items", []):
                for status in pod.get("status", {}).get("containerStatuses", []) or []:
                    last_seen = status
                    terminated = status.get("lastState", {}).get("terminated") or (
                        status.get("state", {}).get("terminated")
                    )
                    if terminated:
                        return {"terminated": terminated, "status": status}
        time.sleep(2)
    raise AssertionError(
        f"deploy/{UNDECLARED_DEPLOYMENT} 가 {timeout:.0f}초 안에 종료 기록을 남기지 "
        f"않았다 (마지막 컨테이너 상태: {last_seen})"
    )


def _undeclared_logs() -> str:
    for extra in ([], ["--previous"]):
        proc = _kubectl(
            "-n", VARIANT_NAMESPACE, "logs", "-l", UNDECLARED_SELECTOR, "--tail=50",
            *extra, check=False,
        )
        if proc.returncode == 0 and proc.stdout.strip():
            return proc.stdout
    raise AssertionError(f"deploy/{UNDECLARED_DEPLOYMENT} 의 로그를 읽지 못했다")


def _available_replicas(deployment: str) -> int:
    raw = _kubectl(
        "-n", VARIANT_NAMESPACE, "get", f"deploy/{deployment}", "-o",
        "jsonpath={.status.availableReplicas}", check=False,
    ).stdout.strip()
    return int(raw) if raw.isdigit() else 0


def test_a_registry_with_an_undeclared_tool_refuses_to_start() -> None:
    """(c) 쌍을 선언하지 않은 도구를 등록한 기동 변형은 뜨지 않는다.

    같은 네임스페이스·같은 게이트 시크릿·같은 SA 를 쓰는 (a) 의 배포가 정상이라는 것이
    대조군이다 — 죽은 이유가 네임스페이스나 시크릿이 아니라 그 도구임을 그것이 가른다.
    """
    assert _available_replicas(VARIANT_DEPLOYMENT) >= 1, (
        f"대조군 deploy/{VARIANT_DEPLOYMENT} 가 서 있지 않다 — (c) 의 실패 귀속이 "
        "이 배포의 네임스페이스·시크릿과 갈리지 않는다"
    )
    observed = _undeclared_pod_status()
    assert observed["terminated"].get("exitCode") == 1, (
        f"(c) 변형이 exit 1 로 끝나지 않았다: {observed['terminated']}"
    )
    assert _available_replicas(UNDECLARED_DEPLOYMENT) == 0, (
        f"deploy/{UNDECLARED_DEPLOYMENT} 가 Available 이 됐다 — 선언 없는 도구를 "
        "등록한 레지스트리가 기동을 통과했다"
    )
    logs = _undeclared_logs()
    assert STARTUP_REFUSAL in logs, f"{STARTUP_REFUSAL!r} 가 로그에 없다:\n{logs[-1200:]}"
    assert DECLARATION_REFUSAL in logs, (
        f"기동은 실패했지만 그 이유가 선언 누락이 아니다:\n{logs[-1200:]}"
    )


def _cleanup() -> None:
    _in_namespace(
        "delete", "configmap", CM_REPLACED, CM_PATCHED, CM_DOOMED, CM_CONTROL,
        *COLLECTION, "--ignore-not-found", check=False,
    )
    _in_namespace("delete", "secret", SECRET, "--ignore-not-found", check=False)
    _in_namespace("delete", f"deploy/{DEPLOYMENT}", "--ignore-not-found", check=False)
    _in_namespace("delete", "pod", POD, "--ignore-not-found", "--wait=false", check=False)


async def run() -> None:
    wait_for_healthz(base_url())
    _fixtures()

    print("--- approval-gate/시나리오 1 (승인 부재 거부 · 실행 호출 0) ---")
    with gatekeeper_url() as gate, port_forward(
        VARIANT_NAMESPACE, VARIANT_SERVICE, 80, _ephemeral_port(), ready_path="/healthz"
    ) as url:
        wait_for_healthz(url)
        try:
            async with open_session(
                url, headers={"Authorization": f"Bearer {API_KEY}"}
            ) as session:
                print("(a) create")
                await test_create_is_refused_and_creates_nothing(session, gate)
                print("(a) update")
                await test_update_is_refused_and_replaces_nothing(session, gate)
                print("(a) update/scale")
                await test_scale_is_refused_and_moves_no_replica(session, gate)
                print("(a) patch")
                await test_patch_is_refused_and_changes_nothing(session, gate)
                print("(a) patch (재시작 어노테이션)")
                await test_restart_patch_is_refused_and_restarts_nothing(session, gate)
                print("(a) delete")
                await test_delete_is_refused_and_removes_nothing(session, gate)
                print("(a) deletecollection")
                await test_delete_collection_is_refused_and_removes_nothing(session, gate)
                print("(a) exec")
                await test_exec_is_refused_and_runs_nothing(session, gate)
                print("(a) attach")
                await test_attach_is_refused_and_writes_no_stdin(session, gate)
                print("(a) port_forward")
                await test_port_forward_is_refused_and_opens_no_tunnel(session, gate)
                print("(a) proxy (GET)")
                await test_proxy_get_is_refused_and_reaches_nothing(session, gate)
                print("(a) get (kind=Secret)")
                await test_sensitive_get_is_refused(session, gate)
                print("(a) watch (kind=Secret)")
                await test_sensitive_watch_is_refused(session, gate)
                print("(b) 비게이트 호출")
                await test_ungated_calls_go_through_without_an_approval(session, gate)

            print("양성 대조 (기록 경로 · 요청 계수)")
            test_the_hit_records_would_have_caught_an_execution()
            test_this_files_requests_were_seen_by_the_gatekeeper(gate)
        finally:
            _cleanup()

    print("(c) 미선언 도구 기동 변형")
    test_a_registry_with_an_undeclared_tool_refuses_to_start()
    print("ok: test-approval-gate.md#시나리오 1")


if __name__ == "__main__":
    asyncio.run(run())
