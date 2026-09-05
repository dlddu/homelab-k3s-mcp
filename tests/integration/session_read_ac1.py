"""Deployed-server e2e for session-read/AC1 (오프셋 커서 읽기).

검증 AC: session-read/AC1
실행 대상: primary

**이 파일이 데이터 플레인을 요구하는 첫 e2e다.** 같은 도구의 AC3·AC4는 에이전트 파드에 닿지
않아 먼저 닫혔지만(없는 id는 제어면 404, 잘못된 커서는 `internal/mcp`가 HTTP 이전에 거부,
미설정 거부는 제어면 자체가 없는 변형), AC1의 커서는 **파드가 실제로 쌓은 바이트**다:
``Service.Read``는 ``activate`` 뒤 ``agent.Read(ctx, sess.Pod, offset)``를 부르고 에이전트
클라이언트는 그 파드 이름을 IP로 해석해 ``:8090/read?offset=N``을 친다. 그래서 이 파일은
``_session_platform.live_shell_session``으로 제품 API를 통해 **실 shell 세션**을 만든다 —
`tests/k8s/kind/session-platform.yaml`이 이번에 ``DATA_PLANE_IMAGE``를 갖춘 이유가 그것이다.

**AC1의 검증 방법 네 절을 전부 단정한다.** 어느 하나를 빼면 커서 규약이 아니라 "읽으면 뭔가
나온다"를 단정하는 것이 된다:

1. ``offset=0``(미지정)이 세션 시작 이후 전체를 반환하고 서버가 ``nextOffset``을 발급한다.
2. 그 커서로 재호출하면 **빈 payload와 동일한 커서**가 돌아온다(새 출력이 없을 때).
3. 새 출력이 쌓인 뒤 같은 커서로 호출하면 **증분만** 돌아온다.
4. 같은 커서를 반복 호출하면 **같은 구간**이 돌아온다 — 읽기는 비파괴적이다.

2와 4가 vacuous하지 않은 이유는 에이전트의 ``scrollback.Since``가 그것을 **구조로** 만들기
때문이다: ``offset >= len``이면 빈 payload와 ``nextOffset = len``을 돌려주고, 그 미만이면
``buf[offset:]``의 사본을 돌려준다(소비하지 않는다). 실 파드가 아니면 두 단정 모두 관측할
대상 자체가 없다.

**증분을 만드는 write는 도구를 경유하지 않는다.** 제어면의 ``POST /api/v1/sessions/{id}/write``에
직접 넣는다(``inject_through_control_plane``) — 여기서 ``session_write``를 쓰면 이 파일의 3번
절이 session-write/AC1의 도구가 도는지에 함께 매달리게 된다. 셋업에서 다른 경로를 경유하는
것은 검증으로 세지 않는다는 규칙 2의 단서가 이 자리를 위한 것이다.

세션은 이 파일이 만들고 이 파일이 지운다 — 다른 파일이 무엇을 남겨 놨든 무관하고, 나갈 때
파드까지 회수된다.
"""

from __future__ import annotations

import asyncio
import time

from _helpers import base_url, open_session, wait_for_healthz
from _session_platform import inject_through_control_plane, live_shell_session

#: 제어면에 만들 세션 이름. 진단 로그에서 어느 파일의 세션인지 드러나야 한다.
SESSION_NAME = "e2e read ac1"

#: 증분으로 관측할 명령과 그 산출물의 마커. 쉘 프롬프트·에코와 섞이지 않도록
#: 이 파일에서만 쓰는 유일 문자열을 고른다. 개행이 있어야 PTY 가 명령을 실행한다.
MARKER = "e2e-read-ac1-increment"
COMMAND = f"printf '%s\\n' {MARKER}\n"

#: 증분이 도착할 때까지의 폴링 예산. write 는 비블로킹이라 반환 시점에 출력이
#: 아직 없을 수 있다(그 사실 자체는 session-write/AC1 이 단정한다).
INCREMENT_TIMEOUT = 30.0
INCREMENT_POLL = 0.5

#: 쉘이 프롬프트를 다 쓸 때까지 기다리는 예산 — "새 출력이 없다"와 "같은 구간이
#: 돌아온다"는 두 전제를 이 파일이 스스로 성립시키기 위한 것이다(``_quiet_end``).
SETTLE_TIMEOUT = 30.0
SETTLE_POLL = 0.5
SETTLE_STABLE_POLLS = 3


async def _read(session, session_id: str, offset: int | None = None) -> dict:
    """One ``session_read`` call, asserted successful, as its structured result."""
    args: dict = {"id": session_id}
    if offset is not None:
        args["offset"] = offset
    result = await session.call_tool("session_read", args)
    assert result.isError is False, result
    return result.structuredContent


async def _quiet_end(session, session_id: str, offset: int) -> int:
    """Poll from ``offset`` until the record stops growing; return its end.

    Two cases here need a *quiet* shell, and for the same underlying reason:
    the shell writes on its own schedule (the prompt when it starts, another
    prompt after each command finishes), so a read taken mid-write would make
    "no new output" or "the same span" fail for a reason the AC is not about.
    Every read reports the record's current end regardless of the offset it was
    given, so polling until that end holds still for several consecutive tries
    establishes quiescence. The polls are ordinary non-consuming reads, so
    settling cannot hide output from the cases below.
    """
    end = -1
    stable = 0
    deadline = time.monotonic() + SETTLE_TIMEOUT
    while stable < SETTLE_STABLE_POLLS and time.monotonic() < deadline:
        current = await _read(session, session_id, offset)
        if current["nextOffset"] == end:
            stable += 1
        else:
            end = current["nextOffset"]
            stable = 0
        await asyncio.sleep(SETTLE_POLL)
    assert stable >= SETTLE_STABLE_POLLS, (
        f"the shell never stopped writing within {SETTLE_TIMEOUT:.0f}s "
        f"(last end {end})"
    )
    return end


