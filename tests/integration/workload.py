"""End-to-end checks for workload_list / workload_restart / workload_scale."""

from __future__ import annotations

import asyncio
import subprocess

from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"
WORKLOAD = "workload-fixture"
RESTART_ANNOTATION_PATH = (
    r"{.spec.template.metadata.annotations.kubectl\.kubernetes\.io/restartedAt}"
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


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- workload_list (namespace=workload-test) ---")
        result = await session.call_tool(
            "workload_list",
            {"kind": "Deployment", "namespace": NAMESPACE},
        )
        assert not result.isError, result
        payload = result.structuredContent
        assert payload["kind"] == "Deployment", payload
        assert payload["namespace"] == NAMESPACE, payload
        names = [item["name"] for item in payload["items"]]
        assert WORKLOAD in names, names
        print("list ok:", names)

        print("--- workload_list (all namespaces) ---")
        result = await session.call_tool(
            "workload_list",
            {"kind": "Deployment"},
        )
        assert not result.isError, result
        items = result.structuredContent["items"]
        pairs = {(i["namespace"], i["name"]) for i in items}
        assert (NAMESPACE, WORKLOAD) in pairs, pairs
        assert ("homelab-k3s-mcp", "homelab-k3s-mcp") in pairs, pairs
        print("list-all ok:", len(items), "items")

        print("--- workload_restart ---")
        result = await session.call_tool(
            "workload_restart",
            {"kind": "Deployment", "namespace": NAMESPACE, "name": WORKLOAD},
        )
        assert not result.isError, result
        payload = result.structuredContent
        assert payload["name"] == WORKLOAD, payload
        assert payload["restartedAt"], payload
        print("workload_restart ok at", payload["restartedAt"])

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
        assert not result.isError, result
        payload = result.structuredContent
        assert payload["name"] == WORKLOAD, payload
        assert payload["replicas"] == 3, payload
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
        assert not result.isError, result
        assert result.structuredContent["replicas"] == 1, result
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


if __name__ == "__main__":
    asyncio.run(run())
