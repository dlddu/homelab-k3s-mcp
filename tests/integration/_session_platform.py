"""session-list 파일들이 공유하는 제어면 표면 (매칭 단위가 아니다).

`_` 접두라 `run_all.py::matching_unit_paths()` 가 걸러내므로 AC를 주검증하지 않고
`검증 AC:` 선언도 갖지 않는다 — `_workload.py`·`_auth_variant.py` 와 같은 자리다.

These helpers drive ``tests/k8s/kind/session-platform.yaml``: the **real**
session-platform control plane, running its published image in the kind
harness. Sessions are seeded straight into the control plane's own state
store rather than created through its product API, because creating one
provisions a data plane pod and only the 60-minute idle path or a CRIU
checkpoint can put a session into ``idle``/``snapshot`` -- neither of which is
what session-list/AC1..AC3 are about. Seeding the real store is the same move
the harness already makes for MinIO (the ``minio-seed`` Job), and it leaves the
control plane's own API, decoding and normalization on the path under test.

The store's representation is not invented here. session-platform's
``control-plane/internal/adapter/configmap`` keeps one ConfigMap per session,
named ``session-<id>``, labelled with the control plane's ownership labels, and
carrying the ``session.Session`` JSON under a single ``session`` data key. The
field names below are that struct's JSON tags; ``List`` reads only these
ConfigMaps and never looks at pods, which is why a seeded session is a complete
one as far as listing is concerned.
"""

from __future__ import annotations

import json
import subprocess

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
