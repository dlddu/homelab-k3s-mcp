"""opensearch_document_put: OPENSEARCH_ENDPOINT 미설정 시 graceful 거부 (e2e).

검증 AC: opensearch-document-put/AC5
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
    OPENSEARCH_REFUSAL,
    assert_unavailable_refusal,
)
from _helpers import base_url, open_session, wait_for_healthz


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


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        print("--- unconfigured graceful refusal (AC: opensearch-document-put/AC5) ---")
        await test_opensearch_document_put_ac5_unconfigured_refusal(session)
        print("refusal ok: opensearch-document-put/AC5")


if __name__ == "__main__":
    asyncio.run(run())
