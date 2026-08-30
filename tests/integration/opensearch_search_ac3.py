"""Deployed-server e2e for opensearch-search/AC3 (AssumeRole·SigV4 접근).

검증 AC: opensearch-search/AC3
실행 대상: primary
추가 인자: trace

도구 응답이 아니라 http-trace 프록시의 기록을 읽는다 — 픽스처가 무서명 검색도
똑같이 답하므로 응답으로는 접근 경로를 구분할 수 없다.

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
    REGION, ROLE_ARN, SIGNING_SERVICE, index_for, put_doc, search, token_for,
)


async def test_opensearch_search_ac3_assume_role_sigv4(session, trace) -> None:
    """AC: opensearch-search/AC3 — the search request is SigV4-signed with assumed-role credentials.

    Reads the http-trace recording rather than the tool response. The fixture
    runs with the security plugin disabled, so it would answer an unsigned
    search exactly the same way — the response cannot distinguish the access
    path, only the recording can.

    Asserts the whole chain: an AssumeRole for the configured role ARN was
    issued (signed with the base credential), and the ``_search`` that followed
    carried an ``AWS4-HMAC-SHA256`` header scoped to ``aoss`` in this region,
    signed with a key STS handed back — never the static base key — and with a
    session token attached.
    """
    index = index_for("searchac3")
    await put_doc(session, index, {"title": f"sigv4 probe {token_for('searchac3')}"})
    await search(session, token_for("searchac3"), index=index)

    record = assert_assumed_role_access(
        fetch_trace(trace),
        role_arn=ROLE_ARN,
        upstream="opensearch",
        method="POST",
        path=f"/{index}/_search",
        service=SIGNING_SERVICE,
        region=REGION,
    )
    # The payload hash has to be signed too, or the body could be swapped in
    # flight without invalidating the signature.
    assert "x-amz-content-sha256" in record["sigv4"]["signedHeaders"], record
    print(
        "opensearch_search assume-role SigV4 ok ->",
        f"POST {record['path']} signed by {record['sigv4']['accessKeyId']}"
        f" ({record['sigv4']['service']}/{record['sigv4']['region']})",
    )


async def run() -> None:
    url = base_url()
    trace = trace_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- opensearch-search/AC3 ---")
        await test_opensearch_search_ac3_assume_role_sigv4(session, trace)
        print("ok: opensearch-search/AC3")


if __name__ == "__main__":
    asyncio.run(run())
