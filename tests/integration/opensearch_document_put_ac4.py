"""Deployed-server e2e for opensearch-document-put/AC4 (AssumeRole·SigV4 접근).

검증 AC: opensearch-document-put/AC4
실행 대상: primary
추가 인자: trace

도구 응답이 아니라 ``tests/k8s/kind/http-trace.yaml`` 프록시의 기록을 읽는다.
AssumeRole 레코드는 서버가 자격증명을 캐시해 한 번만 발급되지만 프록시가 그
레코드만은 evict 하지 않으므로, 파일이 나뉘어도 상관 관계 대조가 성립한다.

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
from _opensearch import REGION, ROLE_ARN, SIGNING_SERVICE, index_for, put_doc, token_for


async def test_opensearch_document_put_ac4_assume_role_sigv4(session, trace) -> None:
    """AC: opensearch-document-put/AC4 — the index write is SigV4-signed with assumed-role credentials.

    Same reasoning as opensearch-search/AC3: the security-disabled fixture
    accepts an unsigned write just as happily, so the assertion has to come
    from the http-trace recording of the write itself (``PUT /<index>/_doc/<id>``)
    rather than from the ``created`` result.
    """
    index = index_for("putac4")
    doc_id = "sigv4-put"
    await put_doc(
        session, index, {"title": f"sigv4 probe {token_for('putac4')}"}, doc_id=doc_id
    )

    record = assert_assumed_role_access(
        fetch_trace(trace),
        role_arn=ROLE_ARN,
        upstream="opensearch",
        method="PUT",
        path=f"/{index}/_doc/{doc_id}",
        service=SIGNING_SERVICE,
        region=REGION,
    )
    assert "x-amz-content-sha256" in record["sigv4"]["signedHeaders"], record
    print(
        "opensearch_document_put assume-role SigV4 ok ->",
        f"PUT {record['path']} signed by {record['sigv4']['accessKeyId']}"
        f" ({record['sigv4']['service']}/{record['sigv4']['region']})",
    )


async def run() -> None:
    url = base_url()
    trace = trace_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- opensearch-document-put/AC4 ---")
        await test_opensearch_document_put_ac4_assume_role_sigv4(session, trace)
        print("ok: opensearch-document-put/AC4")


if __name__ == "__main__":
    asyncio.run(run())
