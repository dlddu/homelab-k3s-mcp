"""승인 화면의 상세가 실물 게이트를 지나 운영자에게 도착한다.

검증 시나리오: test-approval-gate.md#시나리오 3
실행 대상: primary
병렬 레인: gate-screens

Go 단위가 이 계약의 문면을 이미 단언한다 — `internal/mcp/gate_test.go` 의
`TestApprovedCallReachesKubernetesWithAJudgeableContext` ·
`TestContextIncludesCurrentAndTargetReplicas` ·
`TestUnresolvableTargetIsRejectedBeforeRequest`, `internal/mcp/delete_collection_test.go` 의
`TestCollectionContextCarriesCountAndNames`. 그 전부가 **가짜 게이트**를 상대로 돈다 —
화면을 만드는 함수의 반환값을 그 자리에서 읽으므로, 만들어진 화면과 **gatekeeper 가 실제로
받아 사람에게 내건 화면**이 갈라져도 초록이다. 이 파일이 재는 것이 그 갈라짐이다: 여기서
읽는 `context` 는 kind 에 뜬 실물 gatekeeper 의 요청 레코드에서 되가져온 것이고, 그래서
직렬화·전송·저장을 통과한 뒤에도 상세가 남아 있는지를 말한다.

**열한 호출은 「게이트 대상 verb 각각」의 실측 전개다.** 쌍으로 세면 열하나 —
`create on configmaps` · `update on configmaps` · `update on deployments/scale` ·
`patch on configmaps` · `patch on deployments` · `delete on configmaps` ·
`deletecollection on configmaps` · `create on pods/{exec,attach,portforward,proxy}`. 스케일과
재시작 어노테이션 패치를 각각 전체 교체·일반 패치와 따로 세우는 것은 시나리오가 그 둘을 **별도
상세**로 지목하기 때문이다(레플리카 이동, 그리고 재시작이라는 것을 운영자가 **스스로** 읽어낼 수
있을 만큼의 본문).

**판정은 전부 거부(REJECTED)다.** 이 시나리오가 재는 것은 화면이지 호출의 효과가 아니고,
승인해 버리면 이 파일 하나가 픽스처를 열한 번 바꿔 같은 네임스페이스를 읽는 다른 파일의
전제를 흔든다. 거부는 블록된 호출을 그 자리에서 되돌려 주므로 승인 대기 만료를 기다릴 필요도
없다. 그래서 대상 객체가 「있어야 한다」는 조건은 남지만(게이트가 화면을 만들기 전에 대상을
읽는다) 「바뀌어도 된다」는 조건은 생기지 않는다.

**공통 계약은 열한 번 되풀이하지 않고 마지막 케이스가 모아서 잰다.** 「모든 `context` 에
도구 이름·좌표·요청 시각이 있다」는 *전체에 대한* 단언이라, 케이스마다 복사해 두면 한 화면이
빠져도 나머지 열이 초록을 만든다. 각 케이스는 자기 verb 의 상세만 단언하고 화면을 `SCREENS`
에 남기며, 마지막 케이스가 그 열하나를 놓고 공통 계약과 **쌍의 전집**을 한 번에 판정한다.

**스케일·재시작 화면의 대상은 이 파일만 쓰는 Deployment(`DEPLOYMENT`)다.** 공용
`workload-fixture` 를 쓰면 그것을 스케일하는 `resource_generic_ac8.py` 와 같은 레인에 묶여야
한다 — 화면의 `replicas: 현재 → 목표` 왼쪽을 읽는 사이 남이 스케일하면 단언이 흔들린다.
그래도 기준값은 박지 않고 실행 시점에 읽는다: 왼쪽 값은 인자 어디에도 없어 화면이 스스로
채웠다는 증거가 그 대조뿐이다.
"""

from __future__ import annotations

import asyncio
import datetime
import json
import re
import subprocess
import time
from dataclasses import dataclass

from mcp.shared.exceptions import McpError

from _gatekeeper import count_requests, decide, gatekeeper_url, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE, kubectl_jsonpath

