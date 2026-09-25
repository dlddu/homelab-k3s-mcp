"""승인한 상태와 실행할 상태가 같아야 한다 — 승인과 실행 사이에 대상이 움직이면 거부된다.

검증 시나리오: test-approval-gate.md#시나리오 6
실행 대상: primary
병렬 레인: gate-screens

다섯 경로를 한 파일에서 태운다 — `resource_patch` · `resource_update(subresource=scale)` ·
`kind=Secret` 의 `resource_get` · `resource_exec` · `resource_create`. 앞 넷은 게이트가
승인 전에 읽어 둔 `resourceVersion`/`uid` 를 실행 직전에 재확인하는 공통 경로
(``internal/mcp/gate.go::confirmTargetUnchanged``)를 타고, 생성만 그 경로를 타지 않는다.

댄스는 늘 같다: 도구 호출을 ``asyncio.create_task`` 로 띄워 PENDING 을 잡고, **승인하기
전에** kubectl 로 대상을 외부에서 움직인 뒤 승인한다. 그 순서가 이 시나리오 자체다 —
승인 시점의 상태와 실행 시점의 상태를 갈라 놓는 창은 사람이 판정을 누르기 전의 창이다.

Go 단위(``internal/mcp/update_precondition_test.go`` · ``internal/k8s/update_precondition_test.go``)가
갱신 호출 횟수와 조건부 PUT 의 늦은 409 를 이미 단언한다. 이 파일이 더하는 것은 **실물
apiserver 와 실물 gatekeeper 로 같은 계약이 성립하는가**이고, 그래서 여기서는 호출 횟수가
아니라 **관측 가능한 결과**(대상 무변경 · Secret 값 미반환 · 재승인 요구 문면)를 단언한다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from mcp.shared.exceptions import McpError

from _gatekeeper import decide, gatekeeper_url, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"

TARGET_MAP = "gk-ac6-configmap"
TARGET_DEPLOYMENT = "gk-ac6-scale"
TARGET_SECRET = "gk-ac6-secret"
TARGET_POD = "gk-ac6-exec"
TARGET_CREATE = "gk-ac6-created"

SECRET_VALUE = "gk-ac6-value-7d2ae14"

#: 재확인이 걸렸을 때 게이트가 내는 문면(``gate.go::confirmTargetUnchanged``).
CHANGED_MARK = "changed after it was approved"
REAPPROVAL_MARK = "this needs a new one"


def _kubectl(*args: str, check: bool = True) -> str:
    return subprocess.run(
        ["kubectl", *args],
        text=True,
        check=check,
        capture_output=True,
    ).stdout


def _apply(manifest: dict) -> None:
    subprocess.run(
        ["kubectl", "apply", "-f", "-"],
        input=json.dumps(manifest),
        text=True,
        check=True,
        capture_output=True,
    )


def _delete(kind: str, name: str) -> None:
    subprocess.run(
        ["kubectl", "-n", NAMESPACE, "delete", kind, name, "--ignore-not-found",
         "--wait=true", "--grace-period=0", "--force"],
        text=True,
        check=False,
        capture_output=True,
    )


def _get(kind: str, name: str, path: str) -> str:
    return _kubectl("-n", NAMESPACE, "get", kind, name, "-o", f"jsonpath={{{path}}}").strip()


def _exists(kind: str, name: str) -> bool:
    return (
        subprocess.run(
            ["kubectl", "-n", NAMESPACE, "get", kind, name],
            capture_output=True,
        ).returncode
        == 0
    )


def _pod_manifest() -> dict:
    return {
        "apiVersion": "v1",
        "kind": "Pod",
        "metadata": {"name": TARGET_POD, "namespace": NAMESPACE},
        "spec": {
            "restartPolicy": "Never",
            "terminationGracePeriodSeconds": 0,
            "containers": [
                {
                    "name": "shell",
                    "image": "busybox:1.36",
                    "command": ["sh", "-c", "sleep 3600"],
                }
            ],
        },
    }


def _seed() -> None:
    _apply(
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {"name": TARGET_MAP, "namespace": NAMESPACE},
            "data": {"key": "initial"},
        }
    )
    _apply(
        {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {"name": TARGET_DEPLOYMENT, "namespace": NAMESPACE},
            "spec": {
                "replicas": 1,
                "selector": {"matchLabels": {"app.kubernetes.io/name": TARGET_DEPLOYMENT}},
                "template": {
                    "metadata": {"labels": {"app.kubernetes.io/name": TARGET_DEPLOYMENT}},
                    "spec": {
                        "terminationGracePeriodSeconds": 0,
                        "containers": [
                            {"name": "quiet", "image": "registry.k8s.io/pause:3.9"}
                        ],
                    },
                },
            },
        }
    )
    _apply(
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {"name": TARGET_SECRET, "namespace": NAMESPACE},
            "stringData": {"token": SECRET_VALUE},
        }
    )
    _delete("configmap", TARGET_CREATE)
    _delete("pod", TARGET_POD)
    _apply(_pod_manifest())


async def _refused_after_change(session, gate, tool: str, args: dict, marker: str, disturb) -> str:
    """도구를 띄우고 PENDING 을 잡은 뒤 **승인 전에** 대상을 흔들고 승인한다."""
    task = asyncio.create_task(session.call_tool(tool, args))
    row = await wait_for_pending(gate, marker)
    disturb()
    await decide(gate, row["id"], "APPROVED")
    try:
        await task
    except McpError as exc:
        text = str(exc)
        assert CHANGED_MARK in text, f"{tool} 의 거부 문면이 재확인 실패가 아니다: {text}"
        assert REAPPROVAL_MARK in text, f"{tool} 거부가 재승인 필요를 알리지 않는다: {text}"
        return text
    raise AssertionError(f"{tool} 이 승인 뒤 바뀐 대상에 대해 실행됐다")


async def test_patch_is_refused_when_the_target_moved(session, gate) -> None:
    before = _get("configmap", TARGET_MAP, ".metadata.resourceVersion")

    def disturb() -> None:
        _kubectl(
            "-n", NAMESPACE, "patch", "configmap", TARGET_MAP,
            "--type=merge", "-p", json.dumps({"data": {"key": "moved-by-someone-else"}}),
        )

    await _refused_after_change(
        session, gate, "resource_patch",
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "name": TARGET_MAP,
            "patchType": "merge",
            "patch": {"data": {"key": "patched-by-the-tool"}},
        },
        TARGET_MAP, disturb,
    )
    after = _get("configmap", TARGET_MAP, ".metadata.resourceVersion")
    assert after != before, "외부 변경 자체가 일어나지 않았다 — 이 케이스는 헛통과다"
    assert _get("configmap", TARGET_MAP, ".data.key") == "moved-by-someone-else", (
        "거부된 patch 가 그래도 대상을 바꿨다"
    )


async def test_scale_is_refused_when_the_target_moved(session, gate) -> None:
    def disturb() -> None:
        _kubectl(
            "-n", NAMESPACE, "annotate", "deployment", TARGET_DEPLOYMENT,
            "gk-ac6/moved=yes", "--overwrite",
        )

    await _refused_after_change(
        session, gate, "resource_update",
        {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": TARGET_DEPLOYMENT,
            "subresource": "scale",
            "replicas": 3,
        },
        TARGET_DEPLOYMENT, disturb,
    )
    assert _get("deployment", TARGET_DEPLOYMENT, ".spec.replicas") == "1", (
        "거부된 scale 이 그래도 레플리카를 바꿨다"
    )


async def test_secret_read_is_refused_and_returns_nothing(session, gate) -> None:
    def disturb() -> None:
        _kubectl(
            "-n", NAMESPACE, "annotate", "secret", TARGET_SECRET,
            "gk-ac6/moved=yes", "--overwrite",
        )

    text = await _refused_after_change(
        session, gate, "resource_get",
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "namespace": NAMESPACE,
            "name": TARGET_SECRET,
        },
        TARGET_SECRET, disturb,
    )
    assert SECRET_VALUE not in text, "거부 문면이 Secret 값을 실어 보냈다"
    assert "dmFsdWU" not in text, "거부 문면이 인코딩된 Secret 값을 실어 보냈다"


async def test_exec_is_refused_after_the_pod_was_recreated(session, gate) -> None:
    """같은 이름·다른 uid — 재확인은 resourceVersion 뿐 아니라 uid 도 본다."""
    original_uid = _get("pod", TARGET_POD, ".metadata.uid")

    def disturb() -> None:
        _delete("pod", TARGET_POD)
        _apply(_pod_manifest())
        # 재확인이 NotFound 를 만나면 「다시 읽을 수 없었다」는 다른 문면이 나온다.
        # 이 케이스가 재려는 것은 uid 불일치이므로, 같은 이름의 새 객체가 실제로
        # 선 뒤에 승인한다.
        for _ in range(60):
            if _exists("pod", TARGET_POD):
                return
        raise AssertionError("재생성한 파드가 나타나지 않았다")

    await _refused_after_change(
        session, gate, "resource_exec",
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "namespace": NAMESPACE,
            "name": TARGET_POD,
            "command": ["echo", "should-not-run"],
        },
        TARGET_POD, disturb,
    )
    assert _get("pod", TARGET_POD, ".metadata.uid") != original_uid, (
        "파드가 실제로 재생성되지 않았다 — 이 케이스는 헛통과다"
    )


async def test_create_is_refused_by_the_apiserver_not_by_reconfirmation(session, gate) -> None:
    """생성 경로는 재확인이 없다 — 409 가 그 자리를 대신하고, context 에 버전이 없다."""
    task = asyncio.create_task(
        session.call_tool(
            "resource_create",
            {
                "manifest": {
                    "apiVersion": "v1",
                    "kind": "ConfigMap",
                    "metadata": {"name": TARGET_CREATE, "namespace": NAMESPACE},
                    "data": {"key": "by-the-tool"},
                }
            },
        )
    )
    row = await wait_for_pending(gate, TARGET_CREATE)
    context = row.get("context", "")
    assert "target resourceVersion" not in context, (
        f"생성 승인 요청이 대상의 resourceVersion 을 실었다: {context}"
    )
    _apply(
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {"name": TARGET_CREATE, "namespace": NAMESPACE},
            "data": {"key": "by-someone-else"},
        }
    )
    await decide(gate, row["id"], "APPROVED")
    result = await task

    assert result.isError is True, f"이미 존재하는 이름의 생성이 성공으로 보고됐다: {result}"
    payload = result.structuredContent
    assert payload["created"] == [], payload
    failed = payload["failed"]
    assert failed and failed["name"] == TARGET_CREATE, payload
    assert "409" in failed["error"] or "already exists" in failed["error"], failed
    assert _get("configmap", TARGET_CREATE, ".data.key") == "by-someone-else", (
        "거부된 생성이 기존 객체를 덮어썼다"
    )


async def run() -> None:
    url = base_url()
    _seed()
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- approval-gate/시나리오 6 (patch) ---")
            await test_patch_is_refused_when_the_target_moved(session, gate)
            print("--- approval-gate/시나리오 6 (update/scale) ---")
            await test_scale_is_refused_when_the_target_moved(session, gate)
            print("--- approval-gate/시나리오 6 (Secret get) ---")
            await test_secret_read_is_refused_and_returns_nothing(session, gate)
            print("--- approval-gate/시나리오 6 (exec) ---")
            await test_exec_is_refused_after_the_pod_was_recreated(session, gate)
            print("--- approval-gate/시나리오 6 (create — 409 경로) ---")
            await test_create_is_refused_by_the_apiserver_not_by_reconfirmation(session, gate)
    print("ok: test-approval-gate.md#시나리오 6")


if __name__ == "__main__":
    asyncio.run(run())
