"""End-to-end checks for workload_list / workload_restart / workload_scale / workload_logs."""

from __future__ import annotations

import asyncio
import subprocess
import time

from mcp.shared.exceptions import McpError

from _helpers import (
    assert_destructive_annotation,
    base_url,
    open_session,
    wait_for_healthz,
)

NAMESPACE = "workload-test"
WORKLOAD = "workload-fixture"
CRASHLOOP_WORKLOAD = "crashloop-fixture"
# Must match the echo line in tests/k8s/kind/test-deployment.yaml.
CRASHLOOP_MARKER = "crashloop-fixture: boom before exit"
RESTART_ANNOTATION_PATH = (
    r"{.spec.template.metadata.annotations.kubectl\.kubernetes\.io/restartedAt}"
)


def wait_for_crashloop_restart(timeout: float = 180.0) -> None:
    """Block until the crash-once fixture pod has restarted at least once.

    previous=true logs only exist once the kubelet has restarted the container
    (restartCount >= 1). The fixture crashes exactly once and then stays
    Running, so after the first restart lastState.terminated is pinned to the
    marker-printing instance and this returns for good. The fixture is applied
    minutes before this script runs in CI, so this normally returns
    immediately; the poll is a safety net for scheduling/backoff timing.
    """
    deadline = time.monotonic() + timeout
    last = "<no pod yet>"
    while time.monotonic() < deadline:
        proc = subprocess.run(
            [
                "kubectl",
                "-n",
                NAMESPACE,
                "get",
                "pod",
                "-l",
                f"app={CRASHLOOP_WORKLOAD}",
                "-o",
                "jsonpath={.items[0].status.containerStatuses[0].restartCount}",
            ],
            capture_output=True,
            text=True,
        )
        last = proc.stdout.strip() or proc.stderr.strip()
        if proc.returncode == 0 and proc.stdout.strip().isdigit():
            if int(proc.stdout.strip()) >= 1:
                return
        time.sleep(3)
    raise RuntimeError(
        f"crashloop fixture never reached restartCount >= 1 within {timeout:.0f}s"
        f" (last observation: {last!r})"
    )


def kubectl_jsonpath(jsonpath: str) -> str:
    out = subprocess.check_output(
        [
            "kubectl",
            "-n",
            NAMESPACE,
            "get",
            f"deploy/{WORKLOAD}",
            "-o",
            f"jsonpath={jsonpath}",
        ],
        text=True,
    )
    return out.strip()


def kubectl_wait_rollout() -> None:
    subprocess.run(
        [
            "kubectl",
            "-n",
            NAMESPACE,
            "rollout",
            "status",
            f"deploy/{WORKLOAD}",
            "--timeout=120s",
        ],
        check=True,
    )


async def test_workload_restart_ac2_destructive_hint(session) -> None:
    """AC: workload-restart/AC2 — workload_restart advertises destructiveHint=true.

    Verifies the destructive-operation marking via tools/list metadata only; no
    restart is triggered.
    """
    await assert_destructive_annotation(session, "workload_restart")