POD = "ag-ac3-target"
POD_PORT = 80
POD_READY_BUDGET = 180.0

CM_CREATED = "ag-ac3-created"
CM_REPLACED = "ag-ac3-replaced"
CM_PATCHED = "ag-ac3-patched"
CM_DOOMED = "ag-ac3-doomed"
COLLECTION = ("ag-ac3-collection-one", "ag-ac3-collection-two")
COLLECTION_LABEL = "ag-ac3"
COLLECTION_VALUE = "collection"

DEPLOYMENT = "ag-ac3-workload"
RESTART_ANNOTATION = "kubectl.kubernetes.io/restartedAt"
RESTART_STAMP = "ag-ac3-restart-2026-09-20T00:00:00Z"

EXEC_COMMAND = ["sh", "-c", "echo ag-ac3-exec-marker && id"]
ATTACH_STDIN = "ag-ac3-attach-marker\n"
ATTACH_READ_SECONDS = 5
FORWARD_PAYLOAD = "GET /ag-ac3-port-forward-marker HTTP/1.0\r\n\r\n"
PROXY_PATH = "/ag-ac3-proxy-marker.txt"
PROXY_BODY = "ag-ac3-proxy-body"

SCALE_TARGET_REPLICAS = 3
GRACE_PERIOD_SECONDS = 7
NAMELESS_MARK = "ag-ac3-nameless"

#: 승인 화면의 두 머리 줄. `approvalContext` 가 이 철자로 쓴다.
PAIR_RE = re.compile(r"^rbac: (.+)$", re.MULTILINE)
TOOL_RE = re.compile(r"^tool: (.+)$", re.MULTILINE)
TARGET_RE = re.compile(r"^target: (.+)$", re.MULTILINE)
REQUESTED_AT_RE = re.compile(r"^requested at: (\S+)$", re.MULTILINE)
REPLICAS_RE = re.compile(r"^replicas: (\d+) → (\d+)$", re.MULTILINE)

#: 화면의 「요청 시각」이 실제 시각인지 보는 창. 위쪽은 시계 어긋남 몫이고, 아래쪽은 이 파일
#: 하나가 도는 데 걸리는 시간보다 넉넉하다 — 재려는 것은 분 단위 정확도가 아니라 **박제된
#: 상수나 0값이 아니라는 것**이다.
CLOCK_SKEW_AHEAD = 120.0
CLOCK_SKEW_BEHIND = 3600.0


@dataclass(frozen=True)
class Screen:
    """한 번의 승인 댄스가 남긴 화면과, 그 화면이 답해야 할 좌표."""

    tool: str
    pair: str
    target_tokens: tuple[str, ...]
    context: str


SCREENS: list[Screen] = []


def _kubectl(*args: str, input: str | None = None, check: bool = True):
    return subprocess.run(
        ["kubectl", "-n", NAMESPACE, *args],
        capture_output=True,
        text=True,
        check=check,
        input=input,
    )


def _apply(manifest: dict) -> None:
    _kubectl("apply", "-f", "-", input=json.dumps(manifest))


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


