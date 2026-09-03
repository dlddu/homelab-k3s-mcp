"""서버 수준 graceful degradation: 자격증명 env를 비운 채 기동한다 (e2e).

검증 AC: platform-auth-safety/AC5
실행 대상: auth-variant

`tests/integration/check_ac_mapping.py`가 이 선언을 읽어 `docs/doc-tracker.md`의 레지스트리와
대조하고, `tests/integration/run_all.py`가 `실행 대상`을 읽어 이 파일을 배차한다.

This runs against the deployment variant in ``tests/k8s/kind/auth-fixture.yaml``:
auth is on (``MCP_API_KEYS`` set, ``MCP_AUTH_DISABLED`` unset) and no credential
secret is attached at all, so ``main.go``'s ``build*Service`` helpers each degrade
to ``NewUnavailable("")`` while the server still starts. Sessions therefore carry
the static key from ``_auth_variant.API_KEY``.

이 배포가 이 AC의 전제가 성립하는 유일한 곳이다 — 주 배포는 모든 자격증명이 배선돼 있어
정상적인 tools/list 가 degradation 에 대해 아무것도 말해 주지 않는다.
"""

from __future__ import annotations

import asyncio

from _auth_variant import API_KEY
from _helpers import EXPECTED_TOOLS, base_url, get_json, open_session, wait_for_healthz


async def test_platform_auth_safety_ac5_graceful_degradation(
    url: str, session: ClientSession
) -> None:
    """AC: platform-auth-safety/AC5 — the server runs with integrations unset.

    This is the only deployment in the suite where the AC's premise holds. The
    primary kind deployment wires up every credential secret, so a healthy
    tools/list there says nothing about degradation; this variant attaches none
    of them (GITHUB_APP_CLIENT_ID / AWS_CONFIG_S3_BUCKET / GRAFANA_ISSUER_TOKEN /
    OPENSEARCH_ENDPOINT all unset), which is exactly the "자격증명 env를 비운 채
    기동" the verification method describes.

    Asserts both halves of that method against this pod: the server is up and
    answering its liveness probe, and tools/list still returns the complete tool
    surface — including every tool whose backing integration is unavailable.
    Unconfigured integrations degrade the tools' *results* (the per-tool cases
    below assert that), never the server's ability to start and advertise them.
    """
    healthz = get_json(url, "/healthz")
    assert healthz.get("status") == "ok", f"unexpected /healthz: {healthz!r}"

    tools = await session.list_tools()
    names = {tool.name for tool in tools.tools}
    missing = EXPECTED_TOOLS - names
    assert not missing, (
        f"tools/list degraded with integrations unset: missing {sorted(missing)} "
        f"(got {sorted(names)})"
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(
        url, headers={"Authorization": f"Bearer {API_KEY}"}
    ) as session:
        print("--- server-level graceful degradation "
              "(AC: platform-auth-safety/AC5) ---")
        await test_platform_auth_safety_ac5_graceful_degradation(url, session)
        print("degradation ok: platform-auth-safety/AC5")


if __name__ == "__main__":
    asyncio.run(run())
