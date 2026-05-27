"""Smoke checks: /healthz, /readyz, and tools/list via the MCP SDK."""

from __future__ import annotations

import asyncio

from _helpers import base_url, get_json, open_session, wait_for_healthz

EXPECTED_TOOLS = {
    "ping",
    "namespace_list",
    "workload_list",
    "workload_restart",
    "workload_scale",
    "workload_logs",
    "pod_describe",
    "dear_baby_reset_onboarding",
    "grafana_token",
}


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    healthz = get_json(url, "/healthz")
    assert healthz.get("status") == "ok", f"unexpected /healthz: {healthz!r}"
    print("healthz ok:", healthz)

    readyz = get_json(url, "/readyz")
    assert readyz.get("status") == "ready", f"unexpected /readyz: {readyz!r}"
    print("readyz ok:", readyz)

    async with open_session(url) as session:
        tools = await session.list_tools()
        names = {tool.name for tool in tools.tools}
        missing = EXPECTED_TOOLS - names
        assert not missing, (
            f"missing tools: {sorted(missing)} (got {sorted(names)})"
        )
        print("tools/list ok:", sorted(names))


if __name__ == "__main__":
    asyncio.run(run())