def _fixtures() -> None:
    """이 파일이 가리키는 객체를 매니페스트 기준으로 세운다.

    `CM_CREATED` 만은 **세우지 않는다** — 그 이름은 create 호출이 만들려다 거부당하는
    대상이고, 미리 만들어 두면 같은 이름의 create 가 승인 화면에 닿기 전에 충돌로 갈릴 수 있다.
    """
    _kubectl("delete", "configmap", CM_CREATED, "--ignore-not-found", "--wait=true")
    for name in (CM_REPLACED, CM_PATCHED, CM_DOOMED):
        _apply(_config_map(name))
    for name in COLLECTION:
        _apply(_config_map(name, {COLLECTION_LABEL: COLLECTION_VALUE}))

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

    _kubectl("delete", "pod", POD, "--ignore-not-found", "--wait=true")
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
                        # stdin 을 여는 것은 attach 호출의 좌표가 성립하기 위해서다. 이 파일은
                        # 모든 호출을 거부로 닫으므로 stdin 에 실제로 무엇이 흘러가지는 않는다.
                        "stdin": True,
                        "stdinOnce": False,
                        "command": [
                            "sh",
                            "-c",
                            f"mkdir -p /www && printf '{PROXY_BODY}' > /www{PROXY_PATH}"
                            f" && exec httpd -f -p {POD_PORT} -h /www",
                        ],
                        "ports": [{"containerPort": POD_PORT}],
                    }
                ],
            },
        }
    )
    deadline = time.monotonic() + POD_READY_BUDGET
    while time.monotonic() < deadline:
        ready = _kubectl(
            "get", "pod", POD, "-o",
            "jsonpath={.status.containerStatuses[0].ready}",
            check=False,
        )
        if ready.stdout.strip() == "true":
            return
        time.sleep(2)
    raise AssertionError(
        f"{POD} 가 {POD_READY_BUDGET:.0f}초 안에 Ready 가 되지 않았다: "
        f"{_kubectl('describe', 'pod', POD, check=False).stdout[-800:]}"
    )


def _line(pattern: re.Pattern[str], context: str, what: str) -> str:
    match = pattern.search(context)
    assert match, f"승인 화면에 {what} 줄이 없다:\n{context}"
    return match.group(1).strip()


def _verbatim(value: str) -> str:
    """JSON 으로 실린 문자열이 화면에서 갖는 철자.

    `arguments:` 블록은 인자를 **JSON 그대로** 싣는다. 개행이나 따옴표를 담은 값은 원문
    철자로는 그 블록에 없으므로, 원문으로 찾으면 「전문이 실렸는가」가 값의 모양에 따라
    거짓 실패한다.
    """
    return json.dumps(value)[1:-1]


async def _screen(
    session,
    gate: str,
    tool: str,
    args: dict,
    marker: str,
    pair: str,
    target_tokens: tuple[str, ...],
) -> str:
    """게이트 대상 호출을 띄우고, 그것이 만든 승인 화면을 읽은 뒤 거부로 닫는다."""
    task = asyncio.create_task(session.call_tool(tool, args))
    row = await wait_for_pending(gate, marker)
    await decide(gate, row["id"], "REJECTED")
    try:
        result = await task
    except McpError as exc:
        assert "approval rejected" in str(exc), exc
    else:
        assert getattr(result, "isError", False), (
            f"{tool} 호출이 거부 판정 뒤에도 성공으로 돌아왔다: {result}"
        )
    context = row["context"]
    SCREENS.append(Screen(tool=tool, pair=pair, target_tokens=target_tokens, context=context))
    return context


async def test_create_shows_the_manifest_it_would_apply(session, gate: str) -> None:
    manifest = _config_map(CM_CREATED)
    context = await _screen(
        session, gate, "resource_create", {"manifest": manifest}, CM_CREATED,
        "create on configmaps", ("v1", "ConfigMap", NAMESPACE, CM_CREATED),
    )
    for token in ("configmaps", CM_CREATED, "data"):
        assert token in context, f"{token!r} 이 create 화면에 없다:\n{context}"


async def test_update_shows_the_whole_replacement(session, gate: str) -> None:
    manifest = _config_map(CM_REPLACED)
    manifest["data"] = {"key": "ag-ac3-replacement-value"}
    context = await _screen(
        session, gate, "resource_update",
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "name": CM_REPLACED,
            "manifest": manifest,
        },
        CM_REPLACED, "update on configmaps",
        ("v1", "ConfigMap", NAMESPACE, CM_REPLACED),
    )
    assert "ag-ac3-replacement-value" in context, context


