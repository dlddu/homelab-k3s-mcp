"""값이 새는 경로가 막혀 있다 — 목록·민감 종류·스트림 넷·노드 프록시·직접 watch.

검증 시나리오: test-resource-generic.md#시나리오 17
실행 대상: gatekeeper-variant

Go 단위가 이 계약의 조각들을 이미 단언한다 — ``internal/mcp/resource_test.go`` 의 민감 종류
분기와 ``internal/mcp/gate_test.go`` 의 ``TestOrdinaryReadsAndSecretListsStayUngated`` ·
``TestExecSubresourceIsGated``. 그 전부가 **가짜 게이트와 가짜 k8s 서비스** 위에서 돈다: 값이
실제로 표에 실려 나오는지도, 실물 apiserver 가 이 SA 에게 ``watch`` 를 주는지도 그 층에서는
관측되지 않는다. 이 파일이 재는 것이 그 자리다.

**실행 대상이 ``gatekeeper-variant`` 인 이유는 (b) 하나다.** 시나리오는 ``RESOURCE_GATED_KINDS``
에 픽스처 CRD 를 더한 뒤 그 종류로 ``resource_get`` 을 부르라고 한다. 이 env 는 **배포당**이라
primary 를 상대로는 세울 수 없고, 반대로 primary 에 세우면 같은 CRD 를 읽는
``resource_generic_ac1.py`` 의 전제가 흔들린다. 그래서 이미 서 있는 이 변형
(``tests/k8s/kind/gatekeeper-variant.yaml``)의 env 한 줄로 세웠다. 이 변형의 SA 는 primary 와
**같은** ``cluster-admin`` 바인딩이므로(``k8s/rbac.yaml``, 2026-09-14 개정), (d) 의 「``watch``
는 이제 부여되어 403 이 아니다」도 운영과 같은 권한 위에서 관측된다.

**미승인은 사람 없이 만든다 — 이 변형의 ``AUTO_REJECT`` 모드로.** 이 배포는 ``GATEKEEPER_USER_ID``
를 물고 있어 gatekeeper 의 자동 응답 모드가 실제로 발화한다(그래서 ``approval_gate_ac9.py`` 가
여기서 돈다). 판정을 직접 내리는 댄스(``wait_for_pending`` → ``decide``)를 쓰지 않는 것은 이
배포의 ``GATEKEEPER_TIMEOUT_SECONDS`` 가 5 초라, 호출마다 그 5 초 창 안에서 폴링과 판정을 끝내야
하는 경주를 만들기 때문이다. 자동 거부는 그 창을 없앤다 — **그리고 기록은 그대로 남아** 화면
(``context``)을 되읽을 수 있다. 화면을 집는 기준은 **호출 직전에 없던 기록 id 이면서 그 호출의
마커를 담은 것**이다(``_refuse``). 둘 다 필요하다: 이 게이트는 배포 넷이 공유해 같은 문자열을 담은
남의 옛 기록이 있을 수 있고, primary 그룹이 이 그룹과 겹쳐 돌아 그사이 남이 거부한 기록도 생긴다.

**「k8s 호출 카운트 0」 절에 대하여.** 시나리오는 미승인 거부에서 k8s 호출이 0 이기를 요구한다.
호출 수 자체는 SUT 안의 사실이라 e2e 가 셀 수 없고(그 자리는 Go 단위의 몫이다), apiserver 감사
프록시는 이 하네스에 없다 — ``docs/doc-tracker/2026-09.md`` 의 ⏳ 표가
``test-approval-gate.md#시나리오 11`` 행에서 그 부재를 이미 못박고 있다. 그래서 이 파일은
``resource_generic_ac16.py`` 가 같은 자리에서 쓴 독법을 따라 **밖에서 관측 가능한 등가물**을
단언한다: 거부된 호출은 **어떤 응답에도 토큰을 싣지 않았고**, 승인 화면에도 값이 아니라 **경로**
만 실리며, 대상은 그대로 남아 있다. 「호출은 갔지만 아무 값도 오지 않았다」와 「호출이 가지
않았다」를 이 층에서 가를 수 없다는 사실을 숨기지 않으려고 여기 적는다.

**토큰은 픽스처가 박는 상수다.** 시나리오가 말하는 「고유 난수」의 요점은 매 실행 새로 뽑히는
것이 아니라 **그 문자열이 이 레포의 다른 어디에도 없어서**, 어떤 응답에서 발견되면 그것이 이
Secret 에서 새어 나온 것이라고 단정할 수 있다는 데 있다. ``resource_generic_ac16.py`` 의
``TOKEN`` 과 같은 방식이다.

**대상 Secret 과 서빙 파드는 이 파일이 스스로 세운다.** 자매
``resource_generic_ac15.py``·``approval_gate_ac3.py`` 가 파드를 세우는 방식 그대로이고, 러너가
파일을 자동 발견하므로 ``ci.yml`` 배선은 늘지 않는다. 이미지는 이미 클러스터에 있는
``busybox:1.36`` 이라 새 pull 도 없다. 파드의 문서 루트를 Secret 마운트로 두면 ``exec`` 의
``cat``, ``port_forward`` 의 ``GET``, ``proxy`` 의 경로가 **같은 값**을 가리켜, 넷이 다 막혀야
한다는 주장이 한 대상 위에서 성립한다.

``병렬 레인:`` 을 선언하지 않는 것은 의도다 — 이 파일은 게이트의 자동 응답 모드를 바꾸므로,
같은 게이트를 쓰는 다른 파일과 동시에 돌면 남의 승인 댄스를 대신 거절해 버린다.
"""

