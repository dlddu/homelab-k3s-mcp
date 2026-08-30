"""Deployed-server e2e for opensearch-search/AC1 (질의 검색).

검증 AC: opensearch-search/AC1
실행 대상: primary

질의 토큰이 이 프로세스의 ``RUN_ID`` 를 포함하는 단일 텀이라, 인덱스를 지정하지
않는 컬렉션 전체 검색도 다른 파일의 문서와 겹치지 않는다.

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio
import json

from _helpers import base_url, open_session, wait_for_healthz
from _opensearch import hit_ids, index_for, put_doc, search, search_until, token_for


async def test_opensearch_search_ac1_query_matching(session) -> None:
    """AC: opensearch-search/AC1 — matching documents only, with index/id/score/source.

    Seeds two indexes so both halves of the AC are observable: the unscoped call
    spans the collection (both indexes answer) while ``index`` narrows it to one,
    and a non-matching document seeded alongside the matches is what proves the
    query — not merely the index scope — is doing the filtering.
    """
    index_a = index_for("search-ac1-a")
    index_b = index_for("search-ac1-b")
    token = token_for("searchac1")
    other_token = token_for("searchac1other")

    await put_doc(
        session,
        index_a,
        {"title": f"apiserver certificate rotation {token}", "body": "rotate certs"},
        doc_id="match-a",
    )
    await put_doc(
        session,
        index_a,
        {"title": f"unrelated grafana note {other_token}"},
        doc_id="other-a",
    )
    await put_doc(
        session,
        index_b,
        {"title": f"etcd certificate rotation {token}"},
        doc_id="match-b",
    )

    across = await search_until(
        session,
        token,
        lambda hits: hit_ids(hits) == {"match-a", "match-b"},
        "both indexes answer the collection-wide query",
    )
    assert across["total"] == 2, across
    assert across["index"] is None, across
    for hit in across["hits"]:
        assert hit["index"] in {index_a, index_b}, hit
        assert hit["id"], hit
        assert hit["score"] is not None, hit
        assert token in json.dumps(hit["source"]), hit

    scoped = await search(session, token, index=index_a)
    assert hit_ids(scoped["hits"]) == {"match-a"}, scoped
    assert scoped["index"] == index_a, scoped
    assert scoped["hits"][0]["index"] == index_a, scoped

    # The non-matching document is in index_a and searchable, so its absence
    # from the queries above is the query filtering, not a missing document.
    other = await search_until(
        session,
        other_token,
        lambda hits: hit_ids(hits) == {"other-a"},
        "the non-matching document is searchable under its own token",
        index=index_a,
    )
    assert other["hits"][0]["index"] == index_a, other
    print("opensearch_search query matching ok ->", index_a, "/", index_b)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- opensearch-search/AC1 ---")
        await test_opensearch_search_ac1_query_matching(session)
        print("ok: opensearch-search/AC1")


if __name__ == "__main__":
    asyncio.run(run())
