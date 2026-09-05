"""Deployed-server e2e for opensearch-search/AC2 (결과 상한).

검증 AC: opensearch-search/AC2
실행 대상: primary

기본값과 무제한을 구분하려면 기본 상한보다 많은 문서가 있어야 하므로 자기 인덱스에
충분한 수를 시드한 뒤 관측한다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _opensearch import hit_ids, index_for, put_doc, search, search_until, token_for


async def test_opensearch_search_ac2_size_default_and_cap(session) -> None:
    """AC: opensearch-search/AC2 — size defaults to 10, allows 50, rejects 51.

    Seeds 12 matching documents because the default is otherwise invisible: with
    ten or fewer documents a size-less search returns everything and "defaults to
    10" cannot be told apart from "no limit at all". ``total`` staying 12 while
    only 10 hits come back is what shows the default caps the returned page
    rather than the match set.
    """
    index = index_for("search-ac2")
    token = token_for("searchac2")

    seeded = set()
    for n in range(12):
        put = await put_doc(
            session, index, {"title": f"scaling note {n} {token}"}, doc_id=f"doc-{n}"
        )
        assert put["result"] == "created", put
        seeded.add(put["id"])
    assert len(seeded) == 12, seeded

    full = await search_until(
        session,
        token,
        lambda hits: hit_ids(hits) == seeded,
        "all 12 seeded documents are searchable",
        index=index,
        size=50,
    )
    assert full["total"] == 12, full

    defaulted = await search(session, token, index=index)
    assert len(defaulted["hits"]) == 10, defaulted
    assert defaulted["total"] == 12, defaulted
    assert hit_ids(defaulted["hits"]) <= seeded, defaulted

    oversize = await session.call_tool(
        "opensearch_search", {"query": token, "index": index, "size": 51}
    )
    assert oversize.isError is True, oversize
    assert "size must be <= 50" in oversize.content[0].text, oversize
    print("opensearch_search size default/cap ok ->", index)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- opensearch-search/AC2 ---")
        await test_opensearch_search_ac2_size_default_and_cap(session)
        print("ok: opensearch-search/AC2")


if __name__ == "__main__":
    asyncio.run(run())
