"""Deployed-server e2e for dear-baby-reset-user/AC2 (explicit target).

검증 시나리오: test-dear-baby-reset-user.md#시나리오 2
실행 대상: primary
병렬 레인: dear-baby
"""

from __future__ import annotations

import asyncio

from mcp.shared.exceptions import McpError

from _helpers import base_url, open_session, wait_for_healthz


NAMESPACE = "dear-baby-test"

# internal/mcp/mcp.go: dearBabyDefaultSelector / dearBabyDefaultContainer.
DEFAULT_SELECTOR = "app=dear-baby"

DEFAULT_CONTAINER = "backend"

POD_PREFIX = "dear-baby-fixture-"


async def test_dear_baby_reset_user_ac2_explicit_target(session) -> None:
    """AC: dear-baby-reset-user/AC2 — email is mandatory, selector/container default but override.

    The discriminator in each override clause is that the call *fails*: an
    ignored ``selector`` would have resolved the fixture pod anyway, and an
    ignored ``container`` would have exec'd ``backend`` and succeeded.

    The exact wording of the apiserver's invalid-container rejection is not
    asserted, only that the call fails and the override is echoed back — the
    message is the cluster's to phrase, not this server's.
    """
    try:
        await session.call_tool(
            "dear_baby_reset_user",
            {"namespace": NAMESPACE},
        )
    except McpError as exc:
        assert "email is required" in str(exc), exc
        print("missing-email rejection ok")
    else:
        raise AssertionError("expected McpError for a call without email")

    defaulted = await session.call_tool(
        "dear_baby_reset_user",
        {"namespace": NAMESPACE, "email": "user@example.com"},
    )
    assert defaulted.isError is False, defaulted
    payload = defaulted.structuredContent
    assert payload["selector"] == DEFAULT_SELECTOR, payload
    assert payload["container"] == DEFAULT_CONTAINER, payload
    assert payload["pod"].startswith(POD_PREFIX), payload

    other_selector = await session.call_tool(
        "dear_baby_reset_user",
        {
            "namespace": NAMESPACE,
            "email": "user@example.com",
            "selector": "app=does-not-exist",
        },
    )
    assert other_selector.isError is True, other_selector
    text = other_selector.content[0].text
    assert "no Running pod matched" in text, text
    assert "app=does-not-exist" in text, text
    print("selector override ok")

    other_container = await session.call_tool(
        "dear_baby_reset_user",
        {
            "namespace": NAMESPACE,
            "email": "user@example.com",
            "container": "not-a-container",
        },
    )
    assert other_container.isError is True, other_container
    print("container override ok ->", other_container.content[0].text.strip()[:120])


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- dear_baby_reset_user (AC: dear-baby-reset-user/AC2) ---")
        await test_dear_baby_reset_user_ac2_explicit_target(session)
        print("ok: dear-baby-reset-user/AC2")


if __name__ == "__main__":
    asyncio.run(run())
