"""Deployed-server e2e for session-write/AC4 (거부 응답의 구분 전달).

검증 AC: session-write/AC4
실행 대상: primary

`tests/integration/check_ac_mapping.py`가 이 선언을 읽어 `docs/doc-tracker.md`의 레지스트리와
대조하고, `tests/integration/run_all.py`가 `실행 대상`을 읽어 이 파일을 배차한다.

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).

AC4가 요구하는 것은 「거부가 **구분되어** 전달된다」이지 「거부가 일어난다」가 아니다. 그래서
이 파일은 각 거부를 관측한 뒤, 관측한 문구들이 **쌍쌍이 다르고** 재시도 의미까지 갈리는지를
따로 단정한다 — 그것이 AC 본문의 「재시도가 의미 있는 거부와 그렇지 않은 거부가 메시지에서
구별된다」다. 문구 리터럴은 손으로 짓지 않고 `internal/sessionplatform`의 ``Error.Error()``
스위치에서 그대로 가져왔다(아래 상수).

**세 거부는 서로 다른 층에서 난다 — 그게 이 파일이 셋을 따로 세우는 이유다.**

* **없는 세션(404)** — 워크로드 타입과 무관하다. 제어면 `Service.Write`가 `Get`에서 이미
  막고, 클라이언트가 `kindNotFound`로 올린다.
* **페이로드 상한 초과(413)** — `claude-code`에서만 재는 상한이고, **파드에 닿지 않는다**:
  `Service.Write`가 `activate`를 부르기 **전에** `WorkloadType == claude-code &&
  len(payload) > MaxClaudePromptBytes`를 본다. 그래서 이 케이스는 세션이 살아 있다는 것 외에
  데이터 플레인에 아무것도 요구하지 않으며, 상한 **경계**(정확히 1 MiB는 통과, +1 바이트는
  거부)까지 함께 잰다 — 그러지 않으면 「큰 페이로드가 거부됐다」가 상한과 무관한 다른 이유로도
  성립해 버린다.
* **프롬프트 큐 포화(429)** — 에이전트 자신의 유계 큐에서 난다. `claude.go`의 ``enqueue``가
  ``queue.Len() >= maxClaudeQueuedPrompts``(**64개**)이거나
  ``queuedBytes + len(prompt) > maxClaudeQueuedBytes``(8 MiB)면 ``errClaudeQueueFull``을
  돌려주고 `POST /write`가 그것을 429로 낸다. 워커는 프롬프트 하나마다 node 기반 Claude CLI를
  새로 띄우므로 배출이 주입보다 느리다. **상류 호출은 개입하지 않는다** — 거부는 큐에 넣기
  전에 결정되고, 그래서 자리표시자 자격증명으로도 이 경로가 성립한다.

**출력 쿼터 소진(507)은 이 파일이 단정하지 않는다 — 지금의 하네스에서 도달 불가이기 때문이다.**
근거는 추정이 아니라 소스다: 507은 ``enqueue``의 ``c.out.claudeFullAt(c.scrollbackLimit, ...)``
에서만 나고, 그 판정은 ``len(buf) > scrollbackLimit - marker``다. 그런데 ``scrollbackLimit``은
``claudeConfig.ScrollbackLimit``이 0이면 ``maxClaudeScrollbackBytes``(**256 MiB**)로 고정되고
(`data-plane/cmd/agent/claude.go`), 그 필드를 **채우는 env·플래그가 데이터 플레인에 없다** —
에이전트 `main.go`의 생성 호출은 ``StateDir``·``Model``·``Binary``·``RunTimeout``만 env에서
읽는다. 즉 e2e가 507을 보려면 유효한 상류 자격증명으로 에이전트에 256 MiB의 출력을 실제로
쌓아야 하고, 그것은 이 하네스의 예산 밖이다. 우회로였던 「이미 상한을 소진한 아카이브를
복원한다」도 막혀 있다: 아카이브 생성·복원 양쪽이 256 MiB 초과를 거부하므로 제품 경로로는
그런 아카이브가 만들어지지 않는다.

그래서 507 분기의 **대체 검증은 Go 단위**다 — session-platform 쪽에서 507이 나는 조건 자체가,
이 레포 쪽에서 ``TestWriteMapsControlPlaneRefusals``와
``TestWriteRefusalsAreDistinctWithoutTheControlPlanesProse``가 「네 상태코드에 같은 본문을
물려도 네 메시지가 쌍쌍이 다르다」를 검사한다. 아래 `test_..._refusals_are_pairwise_distinct`
가 그 단위 검사와 같은 성질을 **관측한 세 문구 + 507 문구 상수**에 대해 한 번 더 확인해,
문구 상수가 구현에서 떠내려가면 이 파일도 함께 깨지게 한다. 해제 조건과 소유는
`docs/doc-tracker.md`의 backlog에 적혀 있다(session-platform 데이터 플레인이 그 상한을 설정
표면으로 노출하는 것 — 다른 레포·다른 모델의 몫).

세션은 이 파일이 만들고 이 파일이 지운다(`_session_platform.live_claude_code_session`) —
다른 파일이 무엇을 남겨 놨든 무관하고, 나갈 때 파드까지 회수된다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _session_platform import (
    MAX_CLAUDE_PROMPT_BYTES,
    live_claude_code_session,
)

#: 제어면에 만들 세션 이름. 진단 로그에서 어느 파일의 세션인지 드러나야 한다.
SESSION_NAME = "e2e write ac4"

#: 제어면에 없는 id. 이 파일이 만드는 세션과 겹치지 않는다.
MISSING_SESSION_ID = "e2e-write-ac4-missing"

#: `internal/sessionplatform` 의 ``Error.Error()`` 가 각 종류 앞에 붙이는 접두.
#: 손으로 지은 문구가 아니라 그 스위치의 리터럴이며, AC4 가 요구하는 "구분"은
#: 바로 이 네 접두가 서로 다르다는 사실이다. 재시도 의미도 여기서 갈린다 —
#: 큐 포화만 "retry after ...", 나머지 둘은 "retrying will not help".
NOT_FOUND_PREFIX = "session platform not found: "
TOO_LARGE_PREFIX = "session platform payload too large, retrying will not help: "
BUSY_PREFIX = "session platform busy, retry after a queued prompt finishes: "
QUOTA_PREFIX = (
    "session platform output quota exhausted, retrying will not help "
    "(existing output is still readable): "
)

#: 큐를 채우러 넣을 프롬프트 한 발의 크기. ``enqueue`` 는 **개수 상한(64)** 과
#: **바이트 예산(8 MiB)** 둘 중 먼저 걸리는 쪽으로 거부하고, 이 크기에서는 개수가
#: 먼저 찬다(64 × 64 KiB = 4 MiB).
#:
#: **일부러 작게 잡았다.** 워커는 프롬프트를 ``claude`` 의 argv **마지막 원소**로
#: 넘기는데(`claude.go` 의 ``argv``), 리눅스는 argv 원소 하나의 길이를
#: ``MAX_ARG_STRLEN``(4 KiB 페이지에서 **128 KiB**)로 제한한다. 상한(1 MiB)에 가까운
#: 프롬프트는 그래서 ``execve`` 단계에서 즉시 실패하고, 그러면 **워커가 큐를 실행이
#: 아니라 실패로 비워** 배출이 주입보다 빨라진다 — 큐를 채우려는 이 케이스에는
#: 정확히 역효과다. 128 KiB 아래로 두면 CLI 가 실제로 기동하므로(node 부팅 + 설정
#: 로드) 배출 한 발이 주입 한 발보다 훨씬 느리다.
QUEUE_FILL_PROMPT_BYTES = 64 << 10

#: 큐 포화까지 허용할 최대 주입 횟수. 개수 상한이 64 이므로 워커가 한 발도 배출하지
#: 못하면 65 발째에 차고, 배출이 있으면 그만큼 더 든다. 스텁 하네스로 배출 속도를
#: 바꿔 가며 재 보면 「주입 2회당 1발 배출」이라는 비관적 비율에서도 127 발이면 차고,
#: 현실적인 비율(node 기동 한 번 ≫ MCP 왕복 한 번)에서는 70 발 안쪽이다. 200 은 그
#: 여유분이다. 상한이 있는 루프라 포화하지 않아도 매달리지 않고, 실패 메시지가 **몇
#: 발이 수락됐는지**를 싣는다 — 「큐가 안 찼다」와 「찼는데 다른 말을 했다」를 가르는
#: 정보이자, 계획이 정의한 축소 범위로 넘어갈지의 판단 근거다.
QUEUE_FILL_MAX_ATTEMPTS = 200


def _error_text(result) -> str:
    """The text of a tool result that is expected to carry ``isError``."""
    assert result.isError is True, f"the call did not fail: {result}"
    assert result.content, result
    block = result.content[0]
    assert block.type == "text", block
    return block.text


async def test_session_write_ac4_missing_session_is_not_found(session) -> str:
    """AC: session-write/AC4 — an unknown id is refused as not-found.

    This is the one refusal of the four that does not depend on the workload
    type: the control plane never reaches a pod for a session it does not
    hold. Asserted as a tool result carrying ``isError`` (not a transport
    failure), and asserted to be *distinguishable* from the unconfigured
    refusal session-write/AC5 covers -- a caller that cannot tell "no such
    session" from "the integration is off" would retry the wrong thing.

    Returns the refusal text so the distinctness case compares what was
    actually observed rather than constants alone.
    """
    text = _error_text(
        await session.call_tool(
            "session_write", {"id": MISSING_SESSION_ID, "payload": "noop\n"}
        )
    )

    assert text.startswith(NOT_FOUND_PREFIX), text
    assert MISSING_SESSION_ID in text, text
    assert "unavailable" not in text, text
    print("not-found ok:", text)
    return text


async def test_session_write_ac4_oversized_payload_is_refused(
    session, session_id: str
) -> str:
    """AC: session-write/AC4 — a prompt past the 1 MiB ceiling is refused as too large.

    The ceiling is workload-specific and the control plane measures it *before*
    activating the session, so this refusal is decided without the pod being
    touched. Both sides of the boundary are asserted: exactly 1 MiB is accepted
    and one byte more is refused. Without the accepted case, "a big payload was
    refused" would also hold if something unrelated to the ceiling rejected it,
    and the file would be asserting the wrong thing.

    Returns the refusal text, like the not-found case above.
    """
    at_limit = await session.call_tool(
        "session_write", {"id": session_id, "payload": "x" * MAX_CLAUDE_PROMPT_BYTES}
    )
    assert at_limit.isError is False, (
        f"a payload of exactly the limit must be accepted: {at_limit}"
    )

    text = _error_text(
        await session.call_tool(
            "session_write",
            {"id": session_id, "payload": "x" * (MAX_CLAUDE_PROMPT_BYTES + 1)},
        )
    )

    assert text.startswith(TOO_LARGE_PREFIX), text
    # The refusal must not read like the retryable one: this payload will never
    # be accepted, however long the caller waits.
    assert "retry after" not in text, text
    print("payload-too-large ok:", text)
    return text


async def test_session_write_ac4_full_queue_is_refused_as_busy(
    session, session_id: str
) -> str:
    """AC: session-write/AC4 — a saturated prompt queue is refused as retryable.

    The agent's ``enqueue`` admits prompts against two ceilings -- a queue
    length and a byte budget -- and refuses with 429 once either would be
    exceeded. Its worker drains serially by launching the Claude CLI per
    prompt, so injection (one MCP round trip) outruns drainage (a Node process
    start) and the queue fills. See ``QUEUE_FILL_PROMPT_BYTES`` for why the
    prompts are deliberately small rather than ceiling-sized: an oversized
    argv element would make the worker *fail* each prompt instantly instead of
    running it, and a queue that empties at exec speed cannot be saturated.

    The loop is bounded and reports how far it got, which separates "the queue
    never filled" from "it filled but said something else".

    Returns the refusal text so the distinctness case can compare it with the
    others actually observed rather than with a constant alone.
    """
    prompt = "x" * QUEUE_FILL_PROMPT_BYTES
    accepted = 0
    for _ in range(QUEUE_FILL_MAX_ATTEMPTS):
        result = await session.call_tool(
            "session_write", {"id": session_id, "payload": prompt}
        )
        if result.isError:
            text = _error_text(result)
            assert text.startswith(BUSY_PREFIX), (
                f"the queue refused a write for a reason other than saturation "
                f"after {accepted} accepted prompt(s): {text}"
            )
            # The retryable refusal must be legible as retryable, and must not
            # collapse onto the permanent ones.
            assert "retrying will not help" not in text, text
            print(f"queue-full ok after {accepted} accepted prompt(s):", text)
            return text
        accepted += 1

    raise AssertionError(
        f"the prompt queue never saturated: {accepted} prompts of "
        f"{QUEUE_FILL_PROMPT_BYTES} bytes were all accepted, so no 429 was "
        f"observed within {QUEUE_FILL_MAX_ATTEMPTS} attempts"
    )


async def test_session_write_ac4_output_stays_readable_after_a_refusal(
    session, session_id: str
) -> None:
    """AC: session-write/AC4 — a refused write does not cost the caller its output.

    AC4 attaches this clause to the queue-full and quota-exhausted refusals:
    the write is rejected, but what the session already produced stays
    retrievable. Asserted right after the saturation case above, so the session
    under observation is one that just refused a write. The cursor matters as
    much as the payload -- a read that succeeded but stopped issuing cursors
    would leave the caller unable to continue.
    """
    result = await session.call_tool("session_read", {"id": session_id})

    assert result.isError is False, (
        f"reading a session that just refused a write failed: {result}"
    )
    body = result.structuredContent
    assert body["session"]["id"] == session_id, body
    assert isinstance(body["payload"], str), body
    assert body["nextOffset"] >= 0, body
    print("read-after-refusal ok: cursor", body["nextOffset"])


async def test_session_write_ac4_refusals_are_pairwise_distinct(
    not_found: str, too_large: str, busy: str
) -> None:
    """AC: session-write/AC4 — the refusals do not collapse onto one another.

    The three texts observed above are compared with each other and with the
    quota-exhausted prefix, which this harness cannot reach (see the module
    docstring). Two properties are asserted: every pair differs, and the
    retry-meaningful refusal is the only one that reads as retryable. Comparing
    *prefixes* rather than whole messages is deliberate -- the tail is the
    control plane's own prose, and a caller that had to parse that prose to
    tell the cases apart would be relying on something no contract pins.
    """
    observed = {
        NOT_FOUND_PREFIX: not_found,
        TOO_LARGE_PREFIX: too_large,
        BUSY_PREFIX: busy,
    }
    for prefix, text in observed.items():
        assert text.startswith(prefix), (prefix, text)

    prefixes = [NOT_FOUND_PREFIX, TOO_LARGE_PREFIX, BUSY_PREFIX, QUOTA_PREFIX]
    assert len(set(prefixes)) == len(prefixes), prefixes
    for left in prefixes:
        for right in prefixes:
            if left is not right:
                assert not left.startswith(right), (left, right)

    retryable = [p for p in prefixes if "retry after" in p]
    permanent = [p for p in prefixes if "retrying will not help" in p]
    assert retryable == [BUSY_PREFIX], retryable
    assert set(permanent) == {TOO_LARGE_PREFIX, QUOTA_PREFIX}, permanent
    print("distinctness ok: 4 prefixes, 1 retryable, 2 explicitly permanent")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    with live_claude_code_session(SESSION_NAME) as (_control_plane_url, created):
        session_id = created["id"]
        print("--- session-write/AC4 --- session", session_id, "pod", created["pod"])
        assert created["workloadType"] == "claude-code", created

        async with open_session(url) as session:
            not_found = await test_session_write_ac4_missing_session_is_not_found(
                session
            )
            too_large = await test_session_write_ac4_oversized_payload_is_refused(
                session, session_id
            )
            # Saturation is last of the three: it fills the agent's queue, and
            # the read-after-refusal clause below is about a session that has
            # just been refused.
            busy = await test_session_write_ac4_full_queue_is_refused_as_busy(
                session, session_id
            )
            await test_session_write_ac4_output_stays_readable_after_a_refusal(
                session, session_id
            )
            await test_session_write_ac4_refusals_are_pairwise_distinct(
                not_found, too_large, busy
            )
            print("ok: session-write/AC4")


if __name__ == "__main__":
    asyncio.run(run())
