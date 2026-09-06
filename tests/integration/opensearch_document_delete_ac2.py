"""Deployed-server e2e for opensearch-document-delete/AC2 (부재 문서의 명확한 처리).

검증 AC: opensearch-document-delete/AC2
실행 대상: primary

부재를 오류가 아닌 결과로 답하는지, 그리고 그 뒤에도 서버가 정상인지(``ping``)를
함께 본다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _opensearch import delete_doc, index_for, put_doc, token_for


async def test_opensearch_document_delete_ac2_missing_document_not_found(
    session,
) -> None:
    """AC: opensearch-document-delete/AC2 — a missing document answers not_found.

    Covers both flavours of "missing": an id in an index that was never created,
    and an id that existed until this case deleted it (the repeat the tool
    advertises as idempotent). The ``ping`` at the end is what turns the AC's
    second half — no effect on the server or the other tools — into an assertion
    instead of an assumption.
    """
    index = index_for("delete-ac2")
    token = token_for("deleteac2")

    never_written = await delete_doc(session, index, "never-written")
    assert never_written == {
        "index": index,
        "id": "never-written",
        "result": "not_found",
    }, never_written

    await put_doc(session, index, {"title": f"short-lived note {token}"}, doc_id="doc-1")
    first = await delete_doc(session, index, "doc-1")
    assert first == {"index": index, "id": "doc-1", "result": "deleted"}, first
    repeated = await delete_doc(session, index, "doc-1")
    assert repeated == {
        "index": index,
        "id": "doc-1",
        "result": "not_found",
    }, repeated

    pong = await session.call_tool("ping", {})
    assert pong.isError is False, pong
    assert pong.content[0].text == "pong", pong
    print("opensearch_document_delete not_found handling ok ->", index)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- opensearch-document-delete/AC2 ---")
        await test_opensearch_document_delete_ac2_missing_document_not_found(session)
        print("ok: opensearch-document-delete/AC2")


if __name__ == "__main__":
    asyncio.run(run())
