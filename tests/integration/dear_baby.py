"""End-to-end checks for the dear_baby_reset_onboarding tool."""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "dear-baby-test"


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- dear_baby_reset_onboarding (success path) ---")
        result = await session.call_tool(
            "dear_baby_reset_onboarding",
            {"namespace": NAMESPACE, "email": "user@example.com"},
        )
        assert not result.isError, result
        payload = result.structuredContent
        assert payload["namespace"] == NAMESPACE, payload
        assert payload["email"] == "user@example.com", payload
        assert payload["selector"] == "app=dear-baby", payload
        assert payload["container"] == "backend", payload
        assert payload["exitCode"] == 0, payload
        assert payload["success"] is True, payload
        assert payload["pod"].startswith("dear-baby-fixture-"), payload
        assert "reset onboarding for user@example.com" in payload["stdout"], payload
        print("reset ok against pod", payload["pod"])

        print("--- dear_baby_reset_onboarding (failure path) ---")
        result = await session.call_tool(
            "dear_baby_reset_onboarding",
            {"namespace": NAMESPACE, "email": "missing@example.com"},
        )
        assert result.isError, result
        payload = result.structuredContent
        assert payload["exitCode"] == 1, payload
        assert payload["success"] is False, payload
        assert "no user found" in payload["stderr"], payload
        print("reset failure path ok")

        print("--- dear_baby_reset_onboarding (no Running pod) ---")
        result = await session.call_tool(
            "dear_baby_reset_onboarding",
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
