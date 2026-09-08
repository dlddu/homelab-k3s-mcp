"""namespace_list: k8s 통합 미설정 시 graceful 거부 (e2e).

검증 시나리오: test-namespace-list.md#시나리오 2
실행 대상: auth-variant

이 배포가 시나리오의 사전 조건("k8s 통합 미구성")이 성립하는 유일한 곳이다. primary 와
oauth-variant 는 in-cluster ServiceAccount 로 클러스터에 붙어 있어 통합이 살아 있고,
auth-variant 만 ``tests/k8s/kind/auth-fixture.yaml`` 에서 ``MCP_K8S_DISABLED`` 를 세워
``main.go`` 의 ``buildK8sService`` 가 실 클라이언트 대신 ``k8s.NewUnavailable`` 을 돌려주게
한다. 대체 검증인 Go 단위 ``mcp_test.go::TestNamespaceListUnavailableIsToolError`` 는
in-process 핸들러에 unavailable 서비스를 직접 주입해 같은 성질을 보지만, 그것은 **배포된
서버가 그 상태로 실제 기동하는지**는 말해 주지 않는다 — 여기서 더해지는 것이 그것이다.
"""

from __future__ import annotations

import asyncio

from _auth_variant import (
    API_KEY,
    K8S_REFUSAL,
    assert_unavailable_refusal,
)
from _helpers import base_url, open_session, wait_for_healthz


async def test_namespace_list_ac2_unconfigured_refusal(session: ClientSession) -> None:
    """시나리오: test-namespace-list.md#시나리오 2 (AC: namespace-list/AC1 degradation)

    With the kubernetes integration switched off, namespace_list comes back as a
    tool error instead of taking the server down, and the server keeps serving —
    the two halves of the scenario's expected result ("서버는 정상, 호출만 도구
    에러 반환"), both of which the shared helper asserts.

    The expected text is matched exactly rather than by substring: it is the
    composition of Error()'s "kubernetes client unavailable: " prefix with the
    reason main.go passes for MCP_K8S_DISABLED. Restoring the integration on this
    fixture changes that text (an RBAC refusal or a success), so this file is
    also the tripwire on the fixture's contract.
    """
    await assert_unavailable_refusal(session, "namespace_list", {}, K8S_REFUSAL)


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        print("--- unconfigured graceful refusal "
              "(시나리오: test-namespace-list.md#시나리오 2) ---")
        await test_namespace_list_ac2_unconfigured_refusal(session)
        print("refusal ok: test-namespace-list.md#시나리오 2")


if __name__ == "__main__":
    asyncio.run(run())
