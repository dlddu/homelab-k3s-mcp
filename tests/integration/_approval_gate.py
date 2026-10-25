"""승인 게이트 e2e 가 게이트 상대(PocketAide)를 조작하는 공유 헬퍼.

게이트 대상 도구 호출은 승인이 떨어질 때까지 **블록**한다. 그래서 모든 댄스는
``asyncio.create_task`` 로 도구 호출을 띄워 놓고 ``wait_for_pending`` 으로 생성된
승인 요청을 찾아 판정을 내리는 순서로 간다. 요청 격리는 context 안에 들어가는
고유 마커(대상 객체 이름)로 **만** 한다 — 레인이 다른 게이트 파일은 동시에 돌고
(``run_all.py`` 의 병렬 레인), 그룹끼리도 겹쳐 같은 PocketAide 에 요청을 띄운다.
그래서 두 가지가 서 있어야 한다:

* 마커는 **다른 레인의 어떤 요청 context 에도 나오지 않는** 문자열이다. 겹치면 남의 요청을
  집어 판정해 버린다 — 겹칠 수밖에 없는 파일(같은 대상을 건드리는 파일)은 같은 레인에 둔다.
* 「승인 요청이 생기지 않았다」는 전체 요청 수가 아니라 ``count_requests`` 로 **마커를 담은
  요청 수**를 센다. 전체 수는 남의 레인이 띄운 요청으로 움직인다.

## 요청은 사용자의 것이다

PocketAide 의 승인 요청은 그 호출자(서비스 어카운트)를 등록한 사용자의 것이고, 앱 API 는
자기 요청만 보여 준다 — 남의 요청 id 는 404 다. 공개 목록이 없으므로 「모든 요청」은 아래
``CALLERS`` 의 사용자 전원의 대기 목록과 처리 이력을 합친 것이다. 요청 id 의 주인은 처음 본
목록에서 기억하고, 모르는 id 는 사용자마다 단건 조회로 찾는다.

자동 응답 모드도 사용자 설정이다. 그래서 SUT 배포마다 **다른 사용자**로 등록한다 — 한
파일이 모드를 바꿔도 그 사용자의 호출자, 곧 그 배포의 요청에만 미친다.

## 사람 로그인 대신

토큰은 in-cluster ``oidc-mock`` 의 PKCE 인가 코드 흐름으로 받는다. subject 는 ``/authorize``
의 ``login_hint`` 로 고른다 — ``/e2e/next-login-subject`` 는 프로세스 전역 값 하나라 동시에 도는
레인이 서로의 다음 로그인을 덮어쓴다.
"""

from __future__ import annotations

import asyncio
import base64
import contextlib
import hashlib
import secrets
import socket
import sys
import time
from collections.abc import Iterable, Iterator
from urllib.parse import parse_qs, urlparse

import httpx

from _helpers import port_forward

POCKET_AIDE_NAMESPACE = "pocket-aide"
POCKET_AIDE_SERVICE = "pocket-aide"
POCKET_AIDE_PORT = 8080
TRACE_SERVICE = "pocket-aide-trace"

OIDC_NAMESPACE = "oidc-mock"
OIDC_SERVICE = "oidc-mock"
OIDC_PORT = 5556
#: ``oidc-mock.yaml`` 의 ``-client-id`` 와 PocketAide 의 ``OIDC_REDIRECT_URI``. 리다이렉트는
#: 따라가지 않고 ``Location`` 에서 코드만 읽으므로 그 주소에 아무것도 없어도 된다.
OIDC_CLIENT_ID = "homelab-k3s-mcp-e2e"
OIDC_REDIRECT_URI = "http://127.0.0.1/e2e-callback"

#: PocketAide 에 호출자로 등록하는 SUT 서비스 어카운트와 그것을 등록한(= 그 요청을 판정하는)
#: 사용자의 OIDC subject. ci.yml 의 등록 스텝이 이 표를 그대로 등록한다. 배포가 같은
#: 사용자를 공유하면 한쪽의 자동 응답 모드가 다른 쪽 요청에 걸린다.
CALLERS = {
    "homelab-k3s-mcp/homelab-k3s-mcp": "e2e-primary",
    "homelab-k3s-mcp-approval-variant/homelab-k3s-mcp": "e2e-approval-variant",
    "homelab-k3s-mcp-rbac-narrow/homelab-k3s-mcp": "e2e-rbac-narrow",
    "homelab-k3s-mcp-gate-refusal/homelab-k3s-mcp": "e2e-gate-refusal",
    "homelab-k3s-mcp-apiserver-audit/homelab-k3s-mcp": "e2e-apiserver-audit",
}
OWNERS = tuple(dict.fromkeys(CALLERS.values()))
#: 자동 응답 모드를 바꾸는 파일들(``실행 대상: approval-variant``)의 사용자.
VARIANT_OWNER = CALLERS["homelab-k3s-mcp-approval-variant/homelab-k3s-mcp"]

