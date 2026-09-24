"""승인 게이트 e2e 가 게이트 상대를 조작하는 공유 헬퍼.

kind 에 뜬 실물 gatekeeper(``tests/k8s/kind/gatekeeper-fixture.yaml``)와 그 앞의
기록 프록시를 포트포워드로 닿게 한다. 사람 조작을 대신하는 승인·거절은 시나리오
문서의 픽스처 절이 정한 방법 — forward-auth 헤더(``Remote-User``)를 실어
``PATCH /api/requests/{id}/approve|reject`` 를 호출하는 것 — 이다.

게이트 대상 도구 호출은 승인이 떨어질 때까지 **블록**한다. 그래서 모든 댄스는
``asyncio.create_task`` 로 도구 호출을 띄워 놓고 ``wait_for_pending`` 으로 생성된
승인 요청을 찾아 판정을 내리는 순서로 간다. 요청 격리는 context 안에 들어가는
고유 마커(대상 객체 이름)로 **만** 한다 — 레인이 다른 게이트 파일은 동시에 돌고
(``run_all.py`` 의 병렬 레인), auth-variant 그룹도 primary 와 겹쳐 같은 gatekeeper 에
요청을 띄운다. 그래서 두 가지가 서 있어야 한다:

* 마커는 **다른 레인의 어떤 요청 context 에도 나오지 않는** 문자열이다. 겹치면 남의 요청을
  집어 판정해 버린다 — 겹칠 수밖에 없는 파일(같은 대상을 건드리는 파일)은 같은 레인에 둔다.
* 「승인 요청이 생기지 않았다」는 전체 요청 수가 아니라 ``count_requests`` 로 **마커를 담은
  요청 수**를 센다. 전체 수는 남의 레인이 띄운 요청으로 움직인다.
"""

from __future__ import annotations

import asyncio
import contextlib
import os
import socket
import time
from collections.abc import Iterator

import httpx

from _helpers import port_forward

GATEKEEPER_NAMESPACE = "gatekeeper"
GATEKEEPER_SERVICE = "gatekeeper"
TRACE_SERVICE = "gatekeeper-trace"

#: 사람 조작을 대신하는 사용자. 픽스처에 뜬 gatekeeper 가 첫 forward-auth 요청에서
#: 이 이름의 사용자를 upsert 하고, ci.yml 의 시드 스텝이 그 id 를 읽어 변형 SUT 의
#: GATEKEEPER_USER_ID 로 핀한다.
E2E_USER = "gatekeeper-e2e"

#: 기록 프록시의 create 기록이 담는 필드 — 시나리오 2 의 요청 본문 계약 그 자체다.
CREATE_BODY_FIELDS = ("externalId", "context", "requesterName", "timeoutSeconds")

TRACE_LOCAL_PORT = 8096


def _ephemeral_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
        probe.bind(("127.0.0.1", 0))
        return probe.getsockname()[1]


@contextlib.contextmanager
def gatekeeper_url() -> Iterator[str]:
    """실물 gatekeeper API 의 로컬 URL.

    로컬 포트는 부를 때마다 빈 포트를 새로 고른다 — 동시에 도는 레인마다 이 포워드를 따로
    열기 때문에 고정 포트면 둘째 레인이 ``port_forward`` 의 점유 단언에서 멈춘다.
    """
    with port_forward(
        GATEKEEPER_NAMESPACE,
        GATEKEEPER_SERVICE,
        80,
        _ephemeral_port(),
        "/api/health",
    ) as url:
        yield url


@contextlib.contextmanager
def trace_url() -> Iterator[str]:
    """기록 프록시 admin 의 로컬 URL."""
    with port_forward(
        GATEKEEPER_NAMESPACE,
        TRACE_SERVICE,
        8081,
        TRACE_LOCAL_PORT,
        "/records",
    ) as url:
        yield url


def _user_headers(user: str = E2E_USER) -> dict[str, str]:
    return {"Remote-User": user}


def _api_key() -> str:
    """gatekeeper API 키 — ci.yml 이 생성해 GITHUB_ENV 로 실행자에게 넘겨 준다.

    gatekeeper 의 경로 인가는 갈려 있다: 목록(GET /api/requests)은 공개지만
    단건 조회(GET /api/requests/{id})는 API 키를 요구한다. 단건 조회가 유일하게
    만료를 조회 시점에 평가하므로(PENDING → EXPIRED 전이) 이 헬퍼는 키 없이는
    성립하지 않는다.
    """
    key = os.environ.get("GATEKEEPER_E2E_API_KEY", "").strip()
    if not key:
        raise AssertionError(
            "GATEKEEPER_E2E_API_KEY 가 비어 있다 — ci.yml 의 시드 스텝이 "
            "GITHUB_ENV 에 넘겨 주는 키가 이 실행에 도달하지 않았다"
        )
    return key


