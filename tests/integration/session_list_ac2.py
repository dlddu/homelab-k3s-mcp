"""Deployed-server e2e for session-list/AC2 (상태를 바꾸지 않는 조회).

검증 AC: session-list/AC2
실행 대상: primary

AC2의 "새 파드가 기동되지 않음" 절이 이 하네스가 **실 제어면**을 띄우는 이유다. 스텁 앞에서는
그 단정이 vacuous 하다 — 스텁은 애초에 파드를 만들지 않으므로 도구가 복원을 유발했더라도
파드 수는 그대로다. 실 제어면은 snapshot 세션을 복원하면 실제로 세션 파드를 만들기 때문에,
파드 집합 불변이 진짜 판별자가 된다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _session_platform import pod_names, seed_sessions, session_payload, sessions_from

#: AC2가 지목하는 구성 — ``snapshot`` 세션이 포함된 재고. idle 세션도 함께 두어
#: "유휴 세션 목록을 확인하는 것만으로 컴퓨트가 되살아나지 않는다"는 문장의 대상을
#: 둘 다 덮는다(idle 은 승격, snapshot 은 복원이 각각의 위험이다).
IDLE_SESSION = session_payload(
    session_id="e2e-ac2-idle",
    name="e2e idle shell",
    workload_type="shell",
    state="idle",
    pod="session-e2e-ac2-idle",
    created_at="2026-09-01T00:00:00Z",
    last_access="2026-09-03T11:00:00Z",
)
SNAPSHOT_SESSION = session_payload(
    session_id="e2e-ac2-snapshot",
    name="e2e parked agent",
    workload_type="claude-code",
    state="snapshot",
    created_at="2026-08-30T09:30:00Z",
    last_access="2026-09-02T18:45:00Z",
)

#: 반복 조회 횟수. 한 번으로는 "반복 조회 뒤에도 동일" 이라는 AC 문언을 만족하지 못한다.
LIST_CALLS = 3


async def test_session_list_ac2_listing_is_passive(session) -> None:
    """AC: session-list/AC2 — repeated listings change nothing.

    Asserts the three things the verification method names, against a control
    plane holding an idle session and a snapshotted one:

    * every session's ``state`` is identical across repeated calls (no idle
      session was promoted, no snapshot restored),
    * every session's ``lastAccess`` is identical (listing is not an access),
    * the control plane started no pod (a restore would have provisioned one).

    The pod set is captured before the first call, so a pod appearing at any
    point during the calls is caught.
    """
    seed_sessions([IDLE_SESSION, SNAPSHOT_SESSION])
    pods_before = pod_names()

    observations = []
    for _ in range(LIST_CALLS):
        result = await session.call_tool("session_list", {})
        observations.append(
            {item["id"]: (item["state"], item["lastAccess"]) for item in sessions_from(result)}
        )

    first = observations[0]
    assert set(first) == {IDLE_SESSION["id"], SNAPSHOT_SESSION["id"]}, first
    # The premise of the AC: at least one session is in a state a read/write
    # would have moved. If the seed silently failed to land, this catches it
    # before the invariance assertions pass vacuously.
    assert {state for state, _ in first.values()} == {"idle", "snapshot"}, first

    for index, observed in enumerate(observations[1:], start=2):
        assert observed == first, (
            f"listing #{index} changed session state/lastAccess: {observed} != {first}"
        )

    pods_after = pod_names()
    assert pods_after == pods_before, (
        f"listing started or reclaimed compute: {sorted(pods_after ^ pods_before)}"
    )
    print("passive listing ok:", LIST_CALLS, "calls,", len(pods_after), "pods unchanged")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- session-list/AC2 ---")
        await test_session_list_ac2_listing_is_passive(session)
        print("ok: session-list/AC2")


if __name__ == "__main__":
    asyncio.run(run())