from __future__ import annotations

import asyncio
import base64
import json
import pathlib
import re
import ssl
import subprocess
import tempfile
import time

import httpx
from mcp.shared.exceptions import McpError

from _gatekeeper import gatekeeper_url, list_requests, set_auto_response
from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE

SECRET = "rg-ac17-token"

#: 이 레포 어디에도 없는 문자열. 응답에서 발견되면 그것은 위 Secret 에서 나온 것이다.
TOKEN = "rg-ac17-token-4d1f90ab5c73"

POD = "rg-ac17-token-server"
CONTAINER = "httpd"
POD_PORT = 80
POD_READY_BUDGET = 180.0

#: Secret 을 파일로 얹는 자리이자 서빙 파드의 문서 루트.
MOUNT = "/var/run/secrets/rg-ac17"
TOKEN_PATH = f"{MOUNT}/token"

#: ``RESOURCE_GATED_KINDS`` 가 이 변형에서 민감 종류로 더한 픽스처 CRD와 그 인스턴스
#: (``tests/k8s/kind/resource-generic-fixture.yaml`` · ``resource-generic-samples.yaml``).
SAMPLE_API_VERSION = "homelab-k3s-mcp.test/v1"
SAMPLE_KIND = "ResourceGenericSample"
SAMPLE = "resource-generic-selected"

#: (b) 의 대조군. 같은 도구·같은 네임스페이스인데 민감 목록에 없는 종류라 승인 없이 지나간다 —
#: 이것이 없으면 「그 종류가 막혔다」와 「그 도구가 막혔다」가 구별되지 않는다.
CONTROL_MAP = "rg-ac17-control"

VARIANT_NAMESPACE = "homelab-k3s-mcp-gatekeeper-variant"
VARIANT_SERVICE_ACCOUNT = "homelab-k3s-mcp"

EXEC_COMMAND = ["sh", "-c", f"cat {TOKEN_PATH}"]
ATTACH_STDIN = "rg-ac17-attach-marker\n"
ATTACH_READ_SECONDS = 5
FORWARD_PAYLOAD = "GET /token HTTP/1.0\r\nHost: rg-ac17\r\n\r\n"
PROXY_PATH = "/token"

#: 노드 프록시로 같은 파일을 읽으려는 호출. kubelet 의 ``/exec`` 은 POST 를 받으므로 쌍의
#: verb 는 ``create`` 다 — 시나리오가 「쌍이 ``create nodes/proxy`` 로만 기록된다」고 적은
#: 자리다. 이 호출도 승인 없이 거부되므로 kubelet 이 실제로 무엇을 답하는지는 여기서 재지
#: 않는다(그 자리는 ``resource_generic_ac15.py`` 다).
NODE_EXEC_PATH = f"/exec/{NAMESPACE}/{POD}/{CONTAINER}?command=cat&command={TOKEN_PATH}"

#: 승인 화면의 쌍 줄. ``approvalContext`` 가 이 철자로 쓴다(``<verb> on <resource>``).
PAIR_RE = re.compile(r"^rbac: (.+)$", re.MULTILINE)

AUTO_REJECT_MARK = "auto-rejected"
# name 이 없는 (d) 의 watch 에는 대상 이름이 context 에 없다 — 이 셀렉터가 그 요청의 마커다.
WATCH_SELECTOR = "rg-ac17=list-scope"

