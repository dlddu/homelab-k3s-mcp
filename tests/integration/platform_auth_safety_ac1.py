"""인증 게이트: 인증 없는 /mcp 는 MCP 핸들러에 닿기 전에 401 로 막힌다 (e2e).

검증 AC: platform-auth-safety/AC1
실행 대상: auth-variant

`tests/integration/check_ac_mapping.py`가 이 선언을 읽어 `docs/doc-tracker.md`의 레지스트리와
대조하고, `tests/integration/run_all.py`가 `실행 대상`을 읽어 이 파일을 배차한다.

This runs against the deployment variant in ``tests/k8s/kind/auth-fixture.yaml``:
auth is on (``MCP_API_KEYS`` set, ``MCP_AUTH_DISABLED`` unset) and no credential
secret is attached at all, so ``main.go``'s ``build*Service`` helpers each degrade
to ``NewUnavailable("")`` while the server still starts. Sessions therefore carry
the static key from ``_auth_variant.API_KEY``.
주 배포는 ``MCP_AUTH_DISABLED=1`` 로 돌아 게이트 자체를 관측할 수 없다 — 그래서 이 변형이 있다.
"""

from __future__ import annotations

import asyncio

import httpx

from _helpers import base_url, wait_for_healthz


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


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    await test_platform_auth_safety_ac1_gate(url)


if __name__ == "__main__":
    asyncio.run(run())
