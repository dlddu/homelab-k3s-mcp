"""github_app_installation_token: GITHUB_APP_CLIENT_ID 미설정 시 graceful 거부 (e2e).

검증 AC: github-app-installation-token/AC3
실행 대상: auth-variant

This runs against the deployment variant in ``tests/k8s/kind/auth-fixture.yaml``:
auth is on (``MCP_API_KEYS`` set, ``MCP_AUTH_DISABLED`` unset) and no credential
secret is attached at all, so ``main.go``'s ``build*Service`` helpers each degrade
to ``NewUnavailable("")`` while the server still starts. Sessions therefore carry
the static key from ``_auth_variant.API_KEY``.

The shared assertion lives in ``_auth_variant.assert_unavailable_refusal``: it
checks both halves of the criterion — the call comes back as a normal MCP tool
result carrying ``isError`` and the unavailable-class text, and ``ping`` still
answers ``pong`` on the same session afterwards.
"""

from __future__ import annotations

import asyncio

from _auth_variant import (
    API_KEY,
    GITHUB_REFUSAL,
    assert_unavailable_refusal,
)
from _helpers import base_url, open_session, wait_for_healthz


async def test_github_app_installation_token_ac3_unconfigured_refusal(
    session: ClientSession,
) -> None:
    """AC: github-app-installation-token/AC3

    With GITHUB_APP_CLIENT_ID unset, token issuance returns the unavailable
    error. Arguments are well-formed so the refusal comes from the missing
    configuration rather than from argument validation.
    """
    await assert_unavailable_refusal(
        session,
        "github_app_installation_token",
        {"repositories": ["homelab-k3s-mcp"], "permissions": {"contents": "read"}},
        GITHUB_REFUSAL,
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        print("--- unconfigured graceful refusal (AC: github-app-installation-token/AC3) ---")
        await test_github_app_installation_token_ac3_unconfigured_refusal(session)
        print("refusal ok: github-app-installation-token/AC3")


if __name__ == "__main__":
    asyncio.run(run())