#: 자동 거부 기록이 목록에 나타나기를 기다리는 창. 판정은 요청 생성 직후라 즉시지만, 기록
#: 목록은 별도 조회라 한 박자 늦을 수 있다.
RECORD_BUDGET = 30.0


def _kubectl(*args: str, input: str | None = None, check: bool = True):
    return subprocess.run(
        ["kubectl", *args], capture_output=True, text=True, check=check, input=input
    )


def _apply(manifest: dict) -> None:
    _kubectl("apply", "-f", "-", input=json.dumps(manifest))


def _node_name() -> str:
    name = _kubectl(
        "get", "nodes", "-o", "jsonpath={.items[0].metadata.name}"
    ).stdout.strip()
    assert name, "클러스터에서 노드 이름을 읽지 못했다"
    return name


def _verbatim(value: str) -> str:
    """JSON 으로 실린 문자열이 화면에서 갖는 철자.

    ``arguments:`` 블록은 인자를 **JSON 그대로** 싣는다. 개행이나 따옴표를 담은 값은 원문
    철자로는 그 블록에 없으므로, 원문으로 찾으면 「전문이 실렸는가」가 값의 모양에 따라 거짓
    실패한다(``approval_gate_ac3.py`` 의 같은 헬퍼).
    """
    return json.dumps(value)[1:-1]


def _fixtures() -> None:
    """토큰 Secret, 대조군 ConfigMap, 그리고 토큰을 서빙하는 HTTP 파드를 세운다."""
    _apply(
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {"name": SECRET, "namespace": NAMESPACE},
            "stringData": {"token": TOKEN},
        }
    )
    _apply(
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {"name": CONTROL_MAP, "namespace": NAMESPACE},
            "data": {"key": "control"},
        }
    )

    _kubectl("-n", NAMESPACE, "delete", "pod", POD, "--ignore-not-found", "--wait=true")
    _apply(
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "metadata": {"name": POD, "namespace": NAMESPACE, "labels": {"app": POD}},
            "spec": {
                "restartPolicy": "Never",
                "volumes": [{"name": "token", "secret": {"secretName": SECRET}}],
                "containers": [
                    {
                        "name": CONTAINER,
                        "image": "busybox:1.36",
                        # attach 호출의 좌표가 성립하려면 stdin 이 열려 있어야 한다. 이 파일은
                        # 모든 스트림 호출을 거부로 닫으므로 실제로 흘러가는 것은 없다.
                        "stdin": True,
                        "stdinOnce": False,
                        "command": [
                            "sh",
                            "-c",
                            f"exec httpd -f -p {POD_PORT} -h {MOUNT}",
                        ],
                        "ports": [{"containerPort": POD_PORT}],
                        "volumeMounts": [
                            {"name": "token", "mountPath": MOUNT, "readOnly": True}
                        ],
                    }
                ],
            },
        }
    )
    deadline = time.monotonic() + POD_READY_BUDGET
    while time.monotonic() < deadline:
        ready = _kubectl(
            "-n", NAMESPACE, "get", "pod", POD,
            "-o", "jsonpath={.status.containerStatuses[0].ready}",
            check=False,
        )
        if ready.stdout.strip() == "true":
            return
        time.sleep(2)
    raise AssertionError(
        f"{POD} 가 {POD_READY_BUDGET:.0f}초 안에 Ready 가 되지 않았다: "
        f"{_kubectl('-n', NAMESPACE, 'describe', 'pod', POD, check=False).stdout[-800:]}"
    )


def _pair(context: str) -> str:
    match = PAIR_RE.search(context)
    assert match, f"화면에 rbac 줄이 없다:\n{context}"
    return match.group(1).strip()


async def _refuse(session, gate: str, tool: str, args: dict, marker: str) -> str:
    """게이트 대상 호출 하나를 태우고, 자동 거부로 끝났음을 확인한 뒤 그 화면을 돌려준다.

    화면은 **이 호출이 새로 만든 기록**에서 읽는다 — 호출 직전의 거부 기록 id 집합 밖에
    나타난 것 가운데 ``marker`` 를 담은 것을 집는다(모듈 머리말).
    """
    before = {row["id"] for row in list_requests(gate, status="REJECTED")}
    try:
        result = await session.call_tool(tool, args)
    except McpError as exc:
        message = str(exc)
        assert AUTO_REJECT_MARK in message, f"{tool}: 자동 거부가 아닌 오류다: {message}"
        assert TOKEN not in message, f"{tool}: 거부 문면에 토큰이 실렸다"
    else:
        raise AssertionError(f"{tool} 이 승인 없이 실행됐다: {result}")

    deadline = time.monotonic() + RECORD_BUDGET
    while time.monotonic() < deadline:
        fresh = [
            row
            for row in list_requests(gate, status="REJECTED")
            if row["id"] not in before and marker in row.get("context", "")
        ]
        if fresh:
            return fresh[-1]["context"]
        time.sleep(0.2)
    raise AssertionError(f"{tool} 의 자동 거부 기록이 {RECORD_BUDGET:.0f}초 안에 없었다")


