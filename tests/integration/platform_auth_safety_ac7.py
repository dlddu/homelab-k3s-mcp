"""API 키 인증: 모르는 키는 401, 구성된 정적 키는 인가된다 (e2e).

검증 AC: platform-auth-safety/AC7
실행 대상: auth-variant

`tests/integration/check_ac_mapping.py`가 이 선언을 읽어 `docs/doc-tracker.md`의 레지스트리와
대조하고, `tests/integration/run_all.py`가 `실행 대상`을 읽어 이 파일을 배차한다.

This runs against the deployment variant in ``tests/k8s/kind/auth-fixture.yaml``:
auth is on (``MCP_API_KEYS`` set, ``MCP_AUTH_DISABLED`` unset) and no credential
secret is attached at all, so ``main.go``'s ``build*Service`` helpers each degrade
to ``NewUnavailable("")`` while the server still starts. Sessions therefore carry
the static key from ``_auth_variant.API_KEY``.
"""

from __future__ import annotations

import asyncio

import httpx

from _auth_variant import API_KEY
from _helpers import base_url, open_session, wait_for_healthz


async def test_platform_auth_safety_ac7_api_key(url: str) -> None:
    """AC: platform-auth-safety/AC7

    A request bearing an unknown key is rejected 401 (``invalid_token``); a
    request bearing the configured static key authorizes and reaches the MCP
    handler, so tools/list succeeds. No response leaks the key value.
    """
    print("--- api key auth: unknown key rejected (AC: platform-auth-safety/AC7) ---")
    bad = httpx.post(
        f"{url}/mcp",
        json={"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}},
        headers={
            "Content-Type": "application/json",
            "Authorization": "Bearer not-a-real-key",
        },
        timeout=5.0,
    )
    assert bad.status_code == 401, (
        f"unknown-key /mcp returned {bad.status_code}, expected 401"
    )
    assert "invalid_token" in bad.text, f"unexpected 401 body: {bad.text!r}"
    assert API_KEY not in bad.text, "401 response leaked the configured API key"
    print("api key auth ok: unknown key -> 401 invalid_token")

    print("--- api key auth: valid key authorizes (AC: platform-auth-safety/AC7) ---")
    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        tools = await session.list_tools()
        names = {tool.name for tool in tools.tools}
        assert "ping" in names, f"authorized tools/list missing ping: {sorted(names)}"
    print("api key auth ok: valid key -> tools/list authorized")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    await test_platform_auth_safety_ac7_api_key(url)


if __name__ == "__main__":
    asyncio.run(run())
