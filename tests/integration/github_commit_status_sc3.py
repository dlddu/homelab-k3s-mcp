"""Deployed-server e2e for github-commit-status/AC3 (입력 검증이 PocketAide 보다 앞선다).

검증 시나리오: test-github-commit-status.md#시나리오 3
실행 대상: primary
병렬 레인: github-app

각 케이스는 유효한 한 벌에서 한 필드만 비튼다. 거부된 호출이 요청을 남기지 않았다는 것은
primary 사용자의 커밋 상태 요청 목록에 이 실행의 마커가 없다는 것으로 잰다 — 다른 레인의
요청이 섞여도 마커로 가른다. 마지막의 유효한 한 벌이 목록에 **나타나는 것**이 양성 대조다.
"""

from __future__ import annotations

import asyncio
import uuid
from typing import Any

from mcp import ClientSession

from _approval_gate import approval_url, commit_status_requests, reject_commit_status_request
from _helpers import base_url, open_session, wait_for_healthz

TOOL = "github_commit_status_request"


def valid(marker: str) -> dict[str, Any]:
    return {
        "repository": "dlddu/homelab-k3s-mcp",
        "sha": "0" * 40,
        "state": "success",
        "context": "homelab-k3s-mcp/e2e",
        "requester": f"e2e 하네스 {marker}",
    }


def cases(marker: str) -> list[tuple[str, dict[str, Any], str]]:
    def without(field: str) -> dict[str, Any]:
        arguments = valid(marker)
        arguments.pop(field)
        return arguments

    def replacing(field: str, value: str) -> dict[str, Any]:
        return {**valid(marker), field: value}

    return [
        ("repository 누락", without("repository"), "repository is required"),
        ("owner 없는 repository", replacing("repository", "homelab-k3s-mcp"), "must be owner/name"),
        ("축약 sha", replacing("sha", "abc1234"), "40-character hex commit sha"),
        ("허용되지 않는 state", replacing("state", "ok"), 'state "ok" must be one of'),
        ("context 누락", without("context"), "context is required"),
        ("141자 description", replacing("description", "d" * 141), "above the 140 limit"),
        ("http 아닌 target_url", replacing("target_url", "ftp://x"), "absolute http(s) URL"),
        ("requester 누락", without("requester"), "requester is required"),
        ("201자 requester", replacing("requester", "가" * 201), "requester must be 200 characters or fewer"),
    ]


def refusal_text(result) -> str:
    assert result.isError is True, f"call did not refuse: {result}"
    block = result.content[0]
    assert block.type == "text", block
    return block.text


def marked(url: str, marker: str) -> list[dict]:
    return [r for r in commit_status_requests(url) if marker in r["requester"]]


async def test_github_commit_status_ac3_invalid_input_never_reaches_pocket_aide(
    session: ClientSession,
) -> None:
    marker = uuid.uuid4().hex[:12]
    for label, arguments, expected in cases(marker):
        text = refusal_text(await session.call_tool(TOOL, arguments))
        assert text.startswith("invalid commit status request: "), f"{label}: {text!r}"
        assert expected in text, f"{label}: refusal text = {text!r}, expected {expected!r}"

    with approval_url() as url:
        assert marked(url, marker) == [], f"a refused call left a request: {marked(url, marker)}"

        accepted = await session.call_tool(TOOL, valid(marker))
        assert accepted.isError is False, accepted
        rows = marked(url, marker)
        assert len(rows) == 1, f"the valid call left {rows} — the zero assertion would hold vacuously"
        reject_commit_status_request(url, rows[0]["id"])


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github-commit-status/AC3 ---")
        await test_github_commit_status_ac3_invalid_input_never_reaches_pocket_aide(session)
        print("ok: github-commit-status/AC3")


if __name__ == "__main__":
    asyncio.run(run())
