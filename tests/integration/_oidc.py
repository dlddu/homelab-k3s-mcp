"""`tests/k8s/kind/oidc-fixture.yaml` 를 상대하는 파일들의 공유 표면 (매칭 단위가 아니다).

`_` 접두라 `run_all.py::matching_unit_paths()` 가 걸러내므로 AC를 주검증하지 않고
`검증 AC:` 선언도 갖지 않는다 — `_auth_variant.py` · `_workload.py` 와 같은 자리다.

그 매니페스트가 세우는 것들의 이름·주소·자격증명을 여기 한 벌만 둔다. 값이 매니페스트와
어긋나면 `platform_auth_safety_ac2.py` 와 `platform_auth_safety_ac8.py` 가 **동시에** 거짓을
단정하게 되므로, 상수는 나뉘지 않는다.

## 왜 포트포워드가 필요한가

`ci.yml` 은 그룹당 배포 하나에만 포트포워드를 걸고, 그 포워드는 그룹 스텝이 사는 동안만
살아 있다. 그런데 platform-auth-safety/AC8 은 **네 가지 구성을 서로 대조하는 것 자체가 AC**라,
자기 그룹의 배포 하나만으로는 관측할 수 없다. 그래서 필요한 순간에만 짧게 여는 포워드를 쓴다 —
배포마다 러너 그룹을 하나씩 늘리는 대안은 `ci.yml` 에 스텝을 셋 더 만들고 각 파일을 다른
그룹으로 흩어 놓는데, 대조하는 파일이 하나라는 사실과 어긋난다.

`port_forward` 자체는 2026-09-04 에 `_helpers.py` 로 옮겼다 — dex 전용이 아니고
`_session_platform.py` 가 두 번째 소비자가 됐기 때문이다. 여기서 다시 내보내므로
`platform_auth_safety_ac{2,8}.py` 의 `from _oidc import port_forward` 는 그대로 돈다.
"""

from __future__ import annotations

import subprocess

import httpx

# Re-exported, not used here: the two platform-auth-safety files import it from
# this module and predate the move to _helpers.
from _helpers import port_forward  # noqa: F401

# --- dex (실 OIDC 발급자) -----------------------------------------------------

DEX_NAMESPACE = "dex"
DEX_SERVICE = "dex"
DEX_PORT = 5556

#: `MCP_OAUTH_ISSUER` 로 배포에 들어가는 값과 같아야 한다 (oidc-fixture.yaml).
ISSUER = "http://dex.dex.svc.cluster.local:5556"

# --- MCP 서버 배포 변형 -------------------------------------------------------

OAUTH_NAMESPACE = "homelab-k3s-mcp-oauth"

#: 구성 (c) — `MCP_OAUTH_*` 와 `MCP_API_KEYS` 를 둘 다 들고 있다.
#: `oauth-variant` 러너 그룹이 이 배포를 상대로 돈다.
BOTH_DEPLOYMENT = "homelab-k3s-mcp-oauth"
BOTH_RESOURCE = "http://homelab-k3s-mcp-oauth.homelab-k3s-mcp-oauth.svc.cluster.local"

#: 구성 (b) — `MCP_OAUTH_*` 만.
OAUTH_ONLY_DEPLOYMENT = "homelab-k3s-mcp-oauth-only"
OAUTH_ONLY_RESOURCE = (
    "http://homelab-k3s-mcp-oauth-only.homelab-k3s-mcp-oauth.svc.cluster.local"
)

#: 구성 (d) — 아무것도 미설정. 기동에 실패하는 것이 의도다.
NO_AUTH_DEPLOYMENT = "homelab-k3s-mcp-no-auth"

#: 세 변형이 공유하는 audience. `MCP_OAUTH_RESOURCE` 는 일부러 이것과 다른 값이라,
#: `auth.configureOAuth` 의 `resource == "" -> resource = audience` 폴백이 아니라
#: 명시된 resource 가 광고되는 경로가 실제로 돈다.
AUDIENCE = "homelab-k3s-mcp"

#: 구성 (c) 배포의 정적 API 키. `MCP_API_KEYS` 와 같아야 한다. auth-variant 의
#: `_auth_variant.API_KEY` 와 **다른 값**이라, 엉뚱한 배포의 키로 통과할 수 없다.
API_KEY = "ci-oauth-e2e-key"

# --- 경로·문자열 --------------------------------------------------------------

#: 보호 리소스 메타데이터 (RFC 9728). `internal/server/server.go` 가 OAuth 가 구성된
#: 경우에만 이 라우트를 건다.
PROTECTED_RESOURCE_PATH = "/.well-known/oauth-protected-resource"

#: OIDC 디스커버리. `internal/auth/auth.go::configureOAuth` 가 기동 시 발급자에게서
#: 이것을 가져와 `jwks_uri` 를 읽는다.
OPENID_CONFIGURATION_PATH = "/.well-known/openid-configuration"

#: `auth.FromEnv` 가 어느 자격증명 경로도 구성되지 않았을 때 내는 오류의 고정 접두.
#: `main.go` 가 이것을 `invalid auth config` 로 로깅하고 `os.Exit(1)` 한다.
NO_AUTH_STARTUP_ERROR = "no authentication configured"


def mcp_request() -> dict:
    """게이트만 보는 데 쓰는 최소 JSON-RPC 본문 (핸들러에 닿으면 tools/list 다)."""
    return {"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}}


def kubectl(*args: str) -> str:
    """`kubectl <args>` 의 stdout. 실패하면 stderr 를 그대로 들고 예외를 던진다."""
    proc = subprocess.run(
        ["kubectl", *args], capture_output=True, text=True, timeout=60
    )
    assert proc.returncode == 0, (
        f"kubectl {' '.join(args)} failed ({proc.returncode}): {proc.stderr.strip()}"
    )
    return proc.stdout


def available_replicas(namespace: str, deployment: str) -> int:
    """Deployment 의 `.status.availableReplicas` (미설정이면 0)."""
    raw = kubectl(
        "-n",
        namespace,
        "get",
        f"deploy/{deployment}",
        "-o",
        "jsonpath={.status.availableReplicas}",
    ).strip()
    return int(raw) if raw else 0


def assert_deployment_available(namespace: str, deployment: str) -> None:
    replicas = available_replicas(namespace, deployment)
    assert replicas >= 1, (
        f"deploy/{deployment} in {namespace} has {replicas} available replicas"
    )


def protected_resource_metadata(url: str) -> dict:
    """`GET <url>/.well-known/oauth-protected-resource` 를 200 으로 읽어 돌려준다."""
    response = httpx.get(f"{url}{PROTECTED_RESOURCE_PATH}", timeout=5.0)
    assert response.status_code == 200, (
        f"{url}{PROTECTED_RESOURCE_PATH} returned {response.status_code}, expected 200"
    )
    return response.json()


def unauthenticated_challenge(url: str) -> tuple[httpx.Response, str]:
    """인증 없는 `POST <url>/mcp` 의 401 응답과 그 `WWW-Authenticate` 챌린지."""
    response = httpx.post(
        f"{url}/mcp",
        json=mcp_request(),
        headers={"Content-Type": "application/json"},
        timeout=5.0,
    )
    assert response.status_code == 401, (
        f"unauthenticated {url}/mcp returned {response.status_code}, expected 401"
    )
    challenge = response.headers.get("WWW-Authenticate", "")
    assert challenge.startswith("Bearer"), (
        f"missing/blank WWW-Authenticate at {url}: {challenge!r}"
    )
    return response, challenge
