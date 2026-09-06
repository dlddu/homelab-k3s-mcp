"""Deployed-server e2e for namespace-list/AC1 (네임스페이스 열거).

검증 AC: namespace-list/AC1
실행 대상: primary

픽스처의 레플리카 상태와 무관하다 — 네임스페이스 오브젝트는 ``test-deployment.yaml``
이 만들고 어떤 케이스도 지우지 않으므로 ``ensure_workload_fixture_baseline()`` 을
부르지 않는다.
"""

from __future__ import annotations

import asyncio
import datetime

from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE


async def test_namespace_list_ac1_enumerates_namespaces(session) -> None:
    """AC: namespace-list/AC1 — namespaces come back with name, phase, creation time.

    Asserts the three fields the AC names are present on every item and carry
    real values on a known namespace: the workload fixture's namespace is
    reported Active with a parseable creation timestamp, and a namespace the
    test did not create (kube-system) is listed too, so the tool is enumerating
    the cluster rather than echoing a filter.
    """
    result = await session.call_tool("namespace_list", {})
    assert result.isError is False, result
    items = result.structuredContent["items"]
    names = [item["name"] for item in items]
    assert NAMESPACE in names, names
    assert "kube-system" in names, names

    for item in items:
        assert item["phase"], item
        assert item["creation_timestamp"], item

    active = next(item for item in items if item["name"] == NAMESPACE)
    assert active["phase"] == "Active", active
    # Parses (and therefore is a real instant), not just a non-empty string.
    datetime.datetime.fromisoformat(active["creation_timestamp"])
    print("namespace_list ok:", len(items), "namespaces")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- namespace-list/AC1 ---")
        await test_namespace_list_ac1_enumerates_namespaces(session)
        print("ok: namespace-list/AC1")


if __name__ == "__main__":
    asyncio.run(run())
