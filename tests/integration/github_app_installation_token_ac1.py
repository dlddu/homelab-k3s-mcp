"""Deployed-server e2e for github-app-installation-token/AC1 (short-lived token).

검증 AC: github-app-installation-token/AC1
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


async def test_github_app_installation_token_ac1_short_lived_token(
    session: ClientSession,
) -> None:
    """AC: github-app-installation-token/AC1 — a short-lived installation token.

    Drives the whole exchange the AC describes against the deployed server: it
    signs an App JWT with the configured private key and posts it to the mock's
    ``/app/installations/<id>/access_tokens``. The returned token
    ``ghs_mock_<installation id>`` is proof the exchange actually happened and
    was authenticated (the mock 401s a request without a Bearer JWT and 400s one
    missing the GitHub API version / Accept headers), and the payload is the
    .env form the AC requires, carrying the expiry and scope comments.

    The "~1 hour" half of the criterion is not asserted here: the mock returns a
    fixed far-future ``expires_at`` (2099-01-01), so no real TTL is observable at
    this layer. The server-side clock is covered by the Go unit tests in
    ``internal/github``; this case asserts the expiry comment is present and
    carries the value the token endpoint returned.
    """
    result = await session.call_tool("github_app_installation_token", {})
    assert result.isError is False, result
    assert result.structuredContent is None, result.structuredContent

    env_text, mime = parse_env_resource(result)
    assert mime == "text/plain", mime
    assert f"GITHUB_TOKEN=ghs_mock_{EXPECTED_INSTALLATION_ID}" in env_text, env_text
    # Expiry comment, carrying exactly what the token endpoint returned.
    assert "# Expires at: 2099-01-01T00:00:00Z" in env_text, env_text
    # Scope comments accompany the token in the same payload.
    assert "# Repository selection: " in env_text, env_text
    assert "# Permissions: " in env_text, env_text


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github_app_installation_token (AC: github-app-installation-token/AC1) ---")
        await test_github_app_installation_token_ac1_short_lived_token(session)
        print("ok: github-app-installation-token/AC1")


if __name__ == "__main__":
    asyncio.run(run())