async def test_scale_shows_the_replica_move(session, gate: str) -> None:
    current = kubectl_jsonpath("{.spec.replicas}", f"deploy/{DEPLOYMENT}")
    assert current.isdigit(), f"기준 레플리카를 읽지 못했다: {current!r}"
    context = await _screen(
        session, gate, "resource_update",
        {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": DEPLOYMENT,
            "subresource": "scale",
            "replicas": SCALE_TARGET_REPLICAS,
        },
        DEPLOYMENT, "update on deployments/scale",
        ("apps/v1", "Deployment/scale", NAMESPACE, DEPLOYMENT),
    )
    move = REPLICAS_RE.search(context)
    assert move, f"승인 화면에 레플리카 이동 줄이 없다:\n{context}"
    # 왼쪽 값은 인자 어디에도 없다 — 이 케이스만 기준값을 kubectl 로 따로 읽는 이유다.
    assert move.group(1) == current, f"현재 레플리카가 {current} 인데 화면은 {move.group(1)}:\n{context}"
    assert move.group(2) == str(SCALE_TARGET_REPLICAS), context


async def test_patch_shows_its_type_and_body_in_full(session, gate: str) -> None:
    patch = {"data": {"key": "ag-ac3-patched-value"}}
    context = await _screen(
        session, gate, "resource_patch",
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "name": CM_PATCHED,
            "patchType": "merge",
            "patch": patch,
        },
        CM_PATCHED, "patch on configmaps",
        ("v1", "ConfigMap", NAMESPACE, CM_PATCHED),
    )
    assert "merge" in context, f"patchType 이 화면에 없다:\n{context}"
    assert "ag-ac3-patched-value" in context, f"패치 본문이 화면에 없다:\n{context}"


async def test_a_restart_patch_carries_its_body_rather_than_a_verdict(session, gate: str) -> None:
    context = await _screen(
        session, gate, "resource_patch",
        {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": DEPLOYMENT,
            "patchType": "merge",
            "patch": {
                "spec": {
                    "template": {
                        "metadata": {"annotations": {RESTART_ANNOTATION: RESTART_STAMP}}
                    }
                }
            },
        },
        RESTART_STAMP, "patch on deployments",
        ("apps/v1", "Deployment", NAMESPACE, DEPLOYMENT),
    )
    # 어노테이션 이름과 값이 **둘 다** 있어야 운영자가 「이건 재시작이구나」를 스스로 읽는다.
    # 서버가 대신 읽어 「재시작」이라고 요약해 주는 화면이었다면 이 둘 중 하나로 족했을 것이다.
    assert RESTART_ANNOTATION in context, context
    assert RESTART_STAMP in context, context


async def test_delete_shows_the_grace_period(session, gate: str) -> None:
    context = await _screen(
        session, gate, "resource_delete",
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "name": CM_DOOMED,
            "gracePeriodSeconds": GRACE_PERIOD_SECONDS,
        },
        CM_DOOMED, "delete on configmaps",
        ("v1", "ConfigMap", NAMESPACE, CM_DOOMED),
    )
    assert f"{GRACE_PERIOD_SECONDS}" in context, context
    assert "gracePeriodSeconds" in context, f"유예 시간이 화면에 없다:\n{context}"


async def test_delete_collection_shows_the_count_and_the_names(session, gate: str) -> None:
    context = await _screen(
        session, gate, "resource_delete_collection",
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "labelSelector": f"{COLLECTION_LABEL}={COLLECTION_VALUE}",
        },
        f"{COLLECTION_LABEL}={COLLECTION_VALUE}", "deletecollection on configmaps",
        ("v1", "ConfigMap", NAMESPACE),
    )
    # 수와 이름을 같은 화면에서 함께 잰다. 셀렉터만 실린 화면은 「무엇이 지워지는가」에 답하지
    # 않고, 수만 실린 화면은 그 답을 운영자가 확인할 방법을 주지 않는다.
    assert f"targets: {len(COLLECTION)}" in context, context
    for name in COLLECTION:
        assert name in context, f"{name} 이 컬렉션 화면에 없다:\n{context}"