def _apiserver() -> tuple[str, ssl.SSLContext]:
    """kubeconfig 가 가리키는 apiserver 주소와, 그 CA 로 세운 TLS 컨텍스트.

    (d) 는 **서버의 SA 토큰으로 직접** apiserver 를 부르라고 한다. kubectl 로는 그렇게 부를 수
    없다 — 클라이언트 인증서를 든 kubeconfig 가 ``--token`` 보다 앞서기 때문이다. 그래서 주소와
    CA 만 kubeconfig 에서 꺼내고 호출은 httpx 로 한다.

    CA 를 파일 경로가 아니라 ``SSLContext`` 로 넘기는 것은 버전 호환이다 —
    ``requirements.txt`` 가 ``httpx>=0.27,<1`` 이라 CI 는 그 범위의 최신을 집는데,
    ``verify`` 가 문자열 경로를 받는 형태는 그 범위 안에서 갈린다. ``SSLContext`` 는 갈리지
    않는다.
    """
    server = _kubectl(
        "config", "view", "--raw", "--minify",
        "-o", "jsonpath={.clusters[0].cluster.server}",
    ).stdout.strip()
    assert server, "kubeconfig 에서 apiserver 주소를 읽지 못했다"

    data = _kubectl(
        "config", "view", "--raw", "--minify",
        "-o", "jsonpath={.clusters[0].cluster.certificate-authority-data}",
    ).stdout.strip()
    if data:
        path = pathlib.Path(tempfile.mkdtemp()) / "ca.crt"
        path.write_bytes(base64.b64decode(data))
        return server, ssl.create_default_context(cafile=str(path))

    file_path = _kubectl(
        "config", "view", "--raw", "--minify",
        "-o", "jsonpath={.clusters[0].cluster.certificate-authority}",
    ).stdout.strip()
    assert file_path, (
        "kubeconfig 에 CA 가 없다 — 검증을 끄고 부르면 이 케이스가 재는 것이 「누가 답했는가」가 "
        "아니게 되므로 여기서 멈춘다"
    )
    return server, ssl.create_default_context(cafile=file_path)


async def test_the_secret_list_passes_ungated_and_carries_no_values(
    session, gate: str
) -> None:
    """(a) — ``kind=Secret`` 의 목록은 승인 없이 지나가고, 표에 값이 없다."""
    before = {row["id"] for row in list_requests(gate)}

    result = await session.call_tool(
        "resource_list",
        {"apiVersion": "v1", "kind": "Secret", "namespace": NAMESPACE},
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload is not None, result

    columns = [column["name"] for column in payload["columns"]]
    # apiserver 의 Secret Table 정의가 내는 네 컬럼. 다섯 번째가 생겼다면 그것이 값을 실어
    # 나르는 자리인지부터 봐야 하므로, 여기서는 **목록 전체로** 못박는다.
    assert columns == ["Name", "Type", "Data", "Age"], f"Secret 표의 컬럼: {columns}"

    names = {row[columns.index("Name")] for row in payload["rows"]}
    assert SECRET in names, f"토큰 Secret 이 목록에 없다: {sorted(names)}"

    assert TOKEN not in json.dumps(payload, ensure_ascii=False), "토큰이 표에 실려 나왔다"
    for block in result.content:
        assert TOKEN not in getattr(block, "text", ""), "토큰이 표 텍스트에 실려 나왔다"

    # 게이트는 배포 넷이 공유하므로 「요청 수가 그대로다」로 재지 않는다 — 이 호출이 만들었을
    # 요청만 골라 본다(``resource_generic_ac16.py`` 의 같은 자리와 같은 독법).
    offenders = [
        row
        for row in list_requests(gate)
        if row["id"] not in before and "tool: resource_list" in row.get("context", "")
    ]
    assert not offenders, (
        f"목록이 승인 요청을 만들었다 — 이 호출은 게이트를 타지 않아야 한다: {offenders}"
    )


async def test_a_gated_custom_kind_is_refused_while_its_control_passes(
    session, gate: str
) -> None:
    """(b) — ``RESOURCE_GATED_KINDS`` 에 더해진 CRD 는 읽기도 승인을 거친다."""
    screen = await _refuse(
        session,
        gate,
        "resource_get",
        {
            "apiVersion": SAMPLE_API_VERSION,
            "kind": SAMPLE_KIND,
            "namespace": NAMESPACE,
            "name": SAMPLE,
        },
        SAMPLE,
    )
    assert _pair(screen) == "get on resourcegenericsamples", screen
    assert SAMPLE in screen, f"대상 이름이 화면에 없다:\n{screen}"

    # 대조군: 민감 목록에 없는 종류는 같은 도구로 승인 없이 지나간다. 이것이 없으면 위 거부가
    # 「그 종류가 민감해서」인지 「그 도구가 막혀서」인지 이 파일 안에서 갈리지 않는다.
    control = await session.call_tool(
        "resource_get",
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "name": CONTROL_MAP,
        },
    )
    assert control.isError is False, control


