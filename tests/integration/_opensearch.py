"""OpenSearch e2e 픽스처의 공유 상수·문서 헬퍼 (매칭 단위가 아니다).

`_` 접두 파일이라 `run_all.py`·`check_ac_mapping.py` 모두 매칭 단위에서 제외한다
(AC를 주검증하지 않으므로 `검증 AC:` 선언도 갖지 않는다).

opensearch-{search,document-put,document-delete} 의 AC별 전용 파일들이 공유하는
것만 담는다. 각 전용 파일은 **자기 프로세스의** ``RUN_ID`` 로 자기 인덱스
``ci-<case>-<RUN_ID>`` 와 자기 질의 토큰을 만들므로, 파일끼리도 서로의 문서를 보지
못한다 — 실행 순서에 의존하지 않는다.

서버는 OPENSEARCH_ROLE_ARN 을 MinIO 의 STS 엔드포인트에 대해 assume 하고, 그 자격
증명으로 서명한(SigV4, service "aoss") 요청을 단일 노드 OpenSearch 픽스처
(tests/k8s/kind/opensearch.yaml, security 플러그인 비활성)로 보낸다.

문서는 refresh 후에야(픽스처에서 ~1s) 검색된다 — 검색 단정은 기대 상태가 나타날
때까지 폴링한다.
"""

from __future__ import annotations

import asyncio
import time
import uuid

# Must match the opensearch secret in .github/workflows/ci.yml.
ROLE_ARN = "arn:aws:iam::000000000000:role/ci-opensearch"
REGION = "us-east-1"

# OpenSearch Serverless signs with the "aoss" service name
# (internal/opensearch/opensearch.go).
SIGNING_SERVICE = "aoss"

# Fresh per process: every AC 파일이 자기 값을 뽑으므로 인덱스·질의 토큰이 파일
# 사이에서도 겹치지 않고, 따뜻한 픽스처에 대한 재실행도 묵은 문서를 보지 않는다.
RUN_ID = uuid.uuid4().hex[:8]
SEARCH_DEADLINE_SECONDS = 60.0

def index_for(case: str) -> str:
    """Index owned by one case: never written to by any other case or run."""
    return f"ci-{case}-{RUN_ID}"

def token_for(case: str) -> str:
    """Query token owned by one case.

    Alphanumeric on purpose: the standard analyzer splits on everything else, so
    a token like ``searchac1a1b2c3d4`` stays a single term that no other case's
    documents contain. That is what makes the unscoped (collection-wide)
    searches deterministic while every case seeds documents concurrently.
    """
    return f"{case}{RUN_ID}"

def structured(result):
    assert result.isError is False, result
    assert result.structuredContent is not None, result
    return result.structuredContent

async def put_doc(session, index, document, doc_id=None):
    args = {"index": index, "document": document}
    if doc_id is not None:
        args["id"] = doc_id
    return structured(await session.call_tool("opensearch_document_put", args))

async def delete_doc(session, index, doc_id):
    return structured(
        await session.call_tool(
            "opensearch_document_delete", {"index": index, "id": doc_id}
        )
    )

async def search(session, query, index=None, size=None):
    args = {"query": query}
    if index is not None:
        args["index"] = index
    if size is not None:
        args["size"] = size
    return structured(await session.call_tool("opensearch_search", args))

async def search_until(session, query, predicate, description, index=None, size=None):
    """Poll search until predicate(hits) holds (documents surface on refresh)."""
    deadline = time.monotonic() + SEARCH_DEADLINE_SECONDS
    last = None
    while time.monotonic() < deadline:
        last = await search(session, query, index=index, size=size)
        if predicate(last["hits"]):
            return last
        await asyncio.sleep(1)
    raise AssertionError(f"search never converged: {description} (last: {last})")

def hit_ids(hits):
    return {hit["id"] for hit in hits}
