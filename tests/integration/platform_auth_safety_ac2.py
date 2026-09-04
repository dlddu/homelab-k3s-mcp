"""인증 디스커버리: 401 챌린지 → 보호 리소스 메타데이터 → OIDC 디스커버리 → JWKS (e2e).

검증 AC: platform-auth-safety/AC2
실행 대상: oauth-variant

`tests/integration/check_ac_mapping.py`가 이 선언을 읽어 `docs/doc-tracker.md`의 레지스트리와
대조하고, `tests/integration/run_all.py`가 `실행 대상`을 읽어 이 파일을 배차한다.

`실행 대상: oauth-variant` 는 `tests/k8s/kind/oidc-fixture.yaml` 의 `homelab-k3s-mcp-oauth`
배포다 — `MCP_OAUTH_*` 가 설정된 유일한 배포 종류이고, 디스커버리 문서는 OAuth 가 구성된
경우에만 라우팅된다(`internal/server/server.go`). 주 배포는 `MCP_AUTH_DISABLED=1` 이고
auth-variant 는 API 키 전용이라, 이 AC 는 두 곳 어디에서도 관측되지 않는다.

AC 의 검증 방법은 「보호 리소스 메타데이터가 발급자/리소스를 반환하고, **표준 MCP 클라이언트가
이를 통해 인증을 자동 구성할 수 있다**」이다. 그래서 이 파일은 문서를 한 번 읽고 끝내지 않고,
클라이언트가 실제로 걷는 순서 그대로 **연결된 사슬**을 걷는다 — 401 이 알려 주는 주소로 가서,
그 문서가 지목한 발급자로 가서, 그 발급자의 OIDC 디스커버리가 가리키는 키 집합에 닿는다.
사슬의 각 칸이 앞 칸이 준 값으로만 이어지므로, 중간의 한 칸이 낡으면 그 자리에서 끊긴다.
"""

from __future__ import annotations

import asyncio
from urllib.parse import urlsplit

import httpx

from _helpers import base_url, wait_for_healthz
from _oidc import (
    AUDIENCE,
    BOTH_DEPLOYMENT,
    BOTH_RESOURCE,
    DEX_NAMESPACE,
    DEX_PORT,
    DEX_SERVICE,
    ISSUER,
    OAUTH_NAMESPACE,
    OPENID_CONFIGURATION_PATH,
    PROTECTED_RESOURCE_PATH,
    assert_deployment_available,
    port_forward,
    protected_resource_metadata,
    unauthenticated_challenge,
)

#: 이 파일이 dex 를 상대할 때만 잠깐 여는 로컬 포트. `ci.yml` 이 그룹용으로 쓰는
#: 8080·8088·8089·8090 과 겹치지 않는 자리를 고른다.
DEX_LOCAL_PORT = 18080


async def test_platform_auth_safety_ac2_challenge_advertises_metadata(
    url: str,
) -> None:
    """AC: platform-auth-safety/AC2

    자동 구성의 **진입점**은 401 이다. OAuth 가 구성된 배포에서 인증 없는 `/mcp` 는
    `WWW-Authenticate` 에 `resource_metadata` 를 실어, 클라이언트가 어디로 가서 인증을
    구성해야 하는지 알려 준다(`internal/auth/auth.go::unauthorized` 의 OAuth 분기).
    API 키 전용 배포에서는 이 파라미터가 없으며, 그 대조는
    `platform_auth_safety_ac8.py` 가 맡는다.
    """
    print("--- discovery: 401 advertises resource_metadata (AC: platform-auth-safety/AC2) ---")
    response, challenge = unauthenticated_challenge(url)

    expected_metadata_url = f"{BOTH_RESOURCE}{PROTECTED_RESOURCE_PATH}"
    assert f'resource_metadata="{expected_metadata_url}"' in challenge, (
        f"challenge does not advertise the metadata document: {challenge!r}"
    )
    assert f'realm="{BOTH_RESOURCE}"' in challenge, (
        f"challenge realm is not the configured resource: {challenge!r}"
    )
    assert 'error="missing_token"' in challenge, f"unexpected challenge: {challenge!r}"
    assert response.text == "missing_token", f"unexpected 401 body: {response.text!r}"
    print(f"discovery ok: challenge -> {expected_metadata_url}")