#: 기록 프록시의 create 기록이 담는 필드 — 시나리오 2 의 요청 본문 계약 그 자체다.
CREATE_BODY_FIELDS = ("externalId", "context", "requesterName", "timeoutSeconds", "reason")

TRACE_LOCAL_PORT = 8096

#: oidc-mock 토큰의 수명은 1 시간이다. 그보다 넉넉히 일찍 새로 받는다.
TOKEN_REFRESH_SECONDS = 1800.0

_tokens: dict[str, tuple[str, float]] = {}
_owner_of_request: dict[str, str] = {}


def _ephemeral_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
        probe.bind(("127.0.0.1", 0))
        return probe.getsockname()[1]


def _pkce_pair() -> tuple[str, str]:
    verifier = secrets.token_urlsafe(48)
    digest = hashlib.sha256(verifier.encode()).digest()
    return verifier, base64.urlsafe_b64encode(digest).rstrip(b"=").decode()


def _mint(oidc: str, subject: str) -> str:
    """``subject`` 로 로그인한 사용자의 액세스 토큰(aud = PocketAide 의 OIDC_AUDIENCE)."""
    verifier, challenge = _pkce_pair()
    authorize = httpx.get(
        f"{oidc}/authorize",
        params={
            "response_type": "code",
            "client_id": OIDC_CLIENT_ID,
            "redirect_uri": OIDC_REDIRECT_URI,
            "scope": "openid",
            "state": subject,
            "code_challenge": challenge,
            "code_challenge_method": "S256",
            "login_hint": subject,
        },
        follow_redirects=False,
        timeout=10.0,
    )
    assert authorize.status_code == 302, (
        f"oidc-mock /authorize 가 {authorize.status_code} 를 돌려줬다: {authorize.text[:200]}"
    )
    code = parse_qs(urlparse(authorize.headers["location"]).query)["code"][0]
    token = httpx.post(
        f"{oidc}/token",
        data={
            "grant_type": "authorization_code",
            "code": code,
            "code_verifier": verifier,
            "client_id": OIDC_CLIENT_ID,
            "redirect_uri": OIDC_REDIRECT_URI,
        },
        timeout=10.0,
    )
    token.raise_for_status()
    return token.json()["access_token"]


def _ensure_tokens(subjects: Iterable[str] = OWNERS) -> None:
    now = time.monotonic()
    stale = [s for s in subjects if s not in _tokens or now - _tokens[s][1] > TOKEN_REFRESH_SECONDS]
    if not stale:
        return
    with port_forward(
        OIDC_NAMESPACE,
        OIDC_SERVICE,
        OIDC_PORT,
        _ephemeral_port(),
        "/.well-known/openid-configuration",
    ) as oidc:
        for subject in stale:
            _tokens[subject] = (_mint(oidc, subject), time.monotonic())


