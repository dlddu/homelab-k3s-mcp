"""Deployed-server e2e for session-delete/AC1 (세션 삭제와 파드 회수).

검증 시나리오: test-session-delete.md#시나리오 1
실행 대상: primary
병렬 레인: session

세션을 둘 만드는 이유는 「지정한 세션만」이 단정의 절반이기 때문이다 — 하나뿐이면 제어면의
세션을 전부 지우는 결함도 이 파일을 통과한다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _session_platform import (
    control_plane,
    create_shell_session,
    delete_and_reclaim,
    pod_names,
    sessions_from,
    wait_for_pod_gone,
)


async def test_session_delete_sc1_removes_only_the_target(
    session, target: dict, bystander: dict
) -> None:
    result = await session.call_tool("session_delete", {"id": target["id"]})
    assert result.isError is False, result
    assert result.structuredContent == {"id": target["id"], "deleted": True}, (
        result.structuredContent
    )

    listed = {s["id"] for s in sessions_from(await session.call_tool("session_list", {}))}
    assert target["id"] not in listed, f"deleted session is still listed: {listed}"
    assert bystander["id"] in listed, f"the other session disappeared: {listed}"

    wait_for_pod_gone(target["pod"])
    assert bystander["pod"] in pod_names(), (
        f"the other session's pod {bystander['pod']} was reclaimed too"
    )
    print("delete ok:", target["id"], "reclaimed; bystander", bystander["id"], "intact")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    with control_plane() as cp_url:
        target = create_shell_session(cp_url, "e2e delete sc1 target")
        try:
            bystander = create_shell_session(cp_url, "e2e delete sc1 bystander")
            try:
                async with open_session(url) as session:
                    print("--- session-delete/시나리오 1 ---")
                    await test_session_delete_sc1_removes_only_the_target(
                        session, target, bystander
                    )
                    print("ok: test-session-delete.md#시나리오 1")
            finally:
                delete_and_reclaim(cp_url, bystander)
        finally:
            delete_and_reclaim(cp_url, target)


if __name__ == "__main__":
    asyncio.run(run())
