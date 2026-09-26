"""시나리오 5 의 409·5xx 를 만드는 결함 주입기의 클라이언트.

주입기 자체는 ``tests/k8s/kind/gatekeeper-injector.yaml`` 이고, 실물 gatekeeper 가
쓰는 SQLite 파일에 **마커 한정** ``BEFORE INSERT`` 트리거를 걸었다 걷는다. 허용
범위는 ``docs/e2e-mocking-policy.md`` 의 「실환경 주입 판정」 조건 C1~C5 다.

이것이 ``_gatekeeper.py`` 와 **다른 파일인 이유**: 저쪽은 승인·거절을 사람 대신
내리는 게이트 조작이고, 이쪽은 게이트 상대를 고장 내는 결함 주입이다. 한 파일에
두면 승인 댄스를 쓰는 여덟 파일이 주입기 상수까지 함께 읽게 된다.

응답은 주입하지 않는다 — 409 의 status·본문은 실물 ``store.isUniqueViolation``
(고유 인덱스 ``Request_externalId_key``)을 탄 실물 핸들러가, 500 은 실물
``httpx.InternalError`` 가 만든다. 주입이 만드는 것은 **전제**뿐이다(C1).
"""

from __future__ import annotations

import contextlib
from collections.abc import Iterator

import httpx

from _gatekeeper import GATEKEEPER_NAMESPACE, _ephemeral_port
from _helpers import port_forward

INJECTOR_SERVICE = "gatekeeper-injector"

#: 주입 모드. ``conflict`` 는 같은 ``externalId`` 의 그림자 행을 먼저 넣어 본
#: INSERT 가 진짜 고유 인덱스 위반으로 죽게 하고(→ 실물 409), ``error`` 는
#: ``RAISE(ABORT, …)`` 로 저장소 오류를 만든다(→ 실물 500).
INJECT_CONFLICT = "conflict"
INJECT_ERROR = "error"


@contextlib.contextmanager
def injector_url() -> Iterator[str]:
    """주입기 제어 API 의 로컬 URL.

    ``gatekeeper_url`` 과 같은 이유로 로컬 포트를 매번 새로 고른다 — 레인마다
    이 포워드를 따로 열기 때문에 고정 포트면 둘째 레인이 점유 단언에서 멈춘다.
    """
    with port_forward(
        GATEKEEPER_NAMESPACE,
        INJECTOR_SERVICE,
        8082,
        _ephemeral_port(),
        "/state",
    ) as url:
        yield url


def injector_state(url: str) -> dict:
    """``{"triggers": [...], "shadow_rows": N}`` — C4 의 잔여 0 을 재는 자리."""
    response = httpx.get(f"{url}/state", timeout=10.0)
    response.raise_for_status()
    return response.json()


def arm(url: str, mode: str, marker: str) -> dict:
    """마커 한정 트리거를 건다.

    주입기는 걸자마자 **프로브 INSERT 를 트랜잭션 안에서 날려 롤백**하고, 그것이
    기대한 오류를 내지 않으면 arm 자체를 실패시킨다. 그래서 이 함수가 돌아왔다는
    것은 「트리거가 이 마커에 실제로 발화한다」가 관측됐다는 뜻이다 — 발화하지
    않는 트리거 위에서 공허하게 통과하는 단언을 막는다.
    """
    response = httpx.post(
        f"{url}/arm", json={"mode": mode, "marker": marker}, timeout=30.0
    )
    if response.status_code != 200:
        raise AssertionError(f"주입기 arm({mode}, {marker!r}) 실패: {response.text}")
    return response.json()


def disarm(url: str) -> dict:
    """트리거를 걷고 그림자 행을 지운다."""
    response = httpx.post(f"{url}/disarm", timeout=30.0)
    if response.status_code != 200:
        raise AssertionError(f"주입기 disarm 실패: {response.text}")
    return response.json()


@contextlib.contextmanager
def injected(url: str, mode: str, marker: str) -> Iterator[dict]:
    """``arm`` → 본문 → 반드시 ``disarm``.

    본문이 무엇으로 끝나든 트리거를 걷는다. 걷은 뒤의 잔여 0 은 **호출자가**
    단언한다 — C4 를 이 헬퍼 안에 숨기면 그 단언이 테스트 문면에서 사라진다.
    """
    armed = arm(url, mode, marker)
    try:
        yield armed
    finally:
        disarm(url)