def _headers(owner: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {_tokens[owner][0]}"}


@contextlib.contextmanager
def approval_url() -> Iterator[str]:
    """실물 PocketAide API 의 로컬 URL. 사용자 전원의 토큰을 먼저 받아 둔다.

    로컬 포트는 부를 때마다 빈 포트를 새로 고른다 — 동시에 도는 레인마다 이 포워드를 따로
    열기 때문에 고정 포트면 둘째 레인이 ``port_forward`` 의 점유 단언에서 멈춘다.
    """
    _ensure_tokens()
    with port_forward(
        POCKET_AIDE_NAMESPACE,
        POCKET_AIDE_SERVICE,
        POCKET_AIDE_PORT,
        _ephemeral_port(),
        "/healthz",
    ) as url:
        yield url


@contextlib.contextmanager
def trace_url() -> Iterator[str]:
    """기록 프록시 admin 의 로컬 URL."""
    with port_forward(
        POCKET_AIDE_NAMESPACE,
        TRACE_SERVICE,
        8081,
        TRACE_LOCAL_PORT,
        "/records",
    ) as url:
        yield url


def _tag(row: dict, owner: str) -> dict:
    _owner_of_request[row["id"]] = owner
    return {**row, "owner": owner}


def _history(client: httpx.Client, url: str, owner: str) -> list[dict]:
    """처리된 요청 전부(승인·거절·만료). 한 쪽에 20건이라 커서가 다할 때까지 넘긴다."""
    rows: list[dict] = []
    cursor: str | None = None
    while True:
        params = {"cursor": cursor} if cursor else None
        response = client.get(
            f"{url}/api/approvals/history", params=params, headers=_headers(owner)
        )
        response.raise_for_status()
        page = response.json()
        rows.extend(page["items"])
        cursor = page.get("next_cursor")
        if not cursor:
            return rows


def list_requests(
    url: str, status: str | None = None, owners: Iterable[str] = OWNERS
) -> list[dict]:
    """사용자들의 요청 레코드(``status`` 를 주면 그 상태만), 오래된 것부터.

    각 레코드는 PocketAide 앱 API 의 모양 그대로(``external_id``·``decision_mode`` 등)에
    주인 subject 를 ``owner`` 로 더한 것이다.
    """
    rows: list[dict] = []
    with httpx.Client(timeout=10.0) as client:
        for owner in owners:
            if status in (None, "PENDING"):
                response = client.get(f"{url}/api/approvals", headers=_headers(owner))
                response.raise_for_status()
                rows += [_tag(r, owner) for r in response.json()["items"]]
            if status != "PENDING":
                rows += [
                    _tag(r, owner)
                    for r in _history(client, url, owner)
                    if status is None or r["status"] == status
                ]
    return sorted(rows, key=lambda r: (r["created_at"], r["id"]))


def count_requests(url: str, marker: str, status: str | None = None) -> int:
    """context 에 마커를 담은 요청 수(``status`` 를 주면 그 상태만)."""
    return sum(1 for row in list_requests(url, status) if marker in row.get("context", ""))


def owner_of(url: str, request_id: str) -> str:
    """요청 id 의 주인 subject. 목록에서 본 적이 없으면 사용자마다 단건 조회로 찾는다."""
    if request_id in _owner_of_request:
        return _owner_of_request[request_id]
    with httpx.Client(timeout=10.0) as client:
        for owner in OWNERS:
            response = client.get(
                f"{url}/api/approvals/{request_id}", headers=_headers(owner)
            )
            if response.status_code == 200:
                _owner_of_request[request_id] = owner
                return owner
            if response.status_code != 404:
                response.raise_for_status()
    raise AssertionError(f"요청 {request_id} 의 주인이 등록된 사용자 {OWNERS} 중에 없다")


def get_request(url: str, request_id: str) -> dict:
    """요청 레코드 하나. PocketAide 는 조회 시점에 만료를 평가하므로(저장된 상태는 PENDING
    그대로다) 이 호출이 PENDING → EXPIRED 를 관측하는 경로다."""
    owner = owner_of(url, request_id)
    response = httpx.get(
        f"{url}/api/approvals/{request_id}", headers=_headers(owner), timeout=10.0
    )
    response.raise_for_status()
    return {**response.json(), "owner": owner}


async def wait_for_pending(url: str, marker: str, timeout: float = 30.0) -> dict:
    """context 에 마커를 담은 PENDING 요청을 폴링으로 기다린다.

    도구 호출이 블록한 채 승인 요청을 만들었다는 최초 관측이다. 마커는 대상 객체
    이름이므로, 다른 테스트의 잔여 요청과 섞이지 않는다. **async** 여야 한다 —
    호출 패턴은 `create_task` 로 도구 호출을 띄운 직후 이 폴링에 들어가는 것이고,
    동기 폴링이면 이벤트 루프가 막혀 그 task 가 요청을 전송조차 하지 못한다.
    """
    deadline = time.monotonic() + timeout
    last_seen: list[str] = []
    async with httpx.AsyncClient(timeout=10.0) as client:
        while time.monotonic() < deadline:
            for owner in OWNERS:
                response = await client.get(f"{url}/api/approvals", headers=_headers(owner))
                response.raise_for_status()
                for row in response.json()["items"]:
                    last_seen.append(row.get("context", "")[:80])
                    if marker in row.get("context", ""):
                        return _tag(row, owner)
            await asyncio.sleep(0.2)
    raise AssertionError(
        f"마커 {marker!r} 을 담은 PENDING 승인 요청이 {timeout:.0f}초 안에 없었다; "
        f"본 것: {last_seen[-20:]}"
    )


async def decide(url: str, request_id: str, status: str, timeout: float = 30.0) -> dict:
    """승인(APPROVED)이나 거부(REJECTED)를 그 요청의 주인 대신 내린다.

    이미 처리됐거나 만료된 요청에 대한 재판정은 PocketAide 가 409 로 거절한다 — 그 자체가
    승인이 한 번 쓰였다는 관측이다. 도구 호출이 비행 중인 창에 불리므로 async 다.
    """
    owner = owner_of(url, request_id)
    deadline = time.monotonic() + timeout
    last_exc: Exception | None = None
    path = "approve" if status == "APPROVED" else "reject"
    async with httpx.AsyncClient(timeout=10.0) as client:
        while time.monotonic() < deadline:
            response = await client.post(
                f"{url}/api/approvals/{request_id}/{path}", headers=_headers(owner)
            )
            if response.status_code == 200:
                return {**response.json(), "owner": owner}
            last_exc = AssertionError(
                f"{request_id} 판정({status})이 {response.status_code} 로 거절됐다: "
                f"{response.text[:200]}"
            )
            await asyncio.sleep(0.2)
    raise last_exc if last_exc else AssertionError("unreachable")


def set_auto_response(url: str, mode: str, owner: str = VARIANT_OWNER) -> None:
    """사용자의 자동 응답 모드(NONE·AUTO_APPROVE·AUTO_REJECT)를 바꾼다. NONE 으로 되돌리는
    청소에도 쓴다. 모드는 요청 **생성** 때만 적용된다 — 이미 대기 중인 요청은 바뀌지 않는다."""
    response = httpx.patch(
        f"{url}/api/approval-settings",
        headers=_headers(owner),
        json={"auto_response": mode},
        timeout=10.0,
    )
    response.raise_for_status()
    assert response.json()["auto_response"] == mode, response.json()


def trace_records(url: str) -> list[dict]:
    """기록 프록시가 쌓아 둔 전체 기록."""
    response = httpx.get(f"{url}/records", timeout=10.0)
    response.raise_for_status()
    return response.json()["records"]


def create_records(url: str) -> list[dict]:
    return [r for r in trace_records(url) if r.get("type") == "create"]


def poll_count(url: str, request_id: str) -> int:
    """프록시가 본 특정 요청 id 의 폴링(GET) 횟수.

    클라이언트가 실제로 몇 번이고 조회했는지는 PocketAide 기록에 남지 않고 이 기록에만 남는다.
    """
    return sum(
        1
        for r in trace_records(url)
        if r.get("type") == "poll" and r.get("path", "").endswith(f"/{request_id}")
    )


def register_callers(url: str) -> None:
    """``CALLERS`` 의 서비스 어카운트를 각자의 사용자 아래 호출자로 등록한다(ci.yml 이 부른다).

    다시 돌려도 된다 — 이미 등록돼 있으면(409) 그 사용자의 호출자 목록에 실제로 있는지 본다.
    다른 사용자가 같은 서비스 어카운트를 쥐고 있으면 그 409 는 등록 실패다.
    """
    with httpx.Client(timeout=10.0) as client:
        for account, owner in CALLERS.items():
            response = client.post(
                f"{url}/api/approval-keys",
                headers=_headers(owner),
                json={"name": f"e2e {account}", "kind": "k8s_sa", "service_account": account},
            )
            if response.status_code == 201:
                print(f"registered {account} -> {owner}")
                continue
            if response.status_code != 409:
                raise AssertionError(
                    f"{account} 등록이 {response.status_code} 로 실패했다: {response.text[:200]}"
                )
            keys = client.get(f"{url}/api/approval-keys", headers=_headers(owner))
            keys.raise_for_status()
            mine = [
                k for k in keys.json()["keys"]
                if k.get("service_account") == account and k.get("revoked_at") is None
            ]
            assert mine, f"{account} 는 {owner} 가 아닌 사용자에게 이미 등록돼 있다"
            print(f"already registered {account} -> {owner}")


if __name__ == "__main__":
    with approval_url() as pocket_aide:
        register_callers(pocket_aide)
    sys.exit(0)
