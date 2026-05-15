"""End-to-end checks for the dear_baby_reset_user tool."""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "dear-baby-test"


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- dear_baby_reset_user (success path) ---")
        result = await session.call_tool(
            "dear_baby_reset_user",
            {"namespace": NAMESPACE, "email": "user@example.com"},
        )
        assert result.isError is False, result
        payload = result.structuredContent
        pod = payload.pop("pod")
        stdout = payload.pop("stdout")
        assert pod.startswith("dear-baby-fixture-"), pod
        assert "reset user for user@example.com" in stdout, stdout
        assert payload == {
            "namespace": NAMESPACE,
            "email": "user@example.com",
            "selector": "app=dear-baby",
            "container": "backend",
            "exitCode": 0,
            "stderr": "",
            "success": True,
        }, payload
        print("reset ok against pod", pod)

        print("--- dear_baby_reset_user (failure path) ---")
        result = await session.call_tool(
            "dear_baby_reset_user",
            {"namespace": NAMESPACE, "email": "missing@example.com"},
        )
        assert result.isError is True, result
        payload = result.structuredContent
        pod = payload.pop("pod")
        stderr = payload.pop("stderr")
        assert pod.startswith("dear-baby-fixture-"), pod
        assert "no user found" in stderr, stderr
        assert payload == {
            "namespace": NAMESPACE,
            "email": "missing@example.com",
            "selector": "app=dear-baby",
            "container": "backend",
            "exitCode": 1,
            "stdout": "",
            "success": False,
        }, payload
        print("reset failure path ok")

        print("--- dear_baby_reset_user (no Running pod) ---")
        result = await session.call_tool(
            "dear_baby_reset_user",
            {
                "namespace": NAMESPACE,
                "email": "user@example.com",
                "selector": "app=does-not-exist",
            },
        )
        assert result.isError, result
        text = result.content[0].text
        assert "no Running pod matched" in text, text
        print("reset no-pod path ok")


if __name__ == "__main__":
    asyncio.run(run())
