"""session_write: SESSION_PLATFORM_ENDPOINT 미설정 시 graceful 거부 (e2e).

검증 시나리오: test-session-write.md#시나리오 5
실행 대상: auth-variant

``SESSION_PLATFORM_ENDPOINT`` is one of the endpoints this variant leaves
unattached: the only place in the repo that sets it is the base deployment
(``k8s/deployment.yaml``), which this variant does not use -- so the AC's
premise holds here and nowhere else. ``session_list_ac3.py`` and
``session_read_ac4.py`` are the precedents for this seat; all three tools share
one ``Unavailable`` service, so this file asserts the refusal is scoped to
*this* tool rather than assuming it.

Both ``id`` and ``payload`` are required by the tool schema, so both are
supplied, and both are supplied *validly*: ``sessionWrite`` rejects a missing or
non-string argument with a ``-32602`` protocol error **before** it ever reaches
the service, so an omitted payload would make this file assert argument
validation instead of the graceful refusal it claims. The id names no real
session on purpose -- with the endpoint unset the refusal must arrive before any
target lookup could matter, so a caller cannot mistake it for a not-found. The
payload is inert text for the same reason: nothing can execute it here, and this
file must not be the one that first injects input into a workload (that is
session-write/AC1).
"""

from __future__ import annotations

import asyncio

from _auth_variant import (
    API_KEY,
    SESSION_PLATFORM_REFUSAL,
    assert_unavailable_refusal,
)
from _helpers import base_url, open_session, wait_for_healthz

UNRESOLVABLE_SESSION_ID = "e2e-write-ac5-unconfigured"

INERT_PAYLOAD = "e2e probe: this never reaches a workload\n"


async def test_session_write_ac5_unconfigured_refusal(session) -> None:
    """AC: session-write/AC5"""
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
