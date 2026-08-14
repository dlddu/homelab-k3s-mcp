"""Deployed-server e2e for dear-baby-reset-user/AC1 (reset execution).

검증 AC: dear-baby-reset-user/AC1
실행 대상: primary

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz


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


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- dear_baby_reset_user (AC: dear-baby-reset-user/AC1) ---")
        await test_dear_baby_reset_user_ac1_reset_execution(session)
        print("ok: dear-baby-reset-user/AC1")


if __name__ == "__main__":
    asyncio.run(run())
