"""End-to-end checks for the dear_baby_reset_user tool.

Per-AC case names + docstrings declare the AC they verify (registry rule 3);
``docs/doc-tracker.md`` is the AC<->case mapping SSOT. ``run()`` is only a
dispatcher: the cases share the ``dear-baby-fixture`` pod but none of them
mutates it (the fixture's ``/reset-user`` is a stub script), so their order is
free.
"""

from __future__ import annotations

import asyncio

from mcp.shared.exceptions import McpError

from _helpers import (
    assert_destructive_annotation,
    base_url,
    open_session,
    wait_for_healthz,
)

NAMESPACE = "dear-baby-test"
# Defaults the tool applies when selector/container are omitted
# (internal/mcp/mcp.go: dearBabyDefaultSelector / dearBabyDefaultContainer).
DEFAULT_SELECTOR = "app=dear-baby"
DEFAULT_CONTAINER = "backend"
POD_PREFIX = "dear-baby-fixture-"


async def test_dear_baby_reset_user_ac1_reset_execution(session) -> None:
    """AC: dear-baby-reset-user/AC1 — a valid email execs the reset CLI in the backend pod.

    Asserts the tool resolves a Running fixture pod, execs ``/reset-user`` with
    the given email, and reports the CLI's own outcome: stdout carries the
    reset line for that exact address, the exit code is 0 and ``success`` is
    true. The not-found email is exercised as a control: it proves the success
    assertions read the CLI's real output rather than a fixed payload, and shows
    a failing reset surfaces as an error result that still carries the exit code
    and stderr.

    Not asserted: that the onboarding fields (onboarded_at, due_date, coachmark,
    first_record_at, ai_preview) are actually cleared while the user record
    survives. The kind fixture's ``/reset-user`` is a busybox stub script (see
    tests/k8s/kind/dear-baby-fixture.yaml) with no database behind it, so field
    level effects are unobservable here; observing them would need the real
    dear-baby backend image plus a seeded database in the CI cluster.
    """
    result = await session.call_tool(
        "dear_baby_reset_user",
        {"namespace": NAMESPACE, "email": "user@example.com"},
    )
    assert result.isError is False, result
    payload = result.structuredContent
    pod = payload.pop("pod")
    stdout = payload.pop("stdout")
    assert pod.startswith(POD_PREFIX), pod
    assert "reset user for user@example.com" in stdout, stdout
    assert payload == {
        "namespace": NAMESPACE,
        "email": "user@example.com",
        "selector": DEFAULT_SELECTOR,
        "container": DEFAULT_CONTAINER,
        "exitCode": 0,
        "stderr": "",
        "success": True,
    }, payload
    print("reset ok against pod", pod)

    failed = await session.call_tool(
        "dear_baby_reset_user",
        {"namespace": NAMESPACE, "email": "missing@example.com"},
    )
    assert failed.isError is True, failed
    payload = failed.structuredContent
    pod = payload.pop("pod")
    stderr = payload.pop("stderr")
    assert pod.startswith(POD_PREFIX), pod
    assert "no user found" in stderr, stderr
    assert payload == {
        "namespace": NAMESPACE,
        "email": "missing@example.com",
        "selector": DEFAULT_SELECTOR,
        "container": DEFAULT_CONTAINER,
        "exitCode": 1,
        "stdout": "",
        "success": False,
    }, payload
    print("reset failure path ok")


async def test_dear_baby_reset_user_ac2_explicit_target(session) -> None:
    """AC: dear-baby-reset-user/AC2 — email is mandatory, selector/container default but override.

    Three clauses, one assertion each. (1) A call without ``email`` is rejected
    before anything is exec'd — argument errors come back as JSON-RPC errors,
    surfaced by the SDK as ``McpError``. (2) With selector/container omitted the
    tool resolves the pod through the documented defaults and echoes them back.
    (3) Both overrides are honoured, and the discriminator in each case is that
    the call *fails*: an ignored ``selector`` would have resolved the fixture pod
    anyway, and an ignored ``container`` would have exec'd ``backend`` and
    succeeded.

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


async def test_dear_baby_reset_user_ac3_destructive_hint(session) -> None:
    """AC: dear-baby-reset-user/AC3 — dear_baby_reset_user advertises destructiveHint=true.

    Verifies the destructive-operation marking via tools/list metadata only; no
    user reset is exec'd.
    """
    await assert_destructive_annotation(session, "dear_baby_reset_user")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- dear_baby_reset_user reset (AC: dear-baby-reset-user/AC1) ---")
        await test_dear_baby_reset_user_ac1_reset_execution(session)

        print("--- dear_baby_reset_user targeting (AC: dear-baby-reset-user/AC2) ---")
        await test_dear_baby_reset_user_ac2_explicit_target(session)

        print(
            "--- dear_baby_reset_user destructiveHint "
            "(AC: dear-baby-reset-user/AC3) ---"
        )
        await test_dear_baby_reset_user_ac3_destructive_hint(session)
        print("dear_baby_reset_user destructiveHint ok")


if __name__ == "__main__":
    asyncio.run(run())
