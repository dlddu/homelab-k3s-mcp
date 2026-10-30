"""OpenSearch e2e 픽스처의 공유 상수·문서 헬퍼 (매칭 단위가 아니다).

opensearch-{search,document-put,document-delete} 의 AC별 전용 파일들이 공유하는
것만 담는다. 각 전용 파일은 **자기 프로세스의** ``RUN_ID`` 로 자기 인덱스
``ci-<case>-<RUN_ID>`` 와 자기 질의 토큰을 만들므로, 파일끼리도 서로의 문서를 보지
못한다 — 실행 순서에 의존하지 않는다.

서버는 서명 없는 HTTP 요청을 http-trace 프록시를 거쳐 실 docstore 픽스처
(tests/k8s/kind/docstore.yaml, 운영과 같은 이미지)로 보낸다.

docstore 는 쓰기 직후 검색에 노출하지만, 검색 단정은 그 성질에 기대지 않고 기대
상태가 나타날 때까지 폴링한다(백엔드가 바뀌어도 단정이 깨지지 않게).
"""

from __future__ import annotations

import asyncio
import time
import uuid

# tests/k8s/kind/http-trace.yaml 의 TRACE_ROUTES 이름. docstore 로 가는 요청이 기록되는 곳이다.
TRACE_UPSTREAM = "docstore"

# 따뜻한 픽스처에 대한 재실행도 묵은 문서를 보지 않는다.
RUN_ID = uuid.uuid4().hex[:8]
SEARCH_DEADLINE_SECONDS = 60.0

def index_for(case: str) -> str:
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


def assert_unsigned_docstore_access(records, *, method, path):
    """``method path`` 가 docstore 업스트림에 서명·자격증명 없이 도달했음을 단정한다.

    도구 응답은 경로를 말해 주지 않는다 — 서명된 요청이었어도 docstore 는 똑같이 답한다.
    그래서 프록시 기록에서 그 요청을 찾아, ``Authorization`` 헤더도 세션 토큰도 없었음을
    본다. 반환값은 매칭된 기록(호출자가 더 단정할 수 있게).
    """
    matches = [
        r for r in records
        if r["upstream"] == TRACE_UPSTREAM and r["method"] == method and r["path"] == path
    ]
    assert matches, (
        f"no {method} {path} recorded against {TRACE_UPSTREAM} — the request never "
        f"reached the traced docstore endpoint"
    )
    record = matches[-1]
    assert record["authorization"] is False, (
        f"{method} {path} carried an Authorization header: {record}"
    )
    assert record["sigv4"] is None, record
    assert record["securityToken"] is False, (
        f"{method} {path} carried X-Amz-Security-Token: {record}"
    )
    assert 200 <= record["status"] < 300, record
    return record
