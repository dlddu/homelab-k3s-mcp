"""Deployed-server e2e for opensearch-document-delete/AC4 (AssumeRole·SigV4 접근).

검증 AC: opensearch-document-delete/AC4
실행 대상: primary
추가 인자: trace

도구 응답이 아니라 http-trace 프록시의 기록을 읽는다(opensearch-document-put/AC4와
같은 이유 — security 플러그인이 꺼진 픽스처는 무서명 요청도 그대로 받아준다).

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from _helpers import (
    assert_assumed_role_access, base_url, fetch_trace, open_session, trace_url,
    wait_for_healthz,
)
from _opensearch import (
    REGION, ROLE_ARN, SIGNING_SERVICE, delete_doc, index_for, put_doc, token_for,
)


async def test_opensearch_document_delete_ac4_assume_role_sigv4(session, trace) -> None:
    """AC: opensearch-document-delete/AC4 — the delete is SigV4-signed with assumed-role credentials.

    Same reasoning as opensearch-search/AC3. The document is seeded first so
    the recorded ``DELETE /<index>/_doc/<id>`` is a real deletion rather than a
    not_found, keeping the observed request the one the AC is about.
    """
    index = index_for("deleteac4")
    doc_id = "sigv4-delete"
    await put_doc(
        session,
        index,
        {"title": f"sigv4 probe {token_for('deleteac4')}"},
        doc_id=doc_id,
    )
    deleted = await delete_doc(session, index, doc_id)
    assert deleted["result"] == "deleted", deleted

    record = assert_assumed_role_access(
        fetch_trace(trace),
        role_arn=ROLE_ARN,
        upstream="opensearch",
        method="DELETE",
        path=f"/{index}/_doc/{doc_id}",
        service=SIGNING_SERVICE,
        region=REGION,
    )
    print(
        "opensearch_document_delete assume-role SigV4 ok ->",
        f"DELETE {record['path']} signed by {record['sigv4']['accessKeyId']}"
        f" ({record['sigv4']['service']}/{record['sigv4']['region']})",
    )


async def run() -> None:
    url = base_url()
    trace = trace_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- opensearch-document-delete/AC4 ---")
        await test_opensearch_document_delete_ac4_assume_role_sigv4(session, trace)
        print("ok: opensearch-document-delete/AC4")


if __name__ == "__main__":
    asyncio.run(run())
