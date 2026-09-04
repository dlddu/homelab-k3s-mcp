"""session_read: SESSION_PLATFORM_ENDPOINT 미설정 시 graceful 거부 (e2e).

검증 AC: session-read/AC4
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
premise holds here and nowhere else. ``session_list_ac3.py`` is the precedent
for this seat; the two tools share one ``Unavailable`` service, so this file
asserts the refusal is scoped to *this* tool rather than assuming it.

The shared assertion lives in ``_auth_variant.assert_unavailable_refusal``: it
checks both halves of the criterion — the call comes back as a normal MCP tool
result carrying ``isError`` and the unavailable-class text, and ``ping`` still
answers ``pong`` on the same session afterwards.

``id`` is required by the tool schema, so one is supplied. It names no real
session on purpose: with the endpoint unset the refusal must arrive before any
target lookup could matter, so a caller cannot mistake it for
session-read/AC3's not-found.
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
UNRESOLVABLE_SESSION_ID = "e2e-read-ac4-unconfigured"


async def test_session_read_ac4_unconfigured_refusal(session) -> None:
    """AC: session-read/AC4

    With SESSION_PLATFORM_ENDPOINT unset, session_read returns the unavailable
    error instead of crashing, and the server keeps serving other tools.
    """
    await assert_unavailable_refusal(
        session,
        "session_read",
        {"id": UNRESOLVABLE_SESSION_ID},
        SESSION_PLATFORM_REFUSAL,
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        print("--- unconfigured graceful refusal (AC: session-read/AC4) ---")
        await test_session_read_ac4_unconfigured_refusal(session)
        print("refusal ok: session-read/AC4")


if __name__ == "__main__":
    asyncio.run(run())
