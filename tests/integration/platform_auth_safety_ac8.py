"""인증 방식 구성 유연성: API 키·OAuth 의 네 가지 env 조합을 배포로 대조한다 (e2e).

검증 AC: platform-auth-safety/AC8
실행 대상: auth-variant

`tests/integration/check_ac_mapping.py`가 이 선언을 읽어 `docs/doc-tracker.md`의 레지스트리와
대조하고, `tests/integration/run_all.py`가 `실행 대상`을 읽어 이 파일을 배차한다.

AC 의 검증 방법은 네 구성을 **각각** 요구한다 — (a) API 키만 → 인증 활성 + 디스커버리 미제공,
(b) OAuth 만 → 디스커버리 제공, (c) 둘 다 → 둘 다 동작, (d) 둘 다 미설정 + `MCP_AUTH_DISABLED`
도 미설정 → 기동 실패. 그래서 이 파일은 **네 배포를 대조하는 것 자체**가 검증이며, 자기 그룹의
배포 하나만으로는 AC 를 관측할 수 없다.

- (a) 는 이 파일의 `실행 대상` 인 auth-variant(`tests/k8s/kind/auth-fixture.yaml`) 다.
  `MCP_API_KEYS` 만 세팅하고 `MCP_AUTH_DISABLED`·`MCP_OAUTH_*` 를 미설정으로 두는 것을
  그 매니페스트가 주석으로 의도 선언해 둔 배포이고, 러너가 준 base URL 이 그것이다.
- (b)(c)(d) 는 `tests/k8s/kind/oidc-fixture.yaml` 이 세우고, `_oidc.port_forward` 로 필요한
  순간에만 짧게 연다.

env 게이팅 자체는 `internal/auth/auth_test.go` 의 `FromEnv` 단위 테스트가 이미 4조합을 덮는다.
여기서 더해지는 것은 그것이 **배포된 서버의 라우팅과 기동 여부로 실제로 나타나는가**다 —
단위 테스트는 `App` 이 라우트를 걸지 않는 것까지만 보고, 파드가 뜨지 않는 것은 보지 못한다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess
import time

import httpx

from _helpers import base_url, open_session, wait_for_healthz
from _oidc import (
    API_KEY,
    BOTH_DEPLOYMENT,
    BOTH_RESOURCE,
    ISSUER,
    NO_AUTH_DEPLOYMENT,
    NO_AUTH_STARTUP_ERROR,
    OAUTH_NAMESPACE,
    OAUTH_ONLY_DEPLOYMENT,
    OAUTH_ONLY_RESOURCE,
    PROTECTED_RESOURCE_PATH,
    assert_deployment_available,
    available_replicas,
    port_forward,
    protected_resource_metadata,
    unauthenticated_challenge,
)

#: 필요한 순간에만 여는 로컬 포트. `ci.yml` 의 그룹 포워드(8080·8088·8089·8090)와
#: `platform_auth_safety_ac2.py` 의 18080 을 피한다.
OAUTH_ONLY_LOCAL_PORT = 18081
BOTH_LOCAL_PORT = 18082

#: (d) 변형의 파드를 고르는 셀렉터. 그 배포는 Service 도 프로브도 없다 — 뜨지 않는 것이
#: 의도이므로 관측은 전부 apiserver 를 통한다.
NO_AUTH_SELECTOR = f"app.kubernetes.io/name={NO_AUTH_DEPLOYMENT}"


def _crashed_container_status(timeout: float = 120.0) -> dict:
    """(d) 변형의 컨테이너가 **한 번이라도 죽은** 뒤 그 상태를 돌려준다.

    파드는 CrashLoopBackOff 로 돌므로 관측하려는 종료 기록은 `lastState.terminated` 이거나
    (첫 종료 직후라면) `state.terminated` 다. 둘 중 먼저 나타나는 것을 기다린다.
    """
    deadline = time.monotonic() + timeout
    last_seen: dict = {}
    while time.monotonic() < deadline:
        proc = subprocess.run(
            [
                "kubectl",
                "-n",
                OAUTH_NAMESPACE,
                "get",
                "pods",
                "-l",
                NO_AUTH_SELECTOR,
                "-o",
                "json",
            ],
            capture_output=True,
            text=True,
            timeout=60,
        )
        if proc.returncode == 0:
            items = json.loads(proc.stdout).get("items", [])
            for pod in items:
                for status in pod.get("status", {}).get("containerStatuses", []) or []:
                    last_seen = status
                    terminated = status.get("lastState", {}).get(
                        "terminated"
                    ) or status.get("state", {}).get("terminated")
                    if terminated:
                        return {"terminated": terminated, "status": status}
        time.sleep(2)
    raise AssertionError(
        f"deploy/{NO_AUTH_DEPLOYMENT} never recorded a terminated container within "
        f"{timeout:.0f}s (last container status seen: {last_seen})"
    )


def _no_auth_logs() -> str:
    """(d) 변형 파드의 로그. 백오프 중이면 현재 인스턴스에 로그가 없어 `--previous` 로 뒤진다."""
    for extra in ([], ["--previous"]):
        proc = subprocess.run(
            [
                "kubectl",
                "-n",
                OAUTH_NAMESPACE,
                "logs",
                "-l",
                NO_AUTH_SELECTOR,
                "--tail=50",
                *extra,
            ],
            capture_output=True,
            text=True,
            timeout=60,
        )
        if proc.returncode == 0 and proc.stdout.strip():
            return proc.stdout
    return ""


async def test_platform_auth_safety_ac8_a_api_keys_only(url: str) -> None:
    """AC: platform-auth-safety/AC8

    (a) API 키만 설정 → **인증은 활성이고 디스커버리는 제공되지 않는다.**

    두 관측이 함께여야 (a) 다. 401 은 게이트가 서 있음을 보이고, 그 챌린지에
    `resource_metadata` 가 **없다**는 것과 디스커버리 라우트가 404 라는 것이 「광고할 발급자가
    없으므로 엔드포인트를 제공하지 않는다」를 보인다. 아래 (c) 가 같은 두 자리에서 정반대를
    관측하므로, 이 단정은 "그냥 아무것도 없는 서버"와 구분된다.
    """
    print("--- config (a) api keys only (AC: platform-auth-safety/AC8) ---")
    _, challenge = unauthenticated_challenge(url)
    assert "resource_metadata" not in challenge, (
        f"API-key-only deployment advertised OAuth discovery: {challenge!r}"
    )
    assert 'error="missing_token"' in challenge, f"unexpected challenge: {challenge!r}"

    discovery = httpx.get(f"{url}{PROTECTED_RESOURCE_PATH}", timeout=5.0)
    assert discovery.status_code == 404, (
        f"API-key-only deployment served {PROTECTED_RESOURCE_PATH} with "
        f"{discovery.status_code}, expected 404 (the route is only registered when "
        f"OAuth is configured)"
    )
    print("config (a) ok: auth on, no discovery advertised or served")


async def test_platform_auth_safety_ac8_b_oauth_only() -> None:
    """AC: platform-auth-safety/AC8

    (b) OAuth 만 설정 → **기존 동작**, 즉 디스커버리가 제공된다.

    `MCP_API_KEYS` 를 전혀 갖지 않은 배포가 뜬다는 것 자체가 「OAuth 는 API 키 없이도 단독으로
    인증을 활성화한다」이고(자격증명이 하나도 없으면 `auth.FromEnv` 가 기동을 막는다 — (d)
    참조), 그 위에서 디스커버리 문서가 자기 리소스와 발급자를 반환한다.
    """
    print("--- config (b) oauth only (AC: platform-auth-safety/AC8) ---")
    assert_deployment_available(OAUTH_NAMESPACE, OAUTH_ONLY_DEPLOYMENT)

    with port_forward(
        OAUTH_NAMESPACE,
        OAUTH_ONLY_DEPLOYMENT,
        80,
        OAUTH_ONLY_LOCAL_PORT,
        ready_path="/healthz",
    ) as url:
        metadata = protected_resource_metadata(url)
        assert metadata.get("authorization_servers") == [ISSUER], (
            f"oauth-only metadata authorization_servers = "
            f"{metadata.get('authorization_servers')!r}, expected [{ISSUER!r}]"
        )
        assert metadata.get("resource") == OAUTH_ONLY_RESOURCE, (
            f"oauth-only metadata resource = {metadata.get('resource')!r}, "
            f"expected {OAUTH_ONLY_RESOURCE!r}"
        )
        _, challenge = unauthenticated_challenge(url)
        assert (
            f'resource_metadata="{OAUTH_ONLY_RESOURCE}{PROTECTED_RESOURCE_PATH}"'
            in challenge
        ), f"oauth-only challenge does not advertise its metadata: {challenge!r}"
    print("config (b) ok: no API keys configured, discovery served")


async def test_platform_auth_safety_ac8_c_both_paths() -> None:
    """AC: platform-auth-safety/AC8

    (c) 둘 다 설정 → **둘 다 동작한다.**

    API 키 경로는 그 키로 `tools/list` 가 인가되는 것으로, OAuth 경로는 디스커버리가 제공되고
    챌린지가 그것을 광고하는 것으로 관측한다. 이 배포의 키는 auth-variant 의 키와 **다른
    값**이라(`_oidc.API_KEY` vs `_auth_variant.API_KEY`), 두 배포가 서로의 자격증명으로 통과할
    수 없다.

    OAuth 경로에서 **유효 JWT 로 인가까지** 태우는 것은 이 슬라이스의 범위 밖이다 — 발급자에게
    실제 토큰을 받아 오려면 dex 에 정적 클라이언트와 password DB 를 붙여야 하고, AC8 이 (c) 에
    요구하는 것은 두 경로가 함께 **구성되어 동작한다**는 것이다. 후속에서 그 구성이 붙으면
    AC1 의 "유효 토큰 → 정상 처리" 절과 함께 강화하는 것이 자연스럽다.
    """
    print("--- config (c) both paths (AC: platform-auth-safety/AC8) ---")
    assert_deployment_available(OAUTH_NAMESPACE, BOTH_DEPLOYMENT)

    with port_forward(
        OAUTH_NAMESPACE,
        BOTH_DEPLOYMENT,
        80,
        BOTH_LOCAL_PORT,
        ready_path="/healthz",
    ) as url:
        _, challenge = unauthenticated_challenge(url)
        assert (
            f'resource_metadata="{BOTH_RESOURCE}{PROTECTED_RESOURCE_PATH}"' in challenge
        ), f"both-paths challenge does not advertise its metadata: {challenge!r}"

        metadata = protected_resource_metadata(url)
        assert metadata.get("authorization_servers") == [ISSUER], (
            f"both-paths metadata authorization_servers = "
            f"{metadata.get('authorization_servers')!r}, expected [{ISSUER!r}]"
        )

        async with open_session(
            url, headers={"Authorization": f"Bearer {API_KEY}"}
        ) as session:
            tools = await session.list_tools()
            names = {tool.name for tool in tools.tools}
            assert "ping" in names, (
                f"API key did not authorize on the both-paths deployment: "
                f"{sorted(names)}"
            )
    print("config (c) ok: api key authorizes and discovery is served")


async def test_platform_auth_safety_ac8_d_neither_refuses_to_start() -> None:
    """AC: platform-auth-safety/AC8

    (d) 둘 다 미설정 + `MCP_AUTH_DISABLED` 도 미설정 → **기동 실패**(무방비 노출 차단).

    이것이 AC 의 안전 절이고, 라우팅이 아니라 프로세스 수명으로만 나타난다 — `auth.FromEnv` 가
    「어느 자격증명 경로도 구성되지 않았다」로 오류를 내고 `main.go` 가 `os.Exit(1)` 하므로,
    파드는 Ready 가 되지 않고 재시작을 반복한다. 종료 코드와 로그를 함께 보는 이유는, 어떤
    이유로든 죽기만 하면 통과하는 단정이 되지 않게 하기 위해서다.

    이 관측은 `platform_auth_safety_ac2.py` 가 「OAuth 배포가 Available 하다 ⇒ 기동 시 OIDC
    디스커버리와 JWKS 로드에 성공했다」로 쓰는 추론의 근거이기도 하다 — 그 경로가 실제로
    치명적임을 여기서 본다.
    """
    print("--- config (d) neither configured (AC: platform-auth-safety/AC8) ---")
    observed = _crashed_container_status()
    terminated = observed["terminated"]

    assert terminated.get("exitCode") == 1, (
        f"deploy/{NO_AUTH_DEPLOYMENT} exited with {terminated.get('exitCode')!r}, "
        f"expected 1 (auth.FromEnv refusing to serve /mcp undefended): {observed}"
    )
    assert available_replicas(OAUTH_NAMESPACE, NO_AUTH_DEPLOYMENT) == 0, (
        f"deploy/{NO_AUTH_DEPLOYMENT} became available, but a server with no "
        f"credential path configured must not serve"
    )

    logs = _no_auth_logs()
    assert NO_AUTH_STARTUP_ERROR in logs, (
        f"deploy/{NO_AUTH_DEPLOYMENT} did not report {NO_AUTH_STARTUP_ERROR!r}; it "
        f"died for some other reason. Last logs: {logs!r}"
    )
    print("config (d) ok: server refuses to start rather than serve /mcp undefended")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    await test_platform_auth_safety_ac8_a_api_keys_only(url)
    await test_platform_auth_safety_ac8_b_oauth_only()
    await test_platform_auth_safety_ac8_c_both_paths()
    await test_platform_auth_safety_ac8_d_neither_refuses_to_start()


if __name__ == "__main__":
    asyncio.run(run())
