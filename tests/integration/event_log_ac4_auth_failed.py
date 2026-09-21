"""인증 실패도 레코드를 남긴다 — 잘못된 API 키와 만료된 JWT, 둘 다 거부·인증 실패로 남고 값은 없다.

검증 시나리오: test-event-log.md#시나리오 6
실행 대상: oauth-variant

이 변형에서 도는 이유는 두 자격증명 경로를 한 배포가 다 갖기 때문이다 — ``MCP_API_KEYS`` 와
``MCP_OAUTH_*`` 가 함께 설정된 구성 (c). 잘못된 API 키는 정적 키 대조에서 떨어져 JWT 검증까지
내려간 뒤 401 이 되고, 만료된 JWT 는 서명은 맞되 ``exp`` 가 지나 401 이 된다. 둘 다 인증 층이
디스패처 앞에서 끊으므로 레코드는 그 층이 남긴다(``internal/auth/auth.go::recordRefusal``).

**만료된 JWT 는 실 발급자(dex)가 서명한 것이다.** 남의 키로 서명한 토큰은 서명 검증에서
떨어지는 것이지 만료가 아니다 — 그래서 픽스처(``tests/k8s/kind/oidc-fixture.yaml``)가 password
grant 를 열어 두고, 이 파일은 그 토큰으로 **먼저 성공 호출을 한 번 한다**(주체가 ``jwt:<sub>`` 로
남는 대조군). 같은 토큰이 만료 뒤 401 이 되고 그 레코드의 주체가 ``unauthenticated`` 라는 것이
「만료」의 관측이다. 픽스처의 ``expiry.idTokens`` 가 짧은 이유는 이 대기 하나뿐이다.

호출은 MCP 세션이 아니라 원시 JSON-RPC ``tools/call`` 본문이다 — 인증 층은 tools/call 본문에만
레코드를 남기고(거부된 initialize·tools/list 는 도구 호출이 아니다) 401 은 세션을 열지 못하므로,
SDK 를 거치면 관측 지점에 닿지 않는다. AC3 교차: 제시한 키 값·JWT 원문·JWT 모양이 두 거부
레코드 어디에도 없다.
"""

from __future__ import annotations

import asyncio
import base64
import json
import re
import time

import httpx

from _eventlog import exactly_one_new, parse, records, select, wait_for_new
from _helpers import base_url, port_forward, wait_for_healthz
from _oidc import DEX_NAMESPACE, DEX_PORT, DEX_SERVICE, OAUTH_NAMESPACE, BOTH_DEPLOYMENT

DEX_LOCAL_PORT = 18086

#: oidc-fixture.yaml 의 staticClients / staticPasswords 와 같아야 한다.
CLIENT_ID = "homelab-k3s-mcp"
CLIENT_SECRET = "ci-oauth-e2e-client-secret"
USERNAME = "e2e@homelab-k3s-mcp.test"
PASSWORD = "password"

WRONG_API_KEY = "el-s6-not-a-configured-key-4c1d"
TOOL = "ping"

JWT_RE = re.compile(r"eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+")


def _call(url: str, bearer: str) -> httpx.Response:
    return httpx.post(
        f"{url}/mcp",
        json={"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": TOOL, "arguments": {}}},
        headers={
            "Authorization": f"Bearer {bearer}",
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
        },
        timeout=10.0,
    )


def _mint_id_token(dex: str) -> tuple[str, dict]:
    response = httpx.post(
        f"{dex}/token",
        data={
            "grant_type": "password",
            "username": USERNAME,
            "password": PASSWORD,
            "scope": "openid",
        },
        auth=(CLIENT_ID, CLIENT_SECRET),
        timeout=10.0,
    )
    assert response.status_code == 200, f"dex password grant 가 {response.status_code}: {response.text[:300]}"
    token = response.json()["id_token"]
    payload = token.split(".")[1]
    claims = json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))
    return token, claims


def _count(principal: str) -> int:
    return len(select(records(OAUTH_NAMESPACE, BOTH_DEPLOYMENT), tool=TOOL, principal=principal))


def _refusal(before: int) -> dict[str, str]:
    found = wait_for_new(OAUTH_NAMESPACE, BOTH_DEPLOYMENT, before, tool=TOOL, principal="unauthenticated")
    record = exactly_one_new(found, before, "인증 실패")
    assert record["result"] == "refused" and record["reason"] == "auth_failed", record
    return record


def test_a_wrong_api_key_leaves_a_refusal(url: str) -> dict[str, str]:
    before = _count("unauthenticated")
    response = _call(url, WRONG_API_KEY)
    assert response.status_code == 401 and response.text == "invalid_token", (
        response.status_code, response.text[:200],
    )
    return _refusal(before)


def test_an_expired_jwt_leaves_a_refusal(url: str, dex: str) -> tuple[dict[str, str], str]:
    token, claims = _mint_id_token(dex)
    subject = f"jwt:{claims['sub']}"
    accepted_before = _count(subject)
    refused_before = _count("unauthenticated")

    fresh = _call(url, token)
    assert fresh.status_code == 200 and "result" in fresh.json(), (
        "방금 발급된 JWT 가 통과하지 못했다 — 이 대조군이 없으면 아래의 401 은 만료가 아니라 "
        f"어떤 거부라도 통과시킨다: {fresh.status_code} {fresh.text[:300]}"
    )
    accepted = exactly_one_new(
        wait_for_new(OAUTH_NAMESPACE, BOTH_DEPLOYMENT, accepted_before, tool=TOOL, principal=subject),
        accepted_before,
        "유효한 JWT",
    )
    assert accepted["result"] == "success", accepted

    time.sleep(max(0.0, claims["exp"] - time.time()) + 2.0)
    expired = _call(url, token)
    assert expired.status_code == 401 and expired.text == "invalid_token", (
        expired.status_code, expired.text[:200],
    )
    return _refusal(refused_before), token


def test_the_presented_credentials_are_not_in_the_records(
    key_record: dict[str, str], jwt_record: dict[str, str], token: str
) -> None:
    for record in (key_record, jwt_record):
        assert record["principal"] == "unauthenticated", record
        line = " ".join(f"{k}={v}" for k, v in record.items())
        assert WRONG_API_KEY not in line, f"레코드에 제시된 API 키가 실렸다: {line}"
        assert token not in line, f"레코드에 JWT 원문이 실렸다: {line}"
        assert not JWT_RE.search(line), f"레코드에 JWT 모양의 값이 실렸다: {line}"


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    print("--- event-log/시나리오 6 (잘못된 API 키) ---")
    key_record = test_a_wrong_api_key_leaves_a_refusal(url)

    print("--- event-log/시나리오 6 (만료된 JWT — 발급 → 통과 → 만료 → 거부) ---")
    with port_forward(DEX_NAMESPACE, DEX_SERVICE, DEX_PORT, DEX_LOCAL_PORT, "/healthz") as dex:
        jwt_record, token = test_an_expired_jwt_leaves_a_refusal(url, dex)

    print("--- event-log/시나리오 6 (주체 필드에 자격증명 값이 없다) ---")
    test_the_presented_credentials_are_not_in_the_records(key_record, jwt_record, token)
    print("ok: test-event-log.md#시나리오 6")


if __name__ == "__main__":
    asyncio.run(run())
