"""session_write: SESSION_PLATFORM_ENDPOINT 미설정 시 graceful 거부 (e2e).

검증 AC: session-write/AC5
실행 대상: auth-variant

`tests/integration/check_ac_mapping.py`가 이 선언을 읽어 `docs/doc-tracker.md`의 레지스트리와
대조하고, `tests/integration/run_all.py`가 `실행 대상`을 읽어 이 파일을 배차한다.

This runs against the deployment variant in ``tests/k8s/kind/auth-fixture.yaml``:
auth is on (``MCP_API_KEYS`` set, ``MCP_AUTH_DISABLED`` unset) and no credential
secret or integration endpoint is attached at all, so ``main.go``'s
``build*Service`` helpers each degrade to ``NewUnavailable("")`` while the
server still starts. ``SESSION_PLATFORM_ENDPOINT`` is one of the absent ones --
the only place in the repo that sets it is the base deployment
(``k8s/deployment.yaml``), which this variant does not use -- so the AC's
premise holds here and nowhere else. ``session_list_ac3.py`` and
``session_read_ac4.py`` are the precedents for this seat; all three tools share
one ``Unavailable`` service, so this file asserts the refusal is scoped to
*this* tool rather than assuming it.

The shared assertion lives in ``_auth_variant.assert_unavailable_refusal``: it
checks both halves of the criterion — the call comes back as a normal MCP tool
result carrying ``isError`` and the unavailable-class text, and ``ping`` still
answers ``pong`` on the same session afterwards.

Both ``id`` and ``payload`` are required by the tool schema, so both are
supplied, and both are supplied *validly*: ``sessionWrite`` rejects a missing or
non-string argument with a ``-32602`` protocol error **before** it ever reaches
the service, so an omitted payload would make this file assert argument
validation instead of the graceful refusal it claims. The id names no real
session on purpose -- with the endpoint unset the refusal must arrive before any
target lookup could matter, so a caller cannot mistake it for a not-found. The
payload is inert text for the same reason: nothing can execute it here, and this
file must not be the one that first injects input into a workload (that is
session-write/AC1, which is still blocked on a real data plane).
"""

from __future__ import annotations

import asyncio

from _auth_variant import (
    API_KEY,
    SESSION_PLATFORM_REFUSAL,
    assert_unavailable_refusal,
)
from _helpers import base_url, open_session, wait_for_healthz

#: 이 변형에는 제어면이 없으므로 어떤 id 도 조회될 수 없다. 거부가 대상 조회보다
#: 먼저 일어난다는 사실을 드러내려고 실재하지 않는 id 를 고른다.
UNRESOLVABLE_SESSION_ID = "e2e-write-ac5-unconfigured"

#: 인자 검증(-32602)을 통과시키기 위한 유효한 문자열. 닿을 워크로드가 없으므로
#: 실행되지 않는다 — 그 사실이 드러나도록 명령이 아닌 평문을 쓴다.
INERT_PAYLOAD = "e2e probe: this never reaches a workload\n"


async def test_session_write_ac5_unconfigured_refusal(session) -> None:
    """AC: session-write/AC5

    With SESSION_PLATFORM_ENDPOINT unset, session_write returns the unavailable
    error instead of crashing, and the server keeps serving other tools.
    """
    await assert_unavailable_refusal(
        session,
        "session_write",
        {"id": UNRESOLVABLE_SESSION_ID, "payload": INERT_PAYLOAD},
        SESSION_PLATFORM_REFUSAL,
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        print("--- unconfigured graceful refusal (AC: session-write/AC5) ---")
        await test_session_write_ac5_unconfigured_refusal(session)
        print("refusal ok: session-write/AC5")


if __name__ == "__main__":
    asyncio.run(run())
