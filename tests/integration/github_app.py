"""End-to-end checks for github_app_installation_token against the mock.

Per-AC case names + docstrings declare the AC they verify (registry rule 3);
``docs/doc-tracker.md`` is the AC<->case mapping SSOT.
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _helpers import base_url, open_session, wait_for_healthz

# Must match GITHUB_APP_INSTALLATION_ID in the CI "Create test GitHub App secret"
# step, which is the installation id the mock embeds in the issued token.
EXPECTED_INSTALLATION_ID = "67890"


def parse_env_resource(result) -> tuple[str, str]:
    """Extract (env_text, mime_type) from a tool result's embedded resource."""
    assert result.content, result
    block = result.content[0]
    assert block.type == "resource", block
    resource = block.resource
    return resource.text, resource.mimeType


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


async def test_github_app_installation_token_ac2_scope_restriction(
    session: ClientSession,
) -> None:
    """AC: github-app-installation-token/AC2 — requested scope narrows the token.

    Asserts both branches the AC names. Unscoped: with neither ``repositories``
    nor ``permissions`` the token is issued for the whole installation
    (``Repository selection: all``) with the installation's default permissions.
    Scoped: passing one repository and ``contents=read`` narrows the issued
    token to that subset (``Repository selection: selected`` and
    ``Permissions: contents=read`` — the default ``metadata=read`` is gone).

    The AC's "설치 범위를 벗어난 요청은 거부된다" clause is GitHub-side behaviour;
    the mock echoes whatever scope it is handed, so rejection of an
    out-of-installation repository is not observable against this fixture.
    """
    unscoped = await session.call_tool("github_app_installation_token", {})
    assert unscoped.isError is False, unscoped
    env_text, _ = parse_env_resource(unscoped)
    assert "# Repository selection: all" in env_text, env_text
    assert "# Permissions: contents=read, metadata=read" in env_text, env_text

    scoped = await session.call_tool(
        "github_app_installation_token",
        {
            "repositories": ["homelab-k3s-mcp"],
            "permissions": {"contents": "read"},
        },
    )
    assert scoped.isError is False, scoped
    env_text, _ = parse_env_resource(scoped)
    assert f"GITHUB_TOKEN=ghs_mock_{EXPECTED_INSTALLATION_ID}" in env_text, env_text
    assert "# Repository selection: selected" in env_text, env_text
    assert "# Permissions: contents=read" in env_text, env_text
    assert "metadata=read" not in env_text, env_text


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
        for label, case in (
            (
                "github-app-installation-token/AC1",
                test_github_app_installation_token_ac1_short_lived_token,
            ),
            (
                "github-app-installation-token/AC2",
                test_github_app_installation_token_ac2_scope_restriction,
            ),
            (
                "github-app-installation-token/AC4",
                test_github_app_installation_token_ac4_private_key_not_exposed,
            ),
        ):
            print(f"--- github_app_installation_token (AC: {label}) ---")
            await case(session)
            print(f"ok: {label}")


if __name__ == "__main__":
    asyncio.run(run())