async def test_the_four_stream_tools_are_refused_and_show_the_path_not_the_value(
    session, gate: str
) -> None:
    """(c) — 스트림 넷이 미승인 거부되고, 화면에는 명령·페이로드·경로가 전문으로 실린다."""
    coordinate = {
        "apiVersion": "v1",
        "kind": "Pod",
        "namespace": NAMESPACE,
        "name": POD,
    }

    exec_screen = await _refuse(
        session,
        gate,
        "resource_exec",
        {**coordinate, "container": CONTAINER, "command": EXEC_COMMAND},
        POD,
    )
    assert _pair(exec_screen) == "create on pods/exec", exec_screen
    # 인자를 하나씩 다 찾는다. 앞머리(``sh``)만 실린 화면은 「셸을 연다」로 읽히고, 실제로 도는
    # 것은 뒤에 붙은 경로다 — 운영자가 거절할 근거가 거기 있다.
    for argument in EXEC_COMMAND:
        assert _verbatim(argument) in exec_screen, (
            f"{argument!r} 가 화면에 없다:\n{exec_screen}"
        )

    attach_screen = await _refuse(
        session,
        gate,
        "resource_attach",
        {
            **coordinate,
            "container": CONTAINER,
            "stdin": ATTACH_STDIN,
            "readSeconds": ATTACH_READ_SECONDS,
        },
        POD,
    )
    assert _pair(attach_screen) == "create on pods/attach", attach_screen
    assert _verbatim(ATTACH_STDIN) in attach_screen, (
        f"stdin 전문이 화면에 없다:\n{attach_screen}"
    )

    forward_screen = await _refuse(
        session,
        gate,
        "resource_port_forward",
        {
            **coordinate,
            "port": POD_PORT,
            "payload": FORWARD_PAYLOAD,
            "readSeconds": ATTACH_READ_SECONDS,
        },
        POD,
    )
    assert _pair(forward_screen) == "create on pods/portforward", forward_screen
    # 페이로드는 **전문**이어야 한다 — 앞 몇 글자만 실어도 부분 문자열 단언은 통과하고,
    # 운영자가 보지 못한 나머지가 그대로 포트에 나간다.
    assert _verbatim(FORWARD_PAYLOAD) in forward_screen, (
        f"페이로드 전문이 화면에 없다:\n{forward_screen}"
    )

    proxy_screen = await _refuse(
        session,
        gate,
        "resource_proxy",
        {**coordinate, "method": "GET", "path": PROXY_PATH},
        POD,
    )
    assert _pair(proxy_screen) == "get on pods/proxy", proxy_screen
    assert PROXY_PATH in proxy_screen, f"경로가 화면에 없다:\n{proxy_screen}"

    # 넷 다 화면에 **경로**를 싣고 **값**은 싣지 않는다. 값이 화면에 실리면 승인 화면 자체가
    # 이 시나리오가 막으려는 유출 경로가 된다.
    for screen in (exec_screen, attach_screen, forward_screen, proxy_screen):
        assert TOKEN not in screen, f"승인 화면에 토큰이 실렸다:\n{screen}"

    # 「거부가 상태에 닿지 않았다」의 관측면: 대상 파드는 그대로 있다.
    assert (
        _kubectl("-n", NAMESPACE, "get", "pod", POD, check=False).returncode == 0
    ), "거부된 스트림 호출 뒤 대상 파드가 사라졌다"


