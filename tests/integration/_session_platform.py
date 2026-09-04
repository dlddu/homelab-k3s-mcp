"""session-* 파일들이 공유하는 제어면 표면 (매칭 단위가 아니다).

`_` 접두라 `run_all.py::matching_unit_paths()` 가 걸러내므로 AC를 주검증하지 않고
`검증 AC:` 선언도 갖지 않는다 — `_workload.py`·`_auth_variant.py` 와 같은 자리다.

These helpers drive ``tests/k8s/kind/session-platform.yaml``: the **real**
session-platform control plane, running its published image in the kind
harness. There are two ways in, and which one a file needs is decided by
whether its AC leaves the state store.

*Seeding* writes a session straight into the control plane's own store. It is
what session-list/AC1..AC3 use: ``List`` reads only these ConfigMaps and never
looks at pods, so a seeded session is a complete one as far as listing is
concerned, and states like ``idle``/``snapshot`` -- reachable in production
only through the 60-minute idle path or a CRIU checkpoint -- become
constructible. Seeding the real store is the same move the harness already
makes for MinIO (the ``minio-seed`` Job), and it leaves the control plane's own
API, decoding and normalization on the path under test.

*Creating* goes through the product API and provisions a real data plane pod.
session-read/AC1 and session-write/AC1 need it: both end in ``agent.Read`` /
``agent.Write``, which resolve the session's stored pod name to a pod IP and
dial the agent there, so a seeded name with no pod behind it has nothing to
read or write. See ``live_shell_session`` at the bottom of this module.

The store's representation is not invented here. session-platform's
``control-plane/internal/adapter/configmap`` keeps one ConfigMap per session,
named ``session-<id>``, labelled with the control plane's ownership labels, and
carrying the ``session.Session`` JSON under a single ``session`` data key. The
field names below are that struct's JSON tags; ``List`` reads only these
ConfigMaps and never looks at pods, which is why a seeded session is a complete
one as far as listing is concerned.
"""

from __future__ import annotations

import contextlib
import json
import subprocess
import time
from collections.abc import Iterator

import httpx

from _helpers import port_forward

#: 제어면 픽스처의 네임스페이스. 프로덕션과 같은 이름이라 base 매니페스트의
#: ``SESSION_PLATFORM_ENDPOINT`` (``k8s/deployment.yaml``) 가 그대로 해석된다.
NAMESPACE = "session-platform"

#: The control plane's ownership label; its store lists exactly the ConfigMaps
#: carrying it, so a seeded session must carry it to be visible.
MANAGED_BY_LABEL = "app.kubernetes.io/managed-by"
MANAGED_BY_VALUE = "control-plane"

#: Ties one stored object to one session id.
SESSION_ID_LABEL = "session-id"

#: ConfigMap name prefix and the single data key holding the session JSON.
NAME_PREFIX = "session-"
DATA_KEY = "session"

#: 시드 세션에 붙이는 파드 이름 접두. 실 제어면이 세션을 위해 만드는 파드와
#: 구분되지 않아도 되지만, 진단 로그에서 시드분임이 드러나는 편이 낫다.
SEED_POD_PREFIX = "session-"


def kubectl(*args: str, check: bool = True) -> str:
    """Run kubectl against the fixture namespace and return stdout."""
    proc = subprocess.run(
        ["kubectl", "-n", NAMESPACE, *args], capture_output=True, text=True
    )
    if check and proc.returncode != 0:
        raise RuntimeError(
            f"kubectl {' '.join(args)} failed ({proc.returncode}): "
            f"{proc.stderr.strip() or proc.stdout.strip()}"
        )
    return proc.stdout


def session_payload(
    *,
    session_id: str,
    name: str,
    workload_type: str,
    state: str,
    created_at: str,
    last_access: str,
    pod: str | None = None,
) -> dict:
    """One stored session, shaped like the control plane's ``session.Session``.

    ``pod`` is omitted (not empty) for a snapshotted session: its pods have been
    reclaimed, and the field is ``omitempty`` on the wire. ``checkpoint`` and
    ``model`` are omitted too -- session_list exposes neither, and the control
    plane's decode ignores fields the PRD does not surface.
    """
    payload = {
        "id": session_id,
        "workloadType": workload_type,
        "name": name,
        "state": state,
        "createdAt": created_at,
        "lastAccess": last_access,
    }
    if pod:
        payload["pod"] = pod
    return payload


def _configmap_manifest(payload: dict) -> dict:
    session_id = payload["id"]
    return {
        "apiVersion": "v1",
        "kind": "ConfigMap",
        "metadata": {
            "name": NAME_PREFIX + session_id,
            "namespace": NAMESPACE,
            "labels": {
                MANAGED_BY_LABEL: MANAGED_BY_VALUE,
                SESSION_ID_LABEL: session_id,
            },
        },
        "data": {DATA_KEY: json.dumps(payload)},
    }


