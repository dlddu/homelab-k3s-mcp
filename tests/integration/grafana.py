"""End-to-end checks for grafana_token against the mock."""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz


def parse_env_resource(result) -> tuple[str, str]:
    """Extract (env_text, mime_type) from a tool result's embedded resource."""
    assert result.content, result
    block = result.content[0]
    assert block.type == "resource", block
    resource = block.resource
    return resource.text, resource.mimeType


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- grafana_token ---")
        result = await session.call_tool("grafana_token", {})
        assert result.isError is False, result
        assert result.structuredContent is None, result.structuredContent
        env_text, mime = parse_env_resource(result)
        assert mime == "text/plain", mime
        assert "# token expires" in env_text, env_text
        for key in (
            "GRAFANA_METRICS_URL=",
            "GRAFANA_METRICS_USER=",
            "GRAFANA_LOGS_URL=",
            "GRAFANA_LOGS_USER=",
            "GRAFANA_TOKEN=glc_mock_",
        ):
            assert key in env_text, f"missing {key!r} in:\n{env_text}"
        print("ok ->", sorted(line.split("=", 1)[0] for line in env_text.splitlines() if "=" in line))


if __name__ == "__main__":
    asyncio.run(run())
