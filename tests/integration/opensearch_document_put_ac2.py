"""Deployed-server e2e for opensearch-document-put/AC2 (인덱스 자동 생성).

검증 AC: opensearch-document-put/AC2
실행 대상: primary

색인 **전** 상태를 먼저 관측해야 "자동 생성"이 성립하므로, 자기 인덱스가 아직
없다는 것(404 ``index_not_found_exception``)을 선행 단정한다.

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _opensearch import hit_ids, index_for, put_doc, search_until, token_for


async def test_opensearch_document_put_ac2_index_auto_creation(session) -> None:
    """AC: opensearch-document-put/AC2 — writing to a missing index creates it.

    The pre-state is asserted, not assumed: searching the index *before* the
    write comes back as a tool error carrying OpenSearch's 404
    ``index_not_found_exception``. Without that step a passing search afterwards
    would be equally consistent with the index having existed all along, and the
    case would demonstrate nothing about auto-creation.
    """
    index = index_for("put-ac2")
    token = token_for("putac2")

    missing = await session.call_tool(
        "opensearch_search", {"query": token, "index": index}
    )
    assert missing.isError is True, missing
    refusal = missing.content[0].text
    assert "status 404" in refusal, refusal
    assert "index_not_found_exception" in refusal, refusal

    created = await put_doc(
        session, index, {"title": f"auto-created index {token}"}, doc_id="doc-1"
    )
    assert created == {"index": index, "id": "doc-1", "result": "created"}, created

    found = await search_until(
        session,
        token,
        lambda hits: hit_ids(hits) == {"doc-1"},
        f"{index} exists and holds the document after the write",
        index=index,
    )
    assert found["hits"][0]["index"] == index, found
    print("opensearch_document_put index auto-creation ok ->", index)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- opensearch-document-put/AC2 ---")
        await test_opensearch_document_put_ac2_index_auto_creation(session)
        print("ok: opensearch-document-put/AC2")


if __name__ == "__main__":
    asyncio.run(run())
