"""Deployed-server e2e for github-app-installation-token/AC4 (private key not exposed).

검증 AC: github-app-installation-token/AC4
실행 대상: primary

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _helpers import base_url, open_session, parse_env_resource, wait_for_healthz


# Must match GITHUB_APP_INSTALLATION_ID in the CI "Create test GitHub App secret"
# step, which is the installation id the mock embeds in the issued token.
EXPECTED_INSTALLATION_ID = "67890"


async def test_github_app_installation_token_ac4_private_key_not_exposed(
    session: ClientSession,
) -> None:
    """AC: github-app-installation-token/AC4 — the App private key is not exposed.

    The private key is the server-only credential the whole tool exists to keep
    server-side; what may cross the wire is the expiring installation token and
    nothing else. This scans the *entire* serialized tool result (content blocks
    and structured payload alike, not just the .env text) for any trace of key
    material: PEM armour, the PKCS#1/PKCS#8 body markers, and the name of the env
    var that holds it. Also asserts the signed App JWT itself does not ride along
    in the response — it is a bearer credential for the App, not for the caller.

    The key bytes themselves are generated per CI run (``openssl genrsa`` in the
    "Create test GitHub App secret" step), so the test cannot compare against a
    known value; the armour markers are what make any leak of a PEM-encoded key
    detectable regardless of its content.
    """
    result = await session.call_tool("github_app_installation_token", {})
    assert result.isError is False, result

    serialized = result.model_dump_json()
    for forbidden in (
        "-----BEGIN",
        "-----END",
        "PRIVATE KEY",
        "RSA PRIVATE",
        "GITHUB_APP_PRIVATE_KEY",
    ):
        assert forbidden not in serialized, (
            f"{forbidden!r} leaked into the github_app_installation_token "
            f"response: {serialized}"
        )
    # A signed App JWT is a compact JWS ("eyJ..."); only the installation token
    # may be handed out.
    assert "eyJ" not in serialized, serialized

    env_text, _ = parse_env_resource(result)
    assert f"GITHUB_TOKEN=ghs_mock_{EXPECTED_INSTALLATION_ID}" in env_text, env_text


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github_app_installation_token (AC: github-app-installation-token/AC4) ---")
        await test_github_app_installation_token_ac4_private_key_not_exposed(session)
        print("ok: github-app-installation-token/AC4")


if __name__ == "__main__":
    asyncio.run(run())
