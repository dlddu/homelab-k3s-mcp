"""No-config e2e: server-level graceful degradation + the six per-tool refusals.

Everything here runs against the one deployment where "미설정" is true, so it
carries both the server-level AC (platform-auth-safety/AC5 — the server starts
and keeps advertising its tools with every integration unset) and the six
per-tool refusal ACs below.

Every credential-backed tool must answer an ``unavailable``-class error when its
server configuration is absent, without taking the server or the other tools
down with it (aws-config-get/AC3, github-app-installation-token/AC3,
grafana-token/AC3, opensearch-search/AC4, opensearch-document-put/AC5,
opensearch-document-delete/AC5).

The primary CI deployment wires up every credential secret, so that refusal path
cannot be observed there. These cases therefore run against the deployment
variant in ``tests/k8s/kind/auth-fixture.yaml``, which attaches no credential
secrets at all: with ``GITHUB_APP_CLIENT_ID`` / ``AWS_CONFIG_S3_BUCKET`` /
``GRAFANA_ISSUER_TOKEN`` / ``OPENSEARCH_ENDPOINT`` unset, ``main.go``'s
``build*Service`` helpers each degrade to ``NewUnavailable("")`` and the server
still starts. That variant gates ``/mcp`` behind ``MCP_API_KEYS``, so the
sessions here carry the same static key ``auth.py`` uses.

Per-AC case names + docstrings declare the AC they verify (registry rule 3);
``docs/doc-tracker.md`` is the AC<->case mapping SSOT.
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _helpers import base_url, get_json, open_session, wait_for_healthz

# Must match MCP_API_KEYS in tests/k8s/kind/auth-fixture.yaml (same value as
# tests/integration/auth.py — the variant carries a single static key).
API_KEY = "ci-e2e-key"

# The full tool surface the server must keep advertising even with every
# integration unconfigured (platform-auth-safety/AC5).
EXPECTED_TOOLS = {
    "ping",
    "namespace_list",
    "workload_list",
    "workload_restart",
    "workload_scale",
    "workload_logs",
    "pod_describe",
    "dear_baby_reset_user",
    "grafana_token",
    "github_app_installation_token",
    "aws_config_get",
    "opensearch_search",
    "opensearch_document_put",
    "opensearch_document_delete",
}

# The exact refusal texts, from the default reasons in NewUnavailable() and the
# Error() prefixes of each package (internal/{awsconfig,github,grafana,
# opensearch}). An empty reason is what main.go passes when the primary env var
# is unset, which is precisely the situation this fixture reproduces.
AWS_REFUSAL = "aws config unavailable: aws config integration is not configured"
GITHUB_REFUSAL = "github app unavailable: github app credentials are not configured"
GRAFANA_REFUSAL = (
    "grafana cloud unavailable: grafana cloud credentials are not configured"
)
OPENSEARCH_REFUSAL = "opensearch unavailable: opensearch integration is not configured"


async def assert_unavailable_refusal(
    session: ClientSession,
    tool_name: str,
    arguments: dict,
    expected_text: str,
) -> None:
    """Assert ``tool_name`` refuses gracefully while the server stays healthy.

    "Graceful" is the whole point of these ACs, so this checks both halves of
    the criterion: the call comes back as a normal MCP tool result carrying
    ``isError`` and the unavailable-class message (not a transport failure or a
    protocol error), and the server keeps serving afterwards — ``ping`` still
    answers ``pong`` on the same session, so neither the server nor the other
    tools were affected by the unconfigured one.
    """
    result = await session.call_tool(tool_name, arguments)
    assert result.isError is True, f"{tool_name} did not refuse: {result}"
    assert result.content, result
    block = result.content[0]
    assert block.type == "text", block
    assert block.text == expected_text, (
        f"{tool_name} refusal text = {block.text!r}, expected {expected_text!r}"
    )

    survivor = await session.call_tool("ping", {})
    assert survivor.isError is False, (
        f"ping broke after the {tool_name} refusal: {survivor}"
    )
    assert survivor.content and survivor.content[0].text == "pong", survivor


async def test_platform_auth_safety_ac5_graceful_degradation(
    url: str, session: ClientSession
) -> None:
    """AC: platform-auth-safety/AC5 — the server runs with integrations unset.

    This is the only deployment in the suite where the AC's premise holds. The
    primary kind deployment wires up every credential secret, so a healthy
    tools/list there says nothing about degradation; this variant attaches none
    of them (GITHUB_APP_CLIENT_ID / AWS_CONFIG_S3_BUCKET / GRAFANA_ISSUER_TOKEN /
    OPENSEARCH_ENDPOINT all unset), which is exactly the "자격증명 env를 비운 채
    기동" the verification method describes.

    Asserts both halves of that method against this pod: the server is up and
    answering its liveness probe, and tools/list still returns the complete tool
    surface — including every tool whose backing integration is unavailable.
    Unconfigured integrations degrade the tools' *results* (the per-tool cases
    below assert that), never the server's ability to start and advertise them.
    """
    healthz = get_json(url, "/healthz")
    assert healthz.get("status") == "ok", f"unexpected /healthz: {healthz!r}"

    tools = await session.list_tools()
    names = {tool.name for tool in tools.tools}
    missing = EXPECTED_TOOLS - names
    assert not missing, (
        f"tools/list degraded with integrations unset: missing {sorted(missing)} "
        f"(got {sorted(names)})"
    )


async def test_aws_config_get_ac3_unconfigured_refusal(session: ClientSession) -> None:
    """AC: aws-config-get/AC3

    With AWS_CONFIG_S3_BUCKET unset, aws_config_get returns the unavailable
    error instead of crashing, and the server keeps serving other tools.
    """
    await assert_unavailable_refusal(session, "aws_config_get", {}, AWS_REFUSAL)


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


async def test_grafana_token_ac3_unconfigured_refusal(session: ClientSession) -> None:
    """AC: grafana-token/AC3

    With GRAFANA_ISSUER_TOKEN unset, grafana_token returns the unavailable
    error rather than minting or leaking anything.
    """
    await assert_unavailable_refusal(session, "grafana_token", {}, GRAFANA_REFUSAL)


async def test_opensearch_search_ac4_unconfigured_refusal(
    session: ClientSession,
) -> None:
    """AC: opensearch-search/AC4

    With OPENSEARCH_ENDPOINT unset, opensearch_search returns the unavailable
    error. The query argument is valid, so the refusal is the configuration
    check and not the required-argument check.
    """
    await assert_unavailable_refusal(
        session, "opensearch_search", {"query": "anything"}, OPENSEARCH_REFUSAL
    )


async def test_opensearch_document_put_ac5_unconfigured_refusal(
    session: ClientSession,
) -> None:
    """AC: opensearch-document-put/AC5

    With OPENSEARCH_ENDPOINT unset, opensearch_document_put returns the
    unavailable error before touching any index.
    """
    await assert_unavailable_refusal(
        session,
        "opensearch_document_put",
        {"index": "no-config-probe", "document": {"probe": True}},
        OPENSEARCH_REFUSAL,
    )


async def test_opensearch_document_delete_ac5_unconfigured_refusal(
    session: ClientSession,
) -> None:
    """AC: opensearch-document-delete/AC5

    With OPENSEARCH_ENDPOINT unset, opensearch_document_delete returns the
    unavailable error before attempting any deletion.
    """
    await assert_unavailable_refusal(
        session,
        "opensearch_document_delete",
        {"index": "no-config-probe", "id": "no-config-probe-doc"},
        OPENSEARCH_REFUSAL,
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        print("--- server-level graceful degradation "
              "(AC: platform-auth-safety/AC5) ---")
        await test_platform_auth_safety_ac5_graceful_degradation(url, session)
        print("degradation ok: platform-auth-safety/AC5")

        for label, case in (
            ("aws-config-get/AC3", test_aws_config_get_ac3_unconfigured_refusal),
            (
                "github-app-installation-token/AC3",
                test_github_app_installation_token_ac3_unconfigured_refusal,
            ),
            ("grafana-token/AC3", test_grafana_token_ac3_unconfigured_refusal),
            ("opensearch-search/AC4", test_opensearch_search_ac4_unconfigured_refusal),
            (
                "opensearch-document-put/AC5",
                test_opensearch_document_put_ac5_unconfigured_refusal,
            ),
            (
                "opensearch-document-delete/AC5",
                test_opensearch_document_delete_ac5_unconfigured_refusal,
            ),
        ):
            print(f"--- unconfigured graceful refusal (AC: {label}) ---")
            await case(session)
            print(f"refusal ok: {label}")


if __name__ == "__main__":
    asyncio.run(run())