async def test_the_node_proxy_is_recorded_only_as_the_node_pair(
    session, gate: str
) -> None:
    """(c') — 노드 프록시로 같은 파일을 읽어도 쌍은 ``create on nodes/proxy`` 뿐이다."""
    screen = await _refuse(
        session,
        gate,
        "resource_proxy",
        {
            "apiVersion": "v1",
            "kind": "Node",
            "name": _node_name(),
            "method": "POST",
            "path": NODE_EXEC_PATH,
        },
        POD,
    )
    pair = _pair(screen)

    # 시나리오의 논점: 이 호출은 Secret 의 **값**에 닿으려 하는데 쌍에는 ``secrets`` 가 없다.
    # 쌍으로 막을 수 없는 경로라는 것이고, 남는 방어선은 화면의 경로 노출뿐이다.
    assert pair == "create on nodes/proxy", screen
    assert "secret" not in pair, f"쌍에 민감 종류가 섞였다: {pair}"
    assert _verbatim(NODE_EXEC_PATH) in screen, f"경로 전문이 화면에 없다:\n{screen}"
    assert TOKEN not in screen, f"승인 화면에 토큰이 실렸다:\n{screen}"


async def test_the_direct_watch_is_granted_by_rbac_and_refused_by_the_kind_gate(
    session, gate: str
) -> None:
    """(d) — ``watch`` 는 부여돼 403 이 아니고, 막는 것은 민감 종류 게이트다."""
    token = _kubectl(
        "-n", VARIANT_NAMESPACE, "create", "token", VARIANT_SERVICE_ACCOUNT,
        "--duration=10m",
    ).stdout.strip()
    assert token, "SA 토큰을 발급하지 못했다"

    server, context = _apiserver()
    response = httpx.get(
        f"{server}/api/v1/secrets",
        params={"watch": "true", "timeoutSeconds": "1"},
        headers={"Authorization": f"Bearer {token}"},
        verify=context,
        timeout=30.0,
    )
    # 403 이면 「RBAC 이 막았다」이고, 그때 이 시나리오의 (d) 는 성립하지 않는다 — 스트림 우회
    # 경로가 RBAC 으로 막혀 있다면 게이트가 유일한 경계라는 주장이 거짓이 된다.
    assert response.status_code == 200, (
        f"SA 의 직접 watch 가 {response.status_code} 로 끝났다: {response.text[:200]}"
    )

    # 같은 watch 를 도구로 부르면 민감 종류 게이트가 받는다.
    screen = await _refuse(
        session,
        gate,
        "resource_watch",
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "namespace": NAMESPACE,
            "watchSeconds": 2,
            "labelSelector": WATCH_SELECTOR,
        },
        WATCH_SELECTOR,
    )
    assert _pair(screen) == "watch on secrets", screen
    assert TOKEN not in screen, f"승인 화면에 토큰이 실렸다:\n{screen}"


async def run() -> None:
    url = base_url()
    _fixtures()
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- resource-generic/시나리오 17 (a) 목록은 게이트를 타지 않는다 ---")
            await test_the_secret_list_passes_ungated_and_carries_no_values(session, gate)

            # 나머지 갈래는 전부 「승인 없이」를 재므로, 사람 대신 자동 거부가 판정을 내린다.
            set_auto_response(gate, "AUTO_REJECT")
            try:
                print("--- resource-generic/시나리오 17 (b) 민감 종류가 된 CRD ---")
                await test_a_gated_custom_kind_is_refused_while_its_control_passes(
                    session, gate
                )
                print("--- resource-generic/시나리오 17 (c) 스트림 넷 ---")
                await test_the_four_stream_tools_are_refused_and_show_the_path_not_the_value(
                    session, gate
                )
                print("--- resource-generic/시나리오 17 (c') 노드 프록시 ---")
                await test_the_node_proxy_is_recorded_only_as_the_node_pair(session, gate)
                print("--- resource-generic/시나리오 17 (d) 직접 watch ---")
                await test_the_direct_watch_is_granted_by_rbac_and_refused_by_the_kind_gate(
                    session, gate
                )
            finally:
                set_auto_response(gate, "NONE")

    print("ok: test-resource-generic.md#시나리오 17")


if __name__ == "__main__":
    asyncio.run(run())
