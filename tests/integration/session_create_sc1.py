"""Deployed-server e2e for session-create/AC1 (세션 생성과 활성 상태 반환).

검증 시나리오: test-session-create.md#시나리오 1
실행 대상: primary
병렬 레인: session

정리는 도구가 아니라 제어면에 직접 한다 — 삭제는 ``session_delete_sc1.py`` 가 단정하는 것이고,
여기서 그 도구를 쓰면 이 파일의 실패가 다른 도구의 결함으로 읽힐 수 있다. 같은 이름의 세션을
전부 지우는 것은 도구가 에러를 내면서도 세션을 만든 경우까지 치우기 위해서다.
"""

from __future__ import annotations

import asyncio

import httpx

from _helpers import base_url, open_session, wait_for_healthz
from _session_platform import (
    CREATE_TIMEOUT,
    SESSIONS_PATH,
    control_plane,
    delete_and_reclaim,
    pod_names,
    sessions_from,
)

SESSION_NAME = "e2e create sc1"


async def test_session_create_sc1_returns_an_active_session(session) -> dict:
    result = await session.call_tool("session_create", {"name": SESSION_NAME})
    assert result.isError is False, result
    created = result.structuredContent["session"]
    assert created["name"] == SESSION_NAME, created
    assert created["workloadType"] == "shell", created
    assert created["state"] == "active", created
    assert created.get("pod"), f"an active session must name its pod: {created}"
    assert created["pod"] in pod_names(), (
        f"pod {created['pod']} does not exist when session_create returned"
    )
    print("create ok:", created["id"], "pod", created["pod"])
    return created


async def test_session_create_sc1_is_listed(session, session_id: str) -> None:
    listed = sessions_from(await session.call_tool("session_list", {}))
    ids = {s["id"] for s in listed}
    assert session_id in ids, f"session_list does not enumerate {session_id}: {ids}"
    print("listed ok")


def _cleanup(url: str) -> None:
    listing = httpx.get(f"{url}{SESSIONS_PATH}", timeout=30.0)
    listing.raise_for_status()
    for leftover in listing.json().get("sessions") or []:
        if leftover.get("name") == SESSION_NAME:
            delete_and_reclaim(url, leftover)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    with control_plane() as cp_url:
        try:
            async with open_session(url, timeout=CREATE_TIMEOUT) as session:
                print("--- session-create/시나리오 1 ---")
                created = await test_session_create_sc1_returns_an_active_session(session)
                await test_session_create_sc1_is_listed(session, created["id"])
                print("ok: test-session-create.md#시나리오 1")
        finally:
            _cleanup(cp_url)


if __name__ == "__main__":
    asyncio.run(run())