def me(url: str, user: str = E2E_USER) -> dict:
    """forward-auth 경로로 사용자를 upsert 하고 그 레코드를 돌려준다."""
    response = httpx.get(f"{url}/api/me/", headers=_user_headers(user), timeout=10.0)
    response.raise_for_status()
    return response.json()


def list_requests(url: str, status: str | None = None) -> list[dict]:
    path = "/api/requests"
    if status is not None:
        path += f"?status={status}"
    response = httpx.get(f"{url}{path}", timeout=10.0)
    response.raise_for_status()
    return response.json()


def count_requests(url: str, marker: str, status: str | None = None) -> int:
    """context 에 마커를 담은 요청 수(``status`` 를 주면 그 상태만)."""
    return sum(1 for row in list_requests(url, status) if marker in row.get("context", ""))


def get_request(url: str, request_id: str) -> dict:
    """요청 레코드 하나. gatekeeper 는 조회 시점에 만료를 평가하므로 이 호출이
    PENDING → EXPIRED 전이를 일으키는 유일한 경로다. 이 경로는 API 키를 요구한다
    (공개인 것은 목록뿐이다)."""
    response = httpx.get(
        f"{url}/api/requests/{request_id}",
        headers={"x-api-key": _api_key()},
        timeout=10.0,
    )
    response.raise_for_status()
    return response.json()


async def wait_for_pending(
    url: str, marker: str, timeout: float = 30.0
) -> dict:
    """context 에 마커를 담은 PENDING 요청을 폴링으로 기다린다.

    도구 호출이 블록한 채 승인 요청을 만들었다는 최초 관측이다. 마커는 대상 객체
    이름이므로, 다른 테스트의 잔여 요청과 섕히지 않는다. **async** 여야 한다 —
    호출 패턴은 `create_task` 로 도구 호출을 띄운 직후 이 폴링에 들어가는 것이고,
    동기 폴링이면 이벤트 루프가 막혀 그 task 가 요청을 전송조차 하지 못한다.
    """
    deadline = time.monotonic() + timeout
    last_seen: list[str] = []
    async with httpx.AsyncClient(timeout=10.0) as client:
        while time.monotonic() < deadline:
            response = await client.get(f"{url}/api/requests?status=PENDING")
            response.raise_for_status()
            for row in response.json():
                last_seen.append(row.get("context", "")[:80])
                if marker in row.get("context", ""):
                    return row
            await asyncio.sleep(0.2)
    raise AssertionError(
        f"마커 {marker!r} 을 담은 PENDING 승인 요청이 {timeout:.0f}초 안에 없었다; "
        f"본 것: {last_seen}"
    )


async def decide(
    url: str,
    request_id: str,
    status: str,
    user: str = E2E_USER,
    timeout: float = 30.0,
) -> dict:
    """승인(APPROVED)이나 거부(REJECTED)를 사람 대신 내린다.

    이미 처리된 요청에 대한 재판정은 gatekeeper 가 409 로 거절한다 — 그 자체가
    승인이 한 번 쓰였다는 관측이다. 도구 호출이 비행 중인 창에 불리므로 async 다.
    """
    deadline = time.monotonic() + timeout
    last_exc: Exception | None = None
    path = "approve" if status == "APPROVED" else "reject"
    async with httpx.AsyncClient(timeout=10.0) as client:
        while time.monotonic() < deadline:
            response = await client.patch(
                f"{url}/api/requests/{request_id}/{path}",
                headers=_user_headers(user),
            )
            if response.status_code == 200:
                return response.json()
            last_exc = AssertionError(
                f"{request_id} 판정({status})이 {response.status_code} 로 거절됐다: "
                f"{response.text[:200]}"
            )
            await asyncio.sleep(0.2)
    raise last_exc if last_exc else AssertionError("unreachable")


def set_auto_response(url: str, mode: str, user: str = E2E_USER) -> None:
    """사용자의 자동 응답 모드를 바꾼다. NONE 으로 되돌리는 청소에도 쓴다."""
    response = httpx.patch(
        f"{url}/api/me/auto-response-mode",
        headers=_user_headers(user),
        json={"mode": mode},
        timeout=10.0,
    )
    response.raise_for_status()


def trace_records(url: str) -> list[dict]:
    """기록 프록시가 쌓아 둔 전체 기록."""
    response = httpx.get(f"{url}/records", timeout=10.0)
    response.raise_for_status()
    return response.json()["records"]


def create_records(url: str) -> list[dict]:
    return [r for r in trace_records(url) if r.get("type") == "create"]


def poll_count(url: str, request_id: str) -> int:
    """프록시가 본 특정 요청 id 의 폴링(GET) 횟수.

    시나리오 4 의 「폴링을 끊고 최초 응답만 본 구현은 통과하지 못함」을 잰다 —
    클라이언트가 실제로 몇 번이고 조회했는지는 upstream 도 gatekeeper 기록에도
    남지 않고 이 기록에만 남는다.
    """
    return sum(
        1
        for r in trace_records(url)
        if r.get("type") == "poll" and request_id in r.get("path", "")
    )