async def test_workload_scale_ac3_destructive_hint(session) -> None:
    """AC: workload-scale/AC3 — workload_scale advertises destructiveHint=true.

    Verifies the destructive-operation marking via tools/list metadata only; no
    scale is performed.
    """
    await assert_destructive_annotation(session, "workload_scale")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- namespace_list ---")
        result = await session.call_tool("namespace_list", {})
        assert result.isError is False, result
        payload = result.structuredContent
        items = payload["items"]
        names = [item["name"] for item in items]
        assert NAMESPACE in names, names
        assert "kube-system" in names, names
        active = next(item for item in items if item["name"] == NAMESPACE)
        assert active["phase"] == "Active", active
        print("namespace_list ok:", len(items), "namespaces")

        print("--- workload_list (namespace=workload-test) ---")
        result = await session.call_tool(
            "workload_list",
            {"kind": "Deployment", "namespace": NAMESPACE},
        )
        assert result.isError is False, result
        payload = result.structuredContent
        items = payload.pop("items")
        assert payload == {"kind": "Deployment", "namespace": NAMESPACE}, payload
        names = [item["name"] for item in items]
        assert WORKLOAD in names, names
        print("list ok:", names)

        print("--- workload_list (all namespaces) ---")
        result = await session.call_tool(
            "workload_list",
            {"kind": "Deployment"},
        )
        assert result.isError is False, result
        payload = result.structuredContent
        items = payload.pop("items")
        assert payload == {"kind": "Deployment", "namespace": None}, payload
        pairs = {(i["namespace"], i["name"]) for i in items}
        assert (NAMESPACE, WORKLOAD) in pairs, pairs
        assert ("homelab-k3s-mcp", "homelab-k3s-mcp") in pairs, pairs
        print("list-all ok:", len(items), "items")

        print("--- workload_restart ---")
        result = await session.call_tool(
            "workload_restart",
            {"kind": "Deployment", "namespace": NAMESPACE, "name": WORKLOAD},
        )
        assert result.isError is False, result
        payload = result.structuredContent
        restarted_at = payload.pop("restartedAt")
        assert restarted_at, "restartedAt should be a non-empty timestamp"
        assert payload == {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": WORKLOAD,
        }, payload
        print("workload_restart ok at", restarted_at)

        annotation = kubectl_jsonpath(RESTART_ANNOTATION_PATH)
        print("restartedAt annotation:", annotation)
        assert annotation, "restartedAt annotation missing on resource"
        kubectl_wait_rollout()

        print("--- workload_scale (scale up to 3) ---")
        result = await session.call_tool(
            "workload_scale",
            {
                "kind": "Deployment",
                "namespace": NAMESPACE,
                "name": WORKLOAD,
                "replicas": 3,
            },
        )
        assert result.isError is False, result
        assert result.structuredContent == {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": WORKLOAD,
            "replicas": 3,
        }, result.structuredContent
        print("workload_scale up ok")

        replicas = kubectl_jsonpath("{.spec.replicas}")
        print("spec.replicas after scale up:", replicas)
        assert replicas == "3", f"expected 3 replicas, got {replicas!r}"
        kubectl_wait_rollout()

        print("--- workload_scale (scale back to 1) ---")
        result = await session.call_tool(
            "workload_scale",
            {
                "kind": "Deployment",
                "namespace": NAMESPACE,
                "name": WORKLOAD,
                "replicas": 1,
            },
        )
        assert result.isError is False, result
        assert result.structuredContent == {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": WORKLOAD,
            "replicas": 1,
        }, result.structuredContent
        print("workload_scale down ok")

        replicas = kubectl_jsonpath("{.spec.replicas}")
        print("spec.replicas after scale down:", replicas)
        assert replicas == "1", f"expected 1 replica, got {replicas!r}"
        kubectl_wait_rollout()

        print("--- workload_scale (DaemonSet rejected) ---")
        result = await session.call_tool(
            "workload_scale",
            {
                "kind": "DaemonSet",
                "namespace": NAMESPACE,
                "name": WORKLOAD,
                "replicas": 1,
            },
        )
        assert result.isError, result
        text = result.content[0].text
        assert "DaemonSet does not have replicas" in text, text
        print("workload_scale daemonset rejection ok")

        # --- workload_logs ---
        # The main fixture runs the `pause` image, which emits no log output.
        # These first checks verify the plumbing: selector resolution, pod
        # lookup, the pods/log RBAC binding, option propagation, and the
        # empty-output placeholder. Actual log *content* (previous=true after
        # a crash loop) is covered below against the crashloop-fixture.
        print("--- workload_logs (defaults, empty output) ---")
        result = await session.call_tool(
            "workload_logs",
            {"kind": "Deployment", "namespace": NAMESPACE, "name": WORKLOAD},
        )
        assert result.isError is False, result
        payload = result.structuredContent
        pod_name = payload.pop("pod")
        assert pod_name.startswith(f"{WORKLOAD}-"), pod_name
        assert payload == {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": WORKLOAD,
            "container": None,
            "tailLines": 200,
            "previous": False,
            "timestamps": False,
            "sinceSeconds": None,
            "logs": "",
        }, payload
        assert result.content[0].text == "(no log output)", result.content[0].text
        print("workload_logs defaults ok, pod:", pod_name)

        print("--- workload_logs (explicit options) ---")
        result = await session.call_tool(
            "workload_logs",
            {
                "kind": "Deployment",
                "namespace": NAMESPACE,
                "name": WORKLOAD,
                "container": "pause",
                "tail_lines": 10,
                "timestamps": True,
                "since_seconds": 60,
            },
        )
        assert result.isError is False, result
        payload = result.structuredContent
        assert payload["container"] == "pause", payload
        assert payload["tailLines"] == 10, payload
        assert payload["timestamps"] is True, payload
        assert payload["sinceSeconds"] == 60, payload
        print("workload_logs options ok")

        print("--- workload_logs (tail_lines over max rejected) ---")
        # Argument validation errors come back as JSON-RPC errors, which the
        # SDK surfaces as McpError exceptions rather than tool result objects.
        try:
            await session.call_tool(
                "workload_logs",
                {
                    "kind": "Deployment",
                    "namespace": NAMESPACE,
                    "name": WORKLOAD,
                    "tail_lines": 999_999,
                },
            )
        except McpError as exc:
            assert "tail_lines" in str(exc), exc
            print("workload_logs tail_lines rejection ok")
        else:
            raise AssertionError("expected McpError for tail_lines over max")

        print("--- workload_logs (previous=true, crash-loop log content) ---")
        # test-workload-logs.md S3 / AC3: after a crash, previous=true must
        # return the terminated instance's actual log content. The fixture
        # prints a known marker, exits non-zero exactly once, then stays
        # Running — pinning lastState.terminated to the marker instance.
        wait_for_crashloop_restart()
        result = await session.call_tool(
            "workload_logs",
            {
                "kind": "Deployment",
                "namespace": NAMESPACE,
                "name": CRASHLOOP_WORKLOAD,
                "previous": True,
            },
        )
        assert result.isError is False, result
        payload = result.structuredContent
        pod_name = payload["pod"]
        assert pod_name.startswith(f"{CRASHLOOP_WORKLOAD}-"), pod_name
        assert payload["previous"] is True, payload
        assert CRASHLOOP_MARKER in payload["logs"], payload["logs"]
        assert CRASHLOOP_MARKER in result.content[0].text, result.content[0].text
        print("workload_logs previous content ok, pod:", pod_name)

        print("--- workload_logs (missing workload returns tool error) ---")
        result = await session.call_tool(
            "workload_logs",
            {
                "kind": "Deployment",
                "namespace": NAMESPACE,
                "name": "does-not-exist",
            },
        )
        assert result.isError, result
        print("workload_logs missing-workload rejection ok")

        print("--- workload_restart destructiveHint (AC: workload-restart/AC2) ---")
        await test_workload_restart_ac2_destructive_hint(session)
        print("workload_restart destructiveHint ok")

        print("--- workload_scale destructiveHint (AC: workload-scale/AC3) ---")
        await test_workload_scale_ac3_destructive_hint(session)
        print("workload_scale destructiveHint ok")


if __name__ == "__main__":
    asyncio.run(run())
