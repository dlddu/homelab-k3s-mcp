"""Auth-gate e2e: platform-auth-safety AC1 (auth gate) + AC7 (API key auth).

검증 AC: platform-auth-safety/AC1, platform-auth-safety/AC7
실행 대상: auth-variant

**분할 대기(2 AC 겸용)** — 모델 `tbm_homelab-k3s-mcp-ac-e2e`의 파일 단위 규칙 2는
파일 하나가 AC 하나만 주검증할 것을 요구하므로, 이 파일은 AC별 전용 파일로 쪼개질 대상이다.
위 선언은 현재 겸용 상태를 있는 그대로 신고하는 것이고,
`tests/integration/check_ac_mapping.py`가 이를 규칙 2 위반으로 계수해 `docs/doc-tracker.md`와 대조한다.

These run against the dedicated auth-enabled deployment variant
(``tests/k8s/kind/auth-fixture.yaml``), where ``MCP_AUTH_DISABLED`` is unset and
``MCP_API_KEYS`` carries a single static key, so ``/mcp`` is gated. The primary
CI deployment runs with ``MCP_AUTH_DISABLED=1`` and therefore cannot observe the
gate at all; that is why a separate instance exists.

Per-AC case names + docstrings declare the AC they verify (registry rule 3);
``docs/doc-tracker.md`` is the AC<->case mapping SSOT.
"""

from __future__ import annotations

import asyncio

import httpx

from _helpers import base_url, open_session, wait_for_healthz

# Must match MCP_API_KEYS in tests/k8s/kind/auth-fixture.yaml.
API_KEY = "ci-e2e-key"


async def test_platform_auth_safety_ac1_gate(url: str) -> None:
    """AC: platform-auth-safety/AC1

    An unauthenticated POST /mcp is rejected 401 before reaching the MCP
    handler. In API-key-only mode the challenge body is the bare error code
    ``missing_token`` (the resource_metadata form of WWW-Authenticate is only
    emitted when OAuth is also configured).
    """
    print("--- auth gate: unauthenticated /mcp (AC: platform-auth-safety/AC1) ---")
    response = httpx.post(
        f"{url}/mcp",
        json={"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}},
        headers={"Content-Type": "application/json"},
        timeout=5.0,
    )
    assert response.status_code == 401, (
        f"unauthenticated /mcp returned {response.status_code}, expected 401"
    )
    assert "missing_token" in response.text, f"unexpected 401 body: {response.text!r}"
    assert response.headers.get("WWW-Authenticate", "").startswith("Bearer"), (
        f"missing/blank WWW-Authenticate: "
        f"{response.headers.get('WWW-Authenticate')!r}"
    )
    print("auth gate ok: unauthenticated /mcp -> 401 missing_token")


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
    await test_platform_auth_safety_ac1_gate(url)
    await test_platform_auth_safety_ac7_api_key(url)


if __name__ == "__main__":
    asyncio.run(run())
