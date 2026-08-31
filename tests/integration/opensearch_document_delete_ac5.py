"""opensearch_document_delete: OPENSEARCH_ENDPOINT 미설정 시 graceful 거부 (e2e).

검증 AC: opensearch-document-delete/AC5
실행 대상: auth-variant

`tests/integration/check_ac_mapping.py`가 이 선언을 읽어 `docs/doc-tracker.md`의 레지스트리와
대조하고, `tests/integration/run_all.py`가 `실행 대상`을 읽어 이 파일을 배차한다.

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
        print("--- unconfigured graceful refusal (AC: opensearch-document-delete/AC5) ---")
        await test_opensearch_document_delete_ac5_unconfigured_refusal(session)
        print("refusal ok: opensearch-document-delete/AC5")


if __name__ == "__main__":
    asyncio.run(run())
