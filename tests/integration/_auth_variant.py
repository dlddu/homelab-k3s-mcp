"""auth-variant 배포를 상대로 도는 파일들의 공유 표면 (매칭 단위가 아니다).

This runs against the deployment variant in ``tests/k8s/kind/auth-fixture.yaml``:
auth is on (``MCP_API_KEYS`` set, ``MCP_AUTH_DISABLED`` unset) and no credential
secret is attached at all, so ``main.go``'s ``build*Service`` helpers each degrade
to ``NewUnavailable("")`` while the server still starts. Sessions therefore carry
the static key from ``_auth_variant.API_KEY``.

`API_KEY` 는 이 변형을 상대로 도는 모든 파일이 같은 값을 써야 하므로 여기 한 벌만 둔다
(분할 전에는 `auth.py` 와 `no_config.py` 가 같은 리터럴을 따로 들고 있었다).
"""

from __future__ import annotations

from mcp import ClientSession

# Must match MCP_API_KEYS in tests/k8s/kind/auth-fixture.yaml (the variant
# carries a single static key).
API_KEY = "ci-e2e-key"

# The exact refusal texts, from the default reasons in NewUnavailable() and the
# Error() prefixes of each package (internal/{awsconfig,github,grafana,
# opensearch,sessionplatform}). An empty reason is what main.go passes when the
# primary env var is unset, which is precisely the situation this fixture
# reproduces. K8S_REFUSAL is the exception: kubernetes reads an in-cluster
# ServiceAccount rather than a secret, so the fixture sets MCP_K8S_DISABLED and
# main.go passes an explicit reason for it instead of an empty one.
AWS_REFUSAL = "aws config unavailable: aws config integration is not configured"
GITHUB_REFUSAL = "github app unavailable: github app credentials are not configured"
GRAFANA_REFUSAL = (
    "grafana cloud unavailable: grafana cloud credentials are not configured"
)
OPENSEARCH_REFUSAL = "opensearch unavailable: opensearch integration is not configured"
SESSION_PLATFORM_REFUSAL = (
    "session platform unavailable: session platform endpoint is not configured"
)
K8S_REFUSAL = "kubernetes client unavailable: kubernetes integration is disabled"


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
