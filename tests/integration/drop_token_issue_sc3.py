"""drop_token_issue: 범위 밖 인자는 drop 에 닿기 전에 거부된다 (e2e).

검증 시나리오: test-drop-token-issue.md#시나리오 3
실행 대상: primary

「닿지 않았다」는 drop 의 로그에서 잰다 — drop 은 발급할 때마다 ``service account token issued`` 한 줄을
남기므로, 거부 묶음 전후로 그 줄 수가 같아야 하고 마지막의 유효한 호출 하나로만 늘어야 한다.
"""

from __future__ import annotations

import asyncio
import subprocess
import time

from mcp import ClientSession
from mcp.shared.exceptions import McpError

from _drop import NAMESPACE, TOOL, issue
from _helpers import base_url, open_session, wait_for_healthz

ISSUED_MARK = "service account token issued"

CASES = [
    ({"valid_minutes": 0}, "valid_minutes must be between 1 and 1440"),
    ({"valid_minutes": 1441}, "valid_minutes must be between 1 and 1440"),
    ({"valid_minutes": "60"}, "valid_minutes must be an integer"),
    ({"max_days": 0}, "max_days must be between 1 and 30"),
    ({"max_days": 31}, "max_days must be between 1 and 30"),
    ({"label": "가" * 41}, "label must be 40 characters or fewer"),
]


def issued_count() -> int:
    log = subprocess.check_output(
        ["kubectl", "-n", NAMESPACE, "logs", "deploy/drop", "--tail=-1"], text=True, timeout=60
    )
    return sum(ISSUED_MARK in line for line in log.splitlines())


def wait_for_count(want: int, budget: float = 15.0) -> int:
    deadline = time.monotonic() + budget
    got = issued_count()
    while got < want and time.monotonic() < deadline:
        time.sleep(1)
        got = issued_count()
    return got


async def test_drop_token_issue_sc3_bounds_refused_before_drop(session: ClientSession) -> None:
    before = issued_count()
    for arguments, expected in CASES:
        try:
            result = await session.call_tool(TOOL, arguments)
        except McpError as exc:
            assert expected in str(exc), f"{arguments}: {exc}"
        else:
            raise AssertionError(f"{arguments} was not refused: {result}")
    assert issued_count() == before, "a refused call still reached drop"

    await issue(session, {"valid_minutes": 1440, "max_days": 30, "label": "가" * 40})
    assert wait_for_count(before + 1) == before + 1, "the call at the limits did not reach drop exactly once"

    survivor = await session.call_tool("ping", {})
    assert survivor.isError is False and survivor.content[0].text == "pong", survivor


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url, timeout=60.0) as session:
        print("--- bounds refused before drop (AC: drop-token-issue/AC4) ---")
        await test_drop_token_issue_sc3_bounds_refused_before_drop(session)
        print("ok: drop-token-issue 시나리오 3")


if __name__ == "__main__":
    asyncio.run(run())