async def test_session_read_ac1_offset_zero_returns_everything(
    session, session_id: str
) -> dict:
    """AC: session-read/AC1 — omitted offset reads from session start.

    Returns the first read so the later cases can continue from the cursor the
    server issued rather than one this file computed. The AC is explicit that
    the caller passes the server's ``nextOffset`` back, so the test does too.
    """
    first = await _read(session, session_id)

    assert first["nextOffset"] >= 0, first
    assert isinstance(first["payload"], str), first
    # The cursor is a byte offset into an append-only record, so a read from the
    # start must issue exactly the byte length of what it returned. A cursor
    # counting anything else -- characters, chunks, calls -- fails here.
    assert first["nextOffset"] == len(first["payload"].encode()), (
        "the opening read's cursor must be the byte length of everything since "
        f"session start: {first}"
    )
    assert first["path"] == "active", first
    assert first["session"]["state"] == "active", first
    print("offset 0 ok: nextOffset =", first["nextOffset"])
    return first


async def test_session_read_ac1_cursor_is_empty_without_new_output(
    session, session_id: str, cursor: int
) -> None:
    """AC: session-read/AC1 — no new output is an empty payload at the same cursor.

    Not an error and not a replay: the agent's ``Since`` returns nothing and
    hands back the same offset, so a caller polling with the server's cursor
    sees "nothing yet" without losing its place.
    """
    idle = await _read(session, session_id, cursor)

    assert idle["payload"] == "", f"expected no new output at {cursor}: {idle}"
    assert idle["nextOffset"] == cursor, (
        f"an empty read must return the same cursor, got {idle['nextOffset']} "
        f"from {cursor}"
    )
    print("empty-at-cursor ok:", cursor)


async def test_session_read_ac1_cursor_returns_only_the_increment(
    session, control_plane_url: str, session_id: str, cursor: int
) -> dict:
    """AC: session-read/AC1 — after new output, the cursor returns only the delta.

    Injects a command through the control plane (setup, not the tool under
    test), then reads at the cursor the previous case ended on. The delta must
    contain the command's own output and must *not* contain the opening span,
    which is what makes this "only what accumulated since" rather than "the
    whole buffer again".
    """
    inject_through_control_plane(control_plane_url, session_id, COMMAND)

    deadline = time.monotonic() + INCREMENT_TIMEOUT
    delta = await _read(session, session_id, cursor)
    while MARKER not in delta["payload"] and time.monotonic() < deadline:
        await asyncio.sleep(INCREMENT_POLL)
        delta = await _read(session, session_id, cursor)

    assert MARKER in delta["payload"], (
        f"the injected command's output never appeared within "
        f"{INCREMENT_TIMEOUT:.0f}s: {delta}"
    )
    assert delta["nextOffset"] > cursor, delta
    assert delta["nextOffset"] == cursor + len(delta["payload"].encode()), (
        f"the delta must be exactly the span between the two cursors: {delta}"
    )
    print("increment ok:", cursor, "->", delta["nextOffset"])
    return delta


async def test_session_read_ac1_repeating_a_cursor_replays_the_span(
    session, session_id: str, cursor: int, observed: str
) -> None:
    """AC: session-read/AC1 — the same cursor always returns the same span.

    Two assertions, because "the same span" has two halves. *Repeatable*: two
    reads at one cursor return byte-identical spans -- settled first, since the
    shell prints a fresh prompt after the command finishes and comparing across
    that append would fail for a reason the AC is not about. *Non-consuming*:
    what the increment case already received is still there, as a prefix of
    what the record now holds from that cursor. A control plane that consumed
    output would return something shorter and fail the second one.
    """
    await _quiet_end(session, session_id, cursor)

    again = await _read(session, session_id, cursor)
    once_more = await _read(session, session_id, cursor)

    assert again["payload"] == once_more["payload"], (
        f"two reads at cursor {cursor} returned different spans: "
        f"{again['payload']!r} != {once_more['payload']!r}"
    )
    assert again["nextOffset"] == once_more["nextOffset"], (again, once_more)
    assert again["payload"].startswith(observed), (
        f"re-reading cursor {cursor} no longer replays what it already "
        f"returned: {again['payload']!r} does not start with {observed!r}"
    )
    print("replay ok:", cursor, "->", again["nextOffset"])


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    with live_shell_session(SESSION_NAME) as (control_plane_url, created):
        session_id = created["id"]
        print("--- session-read/AC1 --- session", session_id, "pod", created["pod"])

        async with open_session(url) as session:
            first = await test_session_read_ac1_offset_zero_returns_everything(
                session, session_id
            )
            cursor = await _quiet_end(session, session_id, first["nextOffset"])

            await test_session_read_ac1_cursor_is_empty_without_new_output(
                session, session_id, cursor
            )

            delta = await test_session_read_ac1_cursor_returns_only_the_increment(
                session, control_plane_url, session_id, cursor
            )
            assert MARKER not in first["payload"], (
                "the marker must arrive in the delta, not in the opening span "
                f"this file read before injecting it: {first}"
            )

            # The span the previous case observed, re-read from the same cursor.
            await test_session_read_ac1_repeating_a_cursor_replays_the_span(
                session, session_id, cursor, delta["payload"]
            )
            print("ok: session-read/AC1")


if __name__ == "__main__":
    asyncio.run(run())
