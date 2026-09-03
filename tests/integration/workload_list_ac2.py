"""Deployed-server e2e for workload-list/AC2 (네임스페이스 스코프).

검증 AC: workload-list/AC2
실행 대상: primary

스코프 목록에 ``deploy/workload-fixture`` 가 있어야 하므로 픽스처 기준선을
선행 조건으로 성립시킨다.

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE, SERVER_NAMESPACE, WORKLOAD, ensure_workload_fixture_baseline


async def test_workload_list_ac2_namespace_scope(session) -> None:
    """AC: workload-list/AC2 — namespace narrows the listing, omitting it widens it.

    The scoped call must return *only* that namespace's workloads (asserted
    over every item, not just the fixture), and the unscoped call must reach
    workloads the scoped one cannot see — the server's own Deployment in
    another namespace.
    """
    scoped = await session.call_tool(
        "workload_list", {"kind": "Deployment", "namespace": NAMESPACE}
    )
    assert scoped.isError is False, scoped
    payload = scoped.structuredContent
    items = payload.pop("items")
    assert payload == {"kind": "Deployment", "namespace": NAMESPACE}, payload
    names = [item["name"] for item in items]
    assert WORKLOAD in names, names
    for item in items:
        assert item["namespace"] == NAMESPACE, item
    assert (SERVER_NAMESPACE, SERVER_NAMESPACE) not in {
        (i["namespace"], i["name"]) for i in items
    }, items
    print("scoped list ok:", names)

    unscoped = await session.call_tool("workload_list", {"kind": "Deployment"})
    assert unscoped.isError is False, unscoped
    payload = unscoped.structuredContent
    items = payload.pop("items")
    assert payload == {"kind": "Deployment", "namespace": None}, payload
    pairs = {(i["namespace"], i["name"]) for i in items}
    assert (NAMESPACE, WORKLOAD) in pairs, pairs
    assert (SERVER_NAMESPACE, SERVER_NAMESPACE) in pairs, pairs
    print("unscoped list ok:", len(items), "items")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    ensure_workload_fixture_baseline()

    async with open_session(url) as session:
        print("--- workload-list/AC2 ---")
        await test_workload_list_ac2_namespace_scope(session)
        print("ok: workload-list/AC2")


if __name__ == "__main__":
    asyncio.run(run())