async def test_exec_shows_the_command_in_full(session, gate: str) -> None:
    context = await _screen(
        session, gate, "resource_exec",
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "namespace": NAMESPACE,
            "name": POD,
            "container": "httpd",
            "command": EXEC_COMMAND,
        },
        "ag-ac3-exec-marker", "create on pods/exec",
        ("v1", "Pod", NAMESPACE, POD),
    )
    # 인자 하나씩 다 찾는다. 앞머리(`sh`)만 실린 화면은 「셸을 연다」로 읽히고, 실제로 도는
    # 것은 뒤에 붙은 문자열이다.
    for argument in EXEC_COMMAND:
        assert _verbatim(argument) in context, f"{argument!r} 가 화면에 없다:\n{context}"


async def test_attach_shows_the_window_and_the_stdin_in_full(session, gate: str) -> None:
    context = await _screen(
        session, gate, "resource_attach",
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "namespace": NAMESPACE,
            "name": POD,
            "container": "httpd",
            "stdin": ATTACH_STDIN,
            "readSeconds": ATTACH_READ_SECONDS,
        },
        "ag-ac3-attach-marker", "create on pods/attach",
        ("v1", "Pod", NAMESPACE, POD),
    )
    assert _verbatim(ATTACH_STDIN) in context, f"stdin 전문이 화면에 없다:\n{context}"
    assert f"{ATTACH_READ_SECONDS}" in context, context
    assert "readSeconds" in context, context


async def test_port_forward_shows_the_port_and_the_payload_in_full(session, gate: str) -> None:
    context = await _screen(
        session, gate, "resource_port_forward",
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "namespace": NAMESPACE,
            "name": POD,
            "port": POD_PORT,
            "payload": FORWARD_PAYLOAD,
            "readSeconds": ATTACH_READ_SECONDS,
        },
        "ag-ac3-port-forward-marker", "create on pods/portforward",
        ("v1", "Pod", NAMESPACE, POD),
    )
    assert "port" in context and f"{POD_PORT}" in context, context
    # 페이로드는 **전문**이어야 한다 — 앞 몇 글자만 실어도 부분 문자열 단언은 통과하고,
    # 운영자가 보지 못한 나머지가 그대로 포트에 나간다.
    assert _verbatim(FORWARD_PAYLOAD) in context, f"페이로드 전문이 화면에 없다:\n{context}"


async def test_proxy_shows_the_method_the_path_and_the_body(session, gate: str) -> None:
    context = await _screen(
        session, gate, "resource_proxy",
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "namespace": NAMESPACE,
            "name": POD,
            "method": "POST",
            "path": PROXY_PATH,
            "body": PROXY_BODY,
            "contentType": "text/plain",
        },
        PROXY_PATH, "create on pods/proxy",
        ("v1", "Pod", NAMESPACE, POD),
    )
    assert f"POST {PROXY_PATH}" in context, f"메서드와 경로가 화면에 없다:\n{context}"
    assert _verbatim(PROXY_BODY) in context, f"본문 전문이 화면에 없다:\n{context}"


async def test_an_unresolvable_coordinate_is_refused_without_an_approval_request(
    session, gate: str
) -> None:
    before = count_requests(gate, NAMELESS_MARK, "PENDING")
    try:
        result = await session.call_tool(
            "resource_patch",
            {
                "apiVersion": "v1",
                "kind": "ConfigMap",
                "namespace": NAMESPACE,
                "patchType": "merge",
                "patch": {"data": {"key": NAMELESS_MARK}},
            },
        )
    except McpError as exc:
        assert "name is required" in str(exc), exc
    else:
        assert getattr(result, "isError", False), (
            f"이름 없는 좌표의 패치가 거부되지 않았다: {result}"
        )
    # 승인 요청 0건까지 재는 이유: 이 거부가 승인 **뒤에** 오면 운영자는 자기가 무엇을
    # 승인했는지 화면에서 읽을 수 없었던 호출을 이미 승인한 뒤다. 「거부됐다」만 재는 단언은
    # 그 순서 사고를 통과시킨다.
    assert count_requests(gate, NAMELESS_MARK, "PENDING") == before, (
        "좌표 해석 실패가 승인 요청을 만들었다"
    )


