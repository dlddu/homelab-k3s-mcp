"""Deployed-server e2e for session-write/AC1 (워크로드 입력 주입).

검증 AC: session-write/AC1
실행 대상: primary

**AC1의 검증 방법이 두 도구를 한 문장에 묶는다** — 「shell 세션에 명령을 write한 뒤 read로 그
명령의 출력이 누적 출력에 나타남을 확인한다」. 그래서 이 파일은 ``session_write``를 주검증
대상으로 선언하되 ``session_read``로 결과를 관측한다. 규칙 2의 「셋업·관측에서 다른 AC의 도구를
경유하는 것은 검증으로 세지 않는다」가 이 자리를 위한 단서다 — 이 파일이 단정하는 것은
**주입이 워크로드에 도달했는가**이지 커서 규약이 아니고, 커서 규약은 `session_read_ac1.py`가
자기 파일에서 따로 단정한다.

**비블로킹이라는 절도 함께 단정한다.** AC 본문이 「호출은 실행 완료를 기다리지 않고 반환하며,
산출물은 `session_read`의 누적 출력으로 관측된다」이므로, write 응답에 출력이 없다는 사실
자체가 단정 대상이다 — ``WriteResult``는 ``path``와 ``session``만 싣는다(`internal/mcp`의
``sessionWrite``가 그 두 키만 넣는다). 반환 즉시 출력이 있는지를 시간으로 재는 대신 **응답의
모양**으로 재는 이유는 그것이 구현이 약속한 계약이고 타이밍은 CI 부하에 흔들리기 때문이다.

**claude-code 절은 범위 밖이다.** AC 본문의 두 번째 문장(프롬프트 1회 실행이 큐에 적재된다)은
`workloadType=claude-code` 세션을 요구하는데, 이 하네스의 제어면은
``DATA_PLANE_CLAUDE_CODE_IMAGE``를 갖지 않아 그 타입을 배포할 수 없다(그 파드는
claude-code-credentials Secret 과 credential-proxy 사이드카를 필요로 한다). shell 절만으로
「입력이 워크로드에 전달되어 실행되고 산출물이 누적 출력에 나타난다」는 AC의 관측 가능한
핵심이 성립하며, claude-code 절은 `docs/doc-tracker.md`의 backlog 에 후속으로 적혀 있다.

세션은 이 파일이 만들고 이 파일이 지운다(`_session_platform.live_shell_session`) — 다른 파일이
무엇을 남겨 놨든 무관하고, 나갈 때 파드까지 회수된다.
"""

from __future__ import annotations

import asyncio
import time

from _helpers import base_url, open_session, wait_for_healthz
from _session_platform import live_shell_session

#: 제어면에 만들 세션 이름. 진단 로그에서 어느 파일의 세션인지 드러나야 한다.
SESSION_NAME = "e2e write ac1"

#: 주입할 명령. 개행이 있어야 PTY 가 명령을 실행한다 — 도구 설명이 "include a
#: trailing newline to submit a command" 라고 적은 그대로다.
#:
#: 마커는 **명령문 안에 나타나지 않도록** 두 조각으로 갈라 넣는다. PTY 는 타이핑된
#: 명령을 그대로 되울리므로, 명령문에 마커가 통째로 들어 있으면 「출력에 마커가 있다」는
#: 단정이 **에코만으로도 성립**해 버린다 — 그러면 이 파일은 「입력이 터미널에 닿았다」를
#: 단정하는 것이지 「워크로드가 그것을 실행했다」를 단정하는 것이 아니게 된다. 붙여 쓴
#: 형태는 쉘이 `printf` 를 실제로 실행해야만 만들어진다.
MARKER = "e2e-write-ac1-executed"
COMMAND = "printf '%s-%s\\n' e2e-write-ac1 executed\n"

#: 에코 자체의 표지. 「주입한 바이트가 터미널에 닿았다」와 「쉘이 실행했다」를 따로
#: 관측하기 위한 것이라, 명령문에는 있고 산출물에는 없는 토큰을 고른다.
ECHOED = "printf"

#: 출력이 누적될 때까지의 폴링 예산. 명령 실행은 write 반환 이후에 일어난다.
OUTPUT_TIMEOUT = 30.0
OUTPUT_POLL = 0.5


async def _read_all(session, session_id: str) -> str:
    """The session's whole accumulated output, read from offset 0."""
    result = await session.call_tool("session_read", {"id": session_id})
    assert result.isError is False, result
    return result.structuredContent["payload"]


async def test_session_write_ac1_returns_without_waiting_for_the_workload(
    session, session_id: str
) -> None:
    """AC: session-write/AC1 — the call is accepted and carries no output.

    The control plane returns once the payload is accepted by the agent, not
    once the shell has run it, so the result reports only which branch served
    the write and the session as it stands afterwards. Asserting the *shape*
    pins that contract: a result that carried the command's output would mean
    the call had waited, and a caller could then stop using session_read.
    """
    result = await session.call_tool(
        "session_write", {"id": session_id, "payload": COMMAND}
    )

    assert result.isError is False, result
    body = result.structuredContent
    assert set(body) == {"path", "session"}, (
        f"a write result must carry only the branch and the session: {body}"
    )
    assert body["path"] == "active", body
    assert body["session"]["id"] == session_id, body
    assert body["session"]["state"] == "active", body
    print("write accepted ok:", body["path"])


async def test_session_write_ac1_payload_reaches_the_workload(
    session, session_id: str
) -> None:
    """AC: session-write/AC1 — the injected command runs and its output accumulates.

    Polls ``session_read`` until the marker appears in the accumulated output.
    The marker exists only in the *result* of running the command, never in the
    command text the PTY echoes back, so finding it is evidence that the shell
    executed the payload rather than merely received it. The echo is asserted
    separately, which separates the two failures: bytes that never arrived look
    different from bytes that arrived and were not run.
    """
    deadline = time.monotonic() + OUTPUT_TIMEOUT
    payload = await _read_all(session, session_id)
    while MARKER not in payload and time.monotonic() < deadline:
        await asyncio.sleep(OUTPUT_POLL)
        payload = await _read_all(session, session_id)

    assert ECHOED in payload, (
        f"the injected command was never echoed by the PTY, so it did not "
        f"reach the workload's terminal at all: {payload!r}"
    )
    assert MARKER in payload, (
        f"the injected command was echoed but produced no output within "
        f"{OUTPUT_TIMEOUT:.0f}s -- it reached the terminal without being run: "
        f"{payload!r}"
    )
    print("workload output ok: echo and result both present")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    with live_shell_session(SESSION_NAME) as (_control_plane_url, created):
        session_id = created["id"]
        print("--- session-write/AC1 --- session", session_id, "pod", created["pod"])

        async with open_session(url) as session:
            before = await _read_all(session, session_id)
            assert MARKER not in before, (
                f"the marker is present before this file injected it: {before!r}"
            )

            await test_session_write_ac1_returns_without_waiting_for_the_workload(
                session, session_id
            )
            await test_session_write_ac1_payload_reaches_the_workload(
                session, session_id
            )
            print("ok: session-write/AC1")


if __name__ == "__main__":
    asyncio.run(run())
