"""Deployed-server e2e for session-list/AC1 (세션 열거).

검증 AC: session-list/AC1
실행 대상: primary

AC1의 검증 방법은 제어면의 **두 가지 상태**를 요구한다 — 세션이 있는 제어면과 없는 제어면.
파일 간 실행 순서로 그것을 만들지 않고(그 결합은 직전 슬라이스가 기각했다), 이 파일이 자기
선행 조건을 스스로 성립시킨다: 먼저 재고를 비워 빈 목록을 관측하고, 그다음 세션 둘을 시드해
열거를 관측한다. 두 절이 한 프로세스 안에 있으므로 다른 파일이 무엇을 남겨 놨든 무관하다.
"""

from __future__ import annotations

import asyncio
import datetime

from _helpers import base_url, open_session, wait_for_healthz
from _session_platform import (
    clear_sessions,
    seed_sessions,
    session_payload,
    sessions_from,
)

#: 서로 다른 상태의 세션 둘. AC1이 "세션 2개(서로 다른 상태)"를 명시한다.
#: snapshot 세션은 파드가 회수된 상태라 ``pod`` 가 없다 — 제어면의 ``Session`` 이
#: 그 필드를 ``omitempty`` 로 선언하는 이유이고, 그래서 이 둘은 응답 모양까지 다르다.
ACTIVE_SESSION = session_payload(
    session_id="e2e-ac1-active",
    name="e2e active shell",
    workload_type="shell",
    state="active",
    pod="session-e2e-ac1-active",
    created_at="2026-09-01T00:00:00Z",
    last_access="2026-09-03T11:00:00Z",
)
SNAPSHOT_SESSION = session_payload(
    session_id="e2e-ac1-snapshot",
    name="e2e parked agent",
    workload_type="claude-code",
    state="snapshot",
    created_at="2026-08-30T09:30:00Z",
    last_access="2026-09-02T18:45:00Z",
)


async def test_session_list_ac1_empty_inventory(session) -> None:
    """AC: session-list/AC1 — an empty control plane returns an empty list.

    The AC's second clause: no sessions is not an error. Asserted before the
    seeding half so the emptiness is one this file established, not a leftover.
    """
    clear_sessions()

    result = await session.call_tool("session_list", {})
    sessions = sessions_from(result)
    assert sessions == [], f"empty control plane did not return an empty list: {sessions}"
    print("empty inventory ok")


async def test_session_list_ac1_enumerates_sessions(session) -> None:
    """AC: session-list/AC1 — every session, with the fields the AC names.

    Seeds two sessions in different states and asserts both come back carrying
    id, name, workloadType, state and lastAccess. The state-dependent shape is
    asserted too: the active one names its pod, the snapshotted one omits the
    field entirely rather than reporting an empty pod name.
    """
    seed_sessions([ACTIVE_SESSION, SNAPSHOT_SESSION])

    result = await session.call_tool("session_list", {})
    sessions = sessions_from(result)
    by_id = {item["id"]: item for item in sessions}
    assert set(by_id) == {ACTIVE_SESSION["id"], SNAPSHOT_SESSION["id"]}, sessions

    active = by_id[ACTIVE_SESSION["id"]]
    snapshot = by_id[SNAPSHOT_SESSION["id"]]

    assert active["name"] == ACTIVE_SESSION["name"], active
    assert active["workloadType"] == "shell", active
    assert active["state"] == "active", active
    assert active["pod"] == ACTIVE_SESSION["pod"], active

    assert snapshot["name"] == SNAPSHOT_SESSION["name"], snapshot
    assert snapshot["workloadType"] == "claude-code", snapshot
    assert snapshot["state"] == "snapshot", snapshot
    assert "pod" not in snapshot, snapshot

    # The two states really are different, so the tool is reporting per-session
    # state rather than one value for the whole inventory.
    assert active["state"] != snapshot["state"], sessions

    for item in (active, snapshot):
        # Parses (and therefore is a real instant), not just a non-empty string.
        datetime.datetime.fromisoformat(item["createdAt"].replace("Z", "+00:00"))
        datetime.datetime.fromisoformat(item["lastAccess"].replace("Z", "+00:00"))

    print("enumeration ok:", sorted(by_id))


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- session-list/AC1 ---")
        await test_session_list_ac1_empty_inventory(session)
        await test_session_list_ac1_enumerates_sessions(session)
        print("ok: session-list/AC1")


if __name__ == "__main__":
    asyncio.run(run())