def test_every_screen_names_the_tool_the_pair_the_coordinate_and_the_time() -> None:
    assert len(SCREENS) == 11, f"화면 11개를 모으지 못했다: {len(SCREENS)}"
    assert {screen.pair for screen in SCREENS} == {
        "create on configmaps",
        "update on configmaps",
        "update on deployments/scale",
        "patch on configmaps",
        "patch on deployments",
        "delete on configmaps",
        "deletecollection on configmaps",
        "create on pods/exec",
        "create on pods/attach",
        "create on pods/portforward",
        "create on pods/proxy",
    }, sorted(screen.pair for screen in SCREENS)

    now = datetime.datetime.now(datetime.timezone.utc)
    for screen in SCREENS:
        context = screen.context
        assert _line(TOOL_RE, context, "tool") == screen.tool, context
        assert _line(PAIR_RE, context, "rbac") == screen.pair, context
        target = _line(TARGET_RE, context, "target")
        missing = [token for token in screen.target_tokens if token not in target]
        assert not missing, f"{screen.tool} 화면의 좌표에 {missing} 이 없다: {target!r}"
        stamp = _line(REQUESTED_AT_RE, context, "requested at")
        requested = datetime.datetime.strptime(stamp, "%Y-%m-%dT%H:%M:%SZ").replace(
            tzinfo=datetime.timezone.utc
        )
        age = (now - requested).total_seconds()
        assert -CLOCK_SKEW_AHEAD <= age <= CLOCK_SKEW_BEHIND, (
            f"{screen.tool} 화면의 요청 시각 {stamp} 이 이 실행의 창 밖이다 ({age:.0f}초)"
        )


async def run() -> None:
    url = base_url()
    _fixtures()
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- approval-gate/시나리오 3 (create — 매니페스트 전문) ---")
            await test_create_shows_the_manifest_it_would_apply(session, gate)
            print("--- approval-gate/시나리오 3 (update — 교체본 전문) ---")
            await test_update_shows_the_whole_replacement(session, gate)
            print("--- approval-gate/시나리오 3 (update/scale — 현재→목표) ---")
            await test_scale_shows_the_replica_move(session, gate)
            print("--- approval-gate/시나리오 3 (patch — 종류와 본문) ---")
            await test_patch_shows_its_type_and_body_in_full(session, gate)
            print("--- approval-gate/시나리오 3 (patch — 재시작 어노테이션) ---")
            await test_a_restart_patch_carries_its_body_rather_than_a_verdict(session, gate)
            print("--- approval-gate/시나리오 3 (delete — 유예 시간) ---")
            await test_delete_shows_the_grace_period(session, gate)
            print("--- approval-gate/시나리오 3 (deletecollection — 수와 이름) ---")
            await test_delete_collection_shows_the_count_and_the_names(session, gate)
            print("--- approval-gate/시나리오 3 (exec — 명령 전문) ---")
            await test_exec_shows_the_command_in_full(session, gate)
            print("--- approval-gate/시나리오 3 (attach — 창과 stdin 전문) ---")
            await test_attach_shows_the_window_and_the_stdin_in_full(session, gate)
            print("--- approval-gate/시나리오 3 (port_forward — 포트와 페이로드 전문) ---")
            await test_port_forward_shows_the_port_and_the_payload_in_full(session, gate)
            print("--- approval-gate/시나리오 3 (proxy — 메서드·경로·본문) ---")
            await test_proxy_shows_the_method_the_path_and_the_body(session, gate)
            print("--- approval-gate/시나리오 3 (좌표 해석 실패는 요청을 만들지 않는다) ---")
            await test_an_unresolvable_coordinate_is_refused_without_an_approval_request(
                session, gate
            )

    print("--- approval-gate/시나리오 3 (열한 화면의 공통 계약) ---")
    test_every_screen_names_the_tool_the_pair_the_coordinate_and_the_time()
    print("ok: test-approval-gate.md#시나리오 3")


if __name__ == "__main__":
    asyncio.run(run())