def clear_sessions() -> None:
    """Remove every session the control plane owns, leaving an empty inventory.

    Each file calls this (directly or through :func:`seed_sessions`) at the top
    of its ``run()`` so it establishes its own precondition instead of depending
    on which file ran before it -- the same rule the workload files follow. The
    selector is the control plane's ownership label, so this never touches a
    ConfigMap the fixture did not create.
    """
    kubectl(
        "delete",
        "configmap",
        "-l",
        f"{MANAGED_BY_LABEL}={MANAGED_BY_VALUE}",
        "--ignore-not-found",
    )


def seed_sessions(payloads: list[dict]) -> None:
    """Make the control plane's inventory exactly ``payloads``."""
    clear_sessions()
    if not payloads:
        return
    manifest = {
        "apiVersion": "v1",
        "kind": "List",
        "items": [_configmap_manifest(p) for p in payloads],
    }
    proc = subprocess.run(
        ["kubectl", "-n", NAMESPACE, "apply", "-f", "-"],
        input=json.dumps(manifest),
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(
            f"seeding sessions failed ({proc.returncode}): "
            f"{proc.stderr.strip() or proc.stdout.strip()}"
        )


def pod_names() -> set[str]:
    """Every pod in the control plane's namespace, control plane itself included.

    session-list/AC2 asserts that a listing starts no compute. A snapshot
    restore would provision a session pod here, so the set staying identical
    across repeated listings is a real discriminator -- which it would not be
    against a stand-in that has no pods to create in the first place.
    """
    out = kubectl("get", "pods", "-o", "jsonpath={.items[*].metadata.name}")
    return set(out.split())


def sessions_from(result) -> list[dict]:
    """Pull the session list out of a successful ``session_list`` tool result."""
    assert result.isError is False, result
    return result.structuredContent["sessions"]


# --- real sessions, created through the control plane's product API ----------
#
# Seeded ConfigMaps are enough for session_list, which never leaves the store.
# session-read/AC1, session-write/AC1 and session-write/AC4 do: the control
# plane resolves the session's stored pod name to a pod IP and dials that pod's
# agent, so those ACs need a session whose pod actually exists and answers. The
# only supported way to get one is the product API, which provisions the pod
# and waits for it (ClientOrchestrator.Start), so that is what these helpers
# drive.
#
# Creating a session is *setup*, not the thing under test: none of those ACs is
# about session creation, and the MCP tool surface has no create tool, so this
# reaches the control plane directly over a short port-forward the way the
# OAuth files reach dex. What the ACs are about -- reading a cursor, injecting
# input, telling four refusals apart -- goes through the MCP tools, on the
# deployed server, as usual.
#
# Two workload types stand up here. `shell` is a PTY the agent forks; the
# `claude-code` pod additionally carries the loopback credential proxy sidecar
# and projects the placeholder Secret the fixture defines. Which one a file
# needs is decided by its AC: the payload ceiling and the prompt queue that
# session-write/AC4 asks about exist only for the agent type.

#: 제어면 Service 와 그 포트 (이 파일 위쪽 픽스처가 세운다).
CONTROL_PLANE_SERVICE = "control-plane"
CONTROL_PLANE_PORT = 80

#: 로컬 포워드 포트. 18080·18081·18082 는 platform-auth-safety 파일들이 쓴다.
CONTROL_PLANE_LOCAL_PORT = 18083

#: 제어면 REST 표면. OpenAPI 문서가 선언한 단일 서버 접두 `/api/v1` 를 포함한다.
HEALTHZ_PATH = "/api/v1/healthz"
SESSIONS_PATH = "/api/v1/sessions"

#: 생성 호출의 클라이언트 타임아웃. 제어면은 파드가 Ready 가 될 때까지 기다린 뒤
#: (기본 2분) attach 스트림까지 열어 보고서야 201 을 낸다 — 그 예산보다 넉넉해야
#: 클라이언트가 먼저 포기해 세션을 고아로 남기지 않는다.
CREATE_TIMEOUT = 300.0

#: 워크로드 타입. 픽스처가 `DATA_PLANE_IMAGE` 와 `DATA_PLANE_CLAUDE_CODE_IMAGE` 를
#: 둘 다 주므로 두 타입 모두 여기서 뜬다. `approval-gated` 는 세션 헬퍼 파드(승인
#: 게이트웨이·세션 MCP)를 더 요구하므로 이 픽스처의 대상이 아니다.
SHELL_WORKLOAD = "shell"
CLAUDE_CODE_WORKLOAD = "claude-code"

#: 제어면이 `workloadType=claude-code` 에 강제하는 프롬프트 상한(바이트).
#: session-platform 의 `session.MaxClaudePromptBytes` 와 같은 값이며, 제어면은
#: 이 상한을 **파드에 닿기 전에** 잰다(`Service.Write` 가 `activate` 보다 먼저
#: `WorkloadType == claude-code && len(payload) > MaxClaudePromptBytes` 를 본다).
MAX_CLAUDE_PROMPT_BYTES = 1 << 20

#: 세션 삭제 뒤 파드가 실제로 사라질 때까지의 예산. 기본 종료 유예가 30초이므로
#: 그보다 넉넉해야 한다.
RECLAIM_TIMEOUT = 150.0
RECLAIM_POLL = 2.0


@contextlib.contextmanager
def live_shell_session(name: str) -> Iterator[tuple[str, dict]]:
    """Create one real shell session, yield ``(control_plane_url, session)``.

    The session is deleted on the way out, which reclaims its pod, so a file
    that uses this leaves the cluster as it found it. ``control_plane_url``
    stays valid for the body: a caller that needs to make the workload produce
    output can post to it without going through the tool under test.
    """
    with _live_session(name, SHELL_WORKLOAD) as pair:
        yield pair


@contextlib.contextmanager
def live_claude_code_session(name: str) -> Iterator[tuple[str, dict]]:
    """Create one real claude-code session, yield ``(control_plane_url, session)``.

    Same lifecycle as :func:`live_shell_session`; only the workload type
    differs. The pod this provisions carries two containers -- the agent
    running its serial one-shot Claude runner, and the loopback credential
    proxy sidecar the control plane attaches for this type -- and both must
    report Ready before the control plane answers 201, so a session yielded
    here is evidence that the claude-code data plane stands up in kind.

    ``model`` is deliberately omitted from the create request: the control
    plane normalizes an empty model to ``platform-default`` for the agent
    types (``session.NormalizeModel``), which is the same value the pod would
    resolve from the Secret's optional ``model`` key -- and that key is absent
    from the fixture on purpose.

    No case built on this asserts a *completed* invocation. The fixture's
    credentials are placeholders, so a prompt the agent's worker drains will
    fail upstream; every assertion here is about what the platform decides
    before an invocation runs (acceptance, the payload ceiling, the bounded
    queue), which is exactly what session-write/AC1's claude-code clause and
    session-write/AC4 are about.
    """
    with _live_session(name, CLAUDE_CODE_WORKLOAD) as pair:
        yield pair


@contextlib.contextmanager
def _live_session(name: str, workload_type: str) -> Iterator[tuple[str, dict]]:
    """The shared body of the two lifecycle helpers above.

    The request and response shapes are session-platform's, read from
    ``control-plane/internal/api/api.go`` (``createReq``, and the ``Session``
    the create handler writes back) rather than invented here.
    """
    with port_forward(
        NAMESPACE,
        CONTROL_PLANE_SERVICE,
        CONTROL_PLANE_PORT,
        CONTROL_PLANE_LOCAL_PORT,
        ready_path=HEALTHZ_PATH,
    ) as url:
        response = httpx.post(
            f"{url}{SESSIONS_PATH}",
            json={"name": name, "workloadType": workload_type},
            timeout=CREATE_TIMEOUT,
        )
        assert response.status_code == 201, (
            f"creating a {workload_type} session returned "
            f"{response.status_code}: {response.text.strip()}"
        )
        session = response.json()
        assert session.get("state") == "active", session
        assert session.get("pod"), (
            f"a created session must name its workload pod: {session}"
        )
        try:
            yield url, session
        finally:
            deleted = httpx.delete(
                f"{url}{SESSIONS_PATH}/{session['id']}", timeout=120.0
            )
            assert deleted.status_code in (204, 404), (
                f"deleting session {session['id']} returned "
                f"{deleted.status_code}: {deleted.text.strip()}"
            )
            _wait_for_pod_gone(session["pod"])


def _wait_for_pod_gone(pod: str, timeout: float = RECLAIM_TIMEOUT) -> None:
    """Block until the session's pod is actually gone from the namespace.

    Deleting a session returns as soon as the control plane has issued the pod
    deletion, but the pod lingers in ``Terminating`` until its grace period
    elapses. That matters to the *next* file rather than to this one:
    ``session_read_ac3.py`` samples the pod set and asserts it is unchanged
    across its calls, so a pod still draining when that file starts and gone by
    the time it finishes would fail an assertion about something else entirely.
    Waiting here keeps the "each file establishes its own precondition" rule
    from being quietly undermined by a neighbour's leftovers.
    """
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if pod not in pod_names():
            return
        time.sleep(RECLAIM_POLL)
    raise RuntimeError(
        f"session pod {pod} was still present {timeout:.0f}s after its session "
        f"was deleted"
    )


def inject_through_control_plane(url: str, session_id: str, payload: str) -> None:
    """Write to a session's workload without going through ``session_write``.

    session-read/AC1 needs new output to appear between two reads. Producing it
    with the ``session_write`` tool would make the read file's increment case
    depend on the other AC's tool; posting to the control plane keeps the two
    files independent, and the setup path is the same one the tool wraps.
    """
    response = httpx.post(
        f"{url}{SESSIONS_PATH}/{session_id}/write",
        json={"payload": payload},
        timeout=60.0,
    )
    assert response.status_code == 200, (
        f"writing to session {session_id} returned {response.status_code}: "
        f"{response.text.strip()}"
    )
