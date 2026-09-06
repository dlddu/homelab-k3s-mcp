"""Deployed-server e2e for session-read/AC3 (대상 부재·잘못된 커서의 명확한 처리).

검증 AC: session-read/AC3
실행 대상: primary

AC3은 **두 실패가 서로 구분된다**는 것과 **어느 쪽도 세션을 건드리지 않는다**는 것, 둘을 함께
요구한다. 그래서 이 파일은 세 가지를 단정한다 — 없는 id 는 not-found 계열 **도구 에러**,
잘못된 커서는 인자 검증 계열 **프로토콜 에러**(SDK 가 `McpError` 로 올린다), 그리고 두 호출
뒤에도 실재하는 세션의 상태·`lastAccess` 가 그대로다.

**두 에러의 층이 다른 것은 의도된 설계다.** 없는 세션은 제어면이 404 로 답해야 알 수 있으므로
`internal/sessionplatform` 이 그것을 `kindNotFound` 로 올려 도구 에러(`isError`)가 되고, 음수·
비정수 커서는 `internal/mcp` 가 **HTTP 에 닿기 전에** `-32602` 로 거부한다. 후자가 AC 의
「세션 상태는 바뀌지 않는다」를 약속이 아니라 **구조**로 만든다: 요청이 0건이면 건드릴 방법이
없다. 그래서 이 파일은 두 층을 뭉뚱그리지 않고 각각의 형태로 단정한다.

기준선은 이 파일이 스스로 세운다(`seed_sessions`) — 다른 파일이 무엇을 남겨 놨든 무관하다.
"""

from __future__ import annotations

import asyncio

from mcp.shared.exceptions import McpError

from _helpers import base_url, open_session, wait_for_healthz
from _session_platform import pod_names, seed_sessions, session_payload, sessions_from

#: 실재하는 대상 세션. AC3의 "호출 후 대상 세션의 상태·lastAccess가 변하지 않는다"를
#: 관측할 기준선이라, 두 실패 호출과 무관하게 존재하기만 하면 된다.
TARGET_SESSION = session_payload(
    session_id="e2e-read-ac3-target",
    name="e2e read target",
    workload_type="shell",
    state="active",
    pod="session-e2e-read-ac3-target",
    created_at="2026-09-01T00:00:00Z",
    last_access="2026-09-03T11:00:00Z",
)

#: 제어면에 없는 id. 시드가 만드는 유일한 세션과 겹치지 않는다.
MISSING_SESSION_ID = "e2e-read-ac3-missing"


def _target_from(sessions: list[dict]) -> dict:
    """Pull the seeded target out of a listing, failing loudly if it vanished."""
    by_id = {item["id"]: item for item in sessions}
    assert TARGET_SESSION["id"] in by_id, (
        f"seeded target {TARGET_SESSION['id']} is not in the inventory: {sessions}"
    )
    return by_id[TARGET_SESSION["id"]]


async def test_session_read_ac3_missing_session_is_not_found(session) -> None:
    """AC: session-read/AC3 — an unknown id comes back as a not-found tool error.

    The control plane answers 404 for a session it does not hold, and the client
    surfaces that as its own error kind. Asserted as a normal tool result
    carrying ``isError`` (not a transport failure), and asserted to be
    *distinguishable*: the text must not read like the unconfigured refusal
    (session-read/AC4) nor like the bad-cursor rejection below, or a caller
    cannot tell "no such session" from "the integration is off".
    """
    result = await session.call_tool("session_read", {"id": MISSING_SESSION_ID})

    assert result.isError is True, f"unknown id did not fail: {result}"
    assert result.content, result
    block = result.content[0]
    assert block.type == "text", block
    assert "session platform not found" in block.text, block.text
    assert MISSING_SESSION_ID in block.text, block.text
    # Distinct from the other two failure classes a caller can hit.
    assert "unavailable" not in block.text, block.text
    assert "invalid argument" not in block.text, block.text
    print("not-found ok:", block.text)


async def test_session_read_ac3_bad_cursor_is_an_argument_error(session) -> None:
    """AC: session-read/AC3 — a negative or non-integer cursor is an argument error.

    Both are rejected by the tool layer before any control plane request, so the
    SDK raises ``McpError`` (JSON-RPC ``-32602``) rather than returning a tool
    result — the same shape ``pod_describe_ac2.py`` asserts for its mutually
    exclusive targeting arguments. The id is a *real* session here, so the
    rejection is provably about the cursor and not about the target.
    """
    bad_cursors = (
        (-1, "offset must be >= 0"),
        (1.5, "offset must be an integer"),
    )
    for offset, expected in bad_cursors:
        try:
            await session.call_tool(
                "session_read", {"id": TARGET_SESSION["id"], "offset": offset}
            )
        except McpError as exc:
            assert expected in str(exc), f"offset={offset!r}: {exc}"
            print(f"bad cursor rejected ok: offset={offset!r} -> {exc}")
        else:
            raise AssertionError(f"expected McpError for offset={offset!r}")


async def test_session_read_ac3_target_is_untouched(session, before: dict) -> None:
    """AC: session-read/AC3 — neither failure moved the session.

    ``before`` is the target as it stood ahead of the two rejected calls. A read
    that had reached the control plane would have activated the session and
    refreshed ``lastAccess``, so both fields staying identical is the observable
    form of "어느 경우에도 세션 상태는 바뀌지 않는다". The pod set is compared
    too: a restore would have provisioned one, and against the real control
    plane that is a discriminator rather than a vacuous assertion.
    """
    result = await session.call_tool("session_list", {})
    after = _target_from(sessions_from(result))

    assert after["state"] == before["state"], (before, after)
    assert after["lastAccess"] == before["lastAccess"], (before, after)
    assert after["pod"] == before["pod"], (before, after)
    print("target untouched ok:", after["state"], after["lastAccess"])


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    seed_sessions([TARGET_SESSION])
    pods_before = pod_names()

    async with open_session(url) as session:
        print("--- session-read/AC3 ---")
        listing = await session.call_tool("session_list", {})
        before = _target_from(sessions_from(listing))

        await test_session_read_ac3_missing_session_is_not_found(session)
        await test_session_read_ac3_bad_cursor_is_an_argument_error(session)
        await test_session_read_ac3_target_is_untouched(session, before)

        assert pod_names() == pods_before, (
            "a rejected read provisioned or reclaimed a pod: "
            f"{pods_before} -> {pod_names()}"
        )
        print("ok: session-read/AC3")


if __name__ == "__main__":
    asyncio.run(run())
