"""Smoke checks against the primary kind deployment: /healthz, /readyz, tools/list.

Per-AC case names + docstrings declare the AC they verify (registry rule 3);
``docs/doc-tracker.md`` is the AC<->case mapping SSOT.
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _helpers import base_url, get_json, open_session, wait_for_healthz

# The full tool surface this deployment advertises. run() asserts it as a shared
# precondition for the cases below rather than as an AC case of its own: every
# integration IS configured here, so a healthy tools/list on this deployment does
# not demonstrate platform-auth-safety/AC5 (server-level graceful degradation).
# AC5 is only observable where credentials are absent, so its dedicated case
# lives in no_config.py, against the credential-less deployment variant.
EXPECTED_TOOLS = {
    "ping",
    "namespace_list",
    "workload_list",
    "workload_restart",
    "workload_scale",
    "workload_logs",
    "pod_describe",
    "dear_baby_reset_user",
    "grafana_token",
}


def test_platform_auth_safety_ac6_health_readiness(url: str) -> None:
    """AC: platform-auth-safety/AC6 — liveness and readiness probes report state.

    Asserts the two probe paths the orchestrator is pointed at (see the
    livenessProbe/readinessProbe/startupProbe in k8s/deployment.yaml) answer 200
    with their own status vocabulary on a healthy server: ``/healthz`` reports
    ``status=ok`` (liveness) and ``/readyz`` reports ``status=ready``
    (readiness). ``get_json`` raises on any non-2xx, so a probe path that
    disappeared or started erroring fails here.

    The unhealthy side of the criterion ("비정상 상태를 올바르게 반영") is not
    asserted: the deployed server offers no e2e-reachable way to force itself
    unready, and faking it would require a fixture that breaks the very
    deployment the other cases in this run share.
    """
    healthz = get_json(url, "/healthz")
    assert healthz.get("status") == "ok", f"unexpected /healthz: {healthz!r}"

    readyz = get_json(url, "/readyz")
    assert readyz.get("status") == "ready", f"unexpected /readyz: {readyz!r}"


async def test_ping_ac1_always_pong(session: ClientSession) -> None:
    """AC: ping/AC1 — an argument-less call always succeeds with ``pong``.

    Calls the deployed ``ping`` tool with no arguments and asserts the result is
    a non-error MCP tool result whose single content block is the text ``pong``
    exactly — the AC's stated verification method. This promotes the in-process
    assertion in ``internal/server/mcp_test.go`` (``TestPingToolReturnsPong``) to
    the deployed-server e2e layer.
    """
    result = await session.call_tool("ping", {})
    assert result.isError is False, result
    assert result.content, result
    block = result.content[0]
    assert block.type == "text", block
    assert block.text == "pong", block.text


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    print("--- health/readiness probes (AC: platform-auth-safety/AC6) ---")
    test_platform_auth_safety_ac6_health_readiness(url)
    print("probes ok")

    async with open_session(url) as session:
        # Shared precondition (not an AC case): the tools the cases below drive
        # must actually be advertised by this deployment.
        tools = await session.list_tools()
        names = {tool.name for tool in tools.tools}
        missing = EXPECTED_TOOLS - names
        assert not missing, (
            f"missing tools: {sorted(missing)} (got {sorted(names)})"
        )
        print("tools/list ok:", sorted(names))

        print("--- ping (AC: ping/AC1) ---")
        await test_ping_ac1_always_pong(session)
        print("ping ok")


if __name__ == "__main__":
    asyncio.run(run())
