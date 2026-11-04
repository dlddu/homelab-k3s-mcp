"""PocketAide 의 거부 사유가 구분돼 돌아온다 — 미설정 거부와도 서로와도 다르게 읽힌다.

검증 시나리오: test-pocket-aide-notification-send.md#시나리오 3
실행 대상: primary

두 배포를 한 파일에서 만난다. 호출자로 **등록된** 배포는 primary 이고(`_approval_gate.CALLERS`),
kind 의 PocketAide 에는 APNs 가 없어 PocketAide 가 503 으로 답한다. 호출자로 **등록되지 않은**
배포는 `tests/k8s/kind/notify-unregistered-variant.yaml` 의 전용 배포이며 이 파일 자신의
포트포워드로 닿는다 —
PocketAide 가 자기 인증 층에서 401 을 낸다. 두 응답 모두 실물 PocketAide 의 것이다.

요청이 PocketAide 의 본문 검증을 통과해야 503 에 닿는다 — `requester` 를 받는 판
(`POCKET_AIDE_SHA` 가 dlddu/pocket-aide#321 이후)이어야 한다.
"""

from __future__ import annotations

import asyncio

from _auth_variant import API_KEY, POCKET_AIDE_REFUSAL
from _helpers import base_url, open_session, port_forward, wait_for_healthz

TOOL = "pocket_aide_notification_send"
ARGUMENTS = {"title": "refusal-probe", "requester": "e2e 하네스"}

UNREGISTERED_NAMESPACE = "homelab-k3s-mcp-notify-unregistered"
UNREGISTERED_SERVICE = "homelab-k3s-mcp-notify-unregistered"
#: 다른 포워드(approval_gate_sc5 18085-18087, 18088·18089, ci.yml 의 그룹 포워드)와 겹치지 않는 자리.
UNREGISTERED_LOCAL_PORT = 18090


async def refusal_text(session) -> str:
    result = await session.call_tool(TOOL, ARGUMENTS)
    assert result.isError is True, f"call did not refuse: {result}"
    assert result.content, result
    block = result.content[0]
    assert block.type == "text", block
    return block.text


def assert_reads_differently(text: str) -> None:
    assert text != POCKET_AIDE_REFUSAL, f"미설정 거부와 같은 문면이다: {text!r}"
    assert "unavailable: PocketAide is not configured" not in text, text


async def test_registered_caller_gets_apns_refusal(session) -> str:
    text = await refusal_text(session)
    assert "(503)" in text and "APNs" in text, f"등록된 배포의 거부가 503/APNs 가 아니다: {text!r}"
    assert_reads_differently(text)
    return text


async def test_unregistered_caller_gets_registration_refusal() -> str:
    with port_forward(
        UNREGISTERED_NAMESPACE,
        UNREGISTERED_SERVICE,
        80,
        UNREGISTERED_LOCAL_PORT,
        ready_path="/healthz",
    ) as url:
        wait_for_healthz(url)
        async with open_session(url, headers={"Authorization": f"Bearer {API_KEY}"}) as session:
            text = await refusal_text(session)
            assert "(401)" in text and "registered" in text, (
                f"미등록 배포의 거부가 401/등록 안내가 아니다: {text!r}"
            )
            assert_reads_differently(text)
            survivor = await session.call_tool("ping", {})
            assert survivor.isError is False and survivor.content[0].text == "pong", survivor
            return text


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    print("--- pocket-aide-notification-send/AC3 ---")
    async with open_session(url) as session:
        registered = await test_registered_caller_gets_apns_refusal(session)
    unregistered = await test_unregistered_caller_gets_registration_refusal()
    assert registered != unregistered, f"두 거부가 같은 문면이다: {registered!r}"
    print("ok: pocket-aide-notification-send/AC3")


if __name__ == "__main__":
    asyncio.run(run())