async def test_platform_auth_safety_ac2_protected_resource_metadata(url: str) -> None:
    """AC: platform-auth-safety/AC2

    챌린지가 가리킨 문서가 **발급자와 리소스를 반환한다**. 세 필드를 배포된 구성과 정확히
    대조하는 것이 요점이다 — `MCP_OAUTH_RESOURCE` 를 audience 와 다른 값으로 배포해 두었으므로,
    `resource` 가 audience 로 돌아오면(= `configureOAuth` 의 폴백이 잘못 탔으면) 여기서 걸린다.
    """
    print("--- discovery: protected resource metadata (AC: platform-auth-safety/AC2) ---")
    metadata = protected_resource_metadata(url)

    assert metadata.get("resource") == BOTH_RESOURCE, (
        f"metadata resource = {metadata.get('resource')!r}, expected {BOTH_RESOURCE!r}"
    )
    assert metadata.get("authorization_servers") == [ISSUER], (
        f"metadata authorization_servers = {metadata.get('authorization_servers')!r}, "
        f"expected [{ISSUER!r}]"
    )
    assert metadata.get("bearer_methods_supported") == ["header"], (
        f"metadata bearer_methods_supported = "
        f"{metadata.get('bearer_methods_supported')!r}, expected ['header']"
    )
    assert metadata["resource"] != AUDIENCE, (
        "resource and audience are indistinguishable in this deployment, so the "
        "metadata assertion above cannot tell the configured resource from the "
        "audience fallback"
    )
    print(f"discovery ok: metadata advertises issuer {ISSUER}")


async def test_platform_auth_safety_ac2_issuer_discovery_loads_jwks(url: str) -> None:
    """AC: platform-auth-safety/AC2

    사슬의 마지막 칸: 메타데이터가 지목한 발급자의 **OIDC 디스커버리로 JWKS 에 닿는다.**
    발급자 주소는 위 문서에서 읽은 값을 그대로 쓰고, 키 집합 주소는 발급자의
    `openid-configuration` 이 준 `jwks_uri` 를 그대로 쓴다 — 어느 것도 이 파일에 하드코딩된
    경로가 아니다. 여기까지 걸리면 「표준 클라이언트가 이 문서로 인증을 자동 구성할 수 있다」가
    관측된 것이다.

    **서버 쪽 동적 로드**는 이 배포가 Available 하다는 사실이 증거다. `auth.FromEnv` 는 기동 시
    같은 `openid-configuration` 을 가져와 `jwks_uri` 가 없으면, 또는 그 JWKS 에 쓸 수 있는 RSA
    키가 하나도 없으면 오류를 내고 `main.go` 가 `os.Exit(1)` 한다. 그 경로가 실제로 치명적이라는
    것은 `platform_auth_safety_ac8.py` 가 자격증명을 하나도 주지 않은 변형에서 관측한다 —
    그래서 이 단정은 공허하지 않다.
    """
    print("--- discovery: issuer OIDC discovery -> JWKS (AC: platform-auth-safety/AC2) ---")
    metadata = protected_resource_metadata(url)
    issuer = metadata["authorization_servers"][0]

    with port_forward(
        DEX_NAMESPACE,
        DEX_SERVICE,
        DEX_PORT,
        DEX_LOCAL_PORT,
        ready_path=OPENID_CONFIGURATION_PATH,
    ) as dex_url:
        discovery = httpx.get(f"{dex_url}{OPENID_CONFIGURATION_PATH}", timeout=5.0)
        assert discovery.status_code == 200, (
            f"issuer OIDC discovery returned {discovery.status_code}, expected 200"
        )
        document = discovery.json()
        assert document.get("issuer") == issuer, (
            f"issuer discovery self-reports {document.get('issuer')!r}, but the "
            f"server advertises {issuer!r} — a client following the metadata would "
            f"reject this document"
        )

        jwks_uri = document.get("jwks_uri") or ""
        assert jwks_uri, f"issuer discovery has no jwks_uri: {document}"
        assert urlsplit(jwks_uri).netloc == urlsplit(issuer).netloc, (
            f"jwks_uri {jwks_uri!r} does not live at the advertised issuer {issuer!r}"
        )

        # jwks_uri 는 클러스터 내부 주소이므로, 그 **경로**를 지금 열어 둔 포워드로 가져온다.
        # 호스트는 바로 위에서 발급자와 같음을 확인했다.
        keys_response = httpx.get(
            f"{dex_url}{urlsplit(jwks_uri).path}", timeout=5.0
        )
        assert keys_response.status_code == 200, (
            f"jwks_uri returned {keys_response.status_code}, expected 200"
        )
        keys = keys_response.json().get("keys") or []

    # `auth.refreshKeys` 가 쓸 수 있다고 보는 키의 조건 그대로다: RSA 이고 n·e 가 있으며
    # kid 로 색인된다. 하나도 없으면 서버는 기동조차 못 한다.
    usable = [
        key
        for key in keys
        if key.get("kty") == "RSA" and key.get("n") and key.get("e") and key.get("kid")
    ]
    assert usable, f"issuer JWKS carries no usable RSA key: {keys}"

    assert_deployment_available(OAUTH_NAMESPACE, BOTH_DEPLOYMENT)
    print(f"discovery ok: {len(usable)} usable RSA key(s) reachable from the metadata")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    await test_platform_auth_safety_ac2_challenge_advertises_metadata(url)
    await test_platform_auth_safety_ac2_protected_resource_metadata(url)
    await test_platform_auth_safety_ac2_issuer_discovery_loads_jwks(url)


if __name__ == "__main__":
    asyncio.run(run())
