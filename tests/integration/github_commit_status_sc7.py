"""Deployed-server e2e for github-commit-status/AC7 (사람 승인 뒤에만 기록한다).

검증 시나리오: test-github-commit-status.md#시나리오 7
실행 대상: primary
병렬 레인: github-app
"""

from __future__ import annotations

import asyncio
import json
import os
import uuid
from typing import Any

import httpx
from mcp import ClientSession
from mcp.shared.exceptions import McpError

from _approval_gate import approval_url, call_with_verdict, count_requests
from _helpers import base_url, open_session, wait_for_healthz

MOCK_URL = os.environ.get("GITHUB_MOCK_URL", "http://127.0.0.1:8093").rstrip("/")

TOOL = "github_commit_status_create"
INSTALLATION_OWNER = "dlddu"
REPOSITORY = "test"
SHA = "0123456789abcdef0123456789abcdef01234567"
STATUS_PATH = f"/repos/{INSTALLATION_OWNER}/{REPOSITORY}/statuses/{SHA}"


def arguments(context: str = "homelab-k3s-mcp/e2e") -> dict[str, Any]:
    return {
        "repository": REPOSITORY,
        "sha": SHA,
        "state": "success",
        "context": context,
        "description": f"e2e gate {uuid.uuid4().hex[:12]}",
        "target_url": "https://example.invalid/run/7",
    }


def reset_recorded() -> None:
    httpx.delete(f"{MOCK_URL}/_admin/requests", timeout=10.0).raise_for_status()


def recorded() -> list[dict[str, Any]]:
    response = httpx.get(f"{MOCK_URL}/_admin/requests", timeout=10.0)
    response.raise_for_status()
    return response.json()["requests"]


async def test_rejected_write_never_reaches_github(session: ClientSession, gate: str) -> None:
    reset_recorded()
    args = arguments()
    row, task = await call_with_verdict(
        session, gate, TOOL, args, args["description"], status="REJECTED"
    )
    try:
        result = await task
    except McpError as refusal:
        assert "rejected" in str(refusal).lower(), refusal
    else:
        raise AssertionError(f"a rejected write still returned a result: {result}")
    assert recorded() == [], f"a rejected write reached GitHub: {recorded()}"

    shown = row["context"]
    for want in (f"tool: {TOOL}", "action: write a GitHub commit status"):
        assert want in shown, shown
    assert "arguments:\n" in shown, shown
    assert json.loads(shown.split("arguments:\n", 1)[1]) == args, shown


async def test_approved_write_is_recorded(session: ClientSession, gate: str) -> None:
    reset_recorded()
    args = arguments()
    _, task = await call_with_verdict(session, gate, TOOL, args, args["description"])
    result = await task
    assert result.isError is False, result
    writes = [r for r in recorded() if r["method"] == "POST" and r["path"] == STATUS_PATH]
    assert len(writes) == 1, f"the approved write was not recorded once: {recorded()}"
    assert json.loads(writes[0]["body"])["description"] == args["description"], writes


async def test_refused_context_asks_nobody(session: ClientSession, gate: str) -> None:
    reset_recorded()
    args = arguments(context="ci/build")
    result = await session.call_tool(TOOL, args)
    assert result.isError is True, result
    assert "outside the allowed prefixes" in result.content[0].text, result
    assert count_requests(gate, args["description"]) == 0, "a refused context asked for approval"
    assert recorded() == [], f"a refused context reached GitHub: {recorded()}"


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    with approval_url() as gate:
        async with open_session(url) as session:
            print("--- github-commit-status/AC7 ---")
            try:
                await test_rejected_write_never_reaches_github(session, gate)
                await test_approved_write_is_recorded(session, gate)
                await test_refused_context_asks_nobody(session, gate)
            finally:
                reset_recorded()
            print("ok: github-commit-status/AC7")


if __name__ == "__main__":
    asyncio.run(run())
