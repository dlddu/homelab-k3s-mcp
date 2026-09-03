"""Deployed-server e2e for workload-logs/AC3 (크래시 루프 후 직전 로그).

검증 AC: workload-logs/AC3
실행 대상: primary

``crashloop-fixture`` 만 상대하고 그 전제를 멱등 폴링으로 스스로 성립시킨다.
직전 인스턴스 마커는 현재 인스턴스가 찍는 문자열과 달라, 라이브 로그를 읽어
통과할 수 없다.

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz
from _workload import CRASHLOOP_MARKER, CRASHLOOP_WORKLOAD, NAMESPACE, wait_for_crashloop_restart


async def test_workload_logs_ac3_previous_after_crash(session) -> None:
    """AC: workload-logs/AC3 — previous=true returns the terminated instance's own log.

    test-workload-logs.md S3 / AC3: after a crash, previous=true must return
    the terminated instance's actual log content. The fixture prints a known
    marker, exits non-zero exactly once, then stays Running — pinning
    lastState.terminated to the marker instance. The marker differs from the
    one the *current* instance prints, so this cannot pass by reading live logs.
    """
    wait_for_crashloop_restart()
    result = await session.call_tool(
        "workload_logs",
        {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": CRASHLOOP_WORKLOAD,
            "previous": True,
        },
    )
    assert result.isError is False, result
    payload = result.structuredContent
    pod_name = payload["pod"]
    assert pod_name.startswith(f"{CRASHLOOP_WORKLOAD}-"), pod_name
    assert payload["previous"] is True, payload
    assert CRASHLOOP_MARKER in payload["logs"], payload["logs"]
    assert CRASHLOOP_MARKER in result.content[0].text, result.content[0].text
    print("workload_logs previous content ok, pod:", pod_name)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- workload-logs/AC3 ---")
        await test_workload_logs_ac3_previous_after_crash(session)
        print("ok: workload-logs/AC3")


if __name__ == "__main__":
    asyncio.run(run())
