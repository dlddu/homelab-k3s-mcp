"""알림 본문의 `requester` 는 호출자가 쓴 요청자다 — 공백이 걷힌 값이 PocketAide 에 닿는다.

검증 시나리오: test-pocket-aide-notification-send.md#시나리오 5
실행 대상: approval-variant

PocketAide 의 앱 API 는 **보낸** 본문을 돌려주지 않는다 — 저장한 필드를 자기 이름으로 돌려줄
뿐이다. 그래서 이 파일은 SUT 를 기록 프록시(`pocket-aide-trace`) 뒤에 두는 변형 배포에서만
돈다(`tests/k8s/kind/approval-variant.yaml` — `APPROVAL_GATE_URL` 이 그 프록시를 가리킨다).
프록시는 모킹이 아니라 http-trace 와 같은 계열의 **기록면**이다(승인 게이트 시나리오 2 가
이미 쓰던 그 면).

«받은» 본문은 실물 PocketAide 가 받아 **본문 검증을 통과한** 것이다 — PocketAide 의 앱 API
는 `requester` 를 받는 판(`POCKET_AIDE_SHA` 가 dlddu/pocket-aide#321 이후)이어야 그 필드를
보관한다. 이 파일이 보는 기록의 상태가 503(APNs 미설정)인 것은 PocketAide 가 본문을 받아들인
뒤 APNs 에서 거절했다는 뜻이다 — 본문 검증이 400 이면 503 에 닿지 못한다
(`tests/integration/pocket_aide_notification_send_sc3.py` 가 같은 계약을 적어 놓았다).

기록에 남는 것은 본문과 식별자뿐이고 Authorization 값(SUT 의 서비스 어코운트 토큰)은
남지 않는다(프록시 계약). 다른 레인이 같은 프록시로 알림을 보낼 수 있으므로 기록은
`requester` 마커로 갈라 읽는다.
"""

from __future__ import annotations

import asyncio

from _approval_gate import notification_records, trace_url
from _helpers import base_url, open_session, wait_for_healthz

TOOL = "pocket_aide_notification_send"

#: 시나리오 5 가 정한 호출. `requester` 앞뒤 공백이 걷혀야 한다.
REQUESTER_RAW = "  Claude Code 세션 — dear-baby 정리  "
REQUESTER_TRIMMED = "Claude Code 세션 — dear-baby 정리"

#: 기록을 갈라 읽는 마커. 이 값이 들어간 기록만 이 파일의 것이다.
MARKER = "dear-baby 정리"

#: PocketAide 가 본문을 받아들인 뒤 APNs 가 없어 건다 거부(실물 응답).
EXPECTED_STATUS = 503


async def test_notification_body_carries_the_trimmed_requester(session, trace) -> dict:
    result = await session.call_tool(
        TOOL, {"title": "x", "requester": REQUESTER_RAW}
    )
    # 도구 호출 자체는 실패한다 — PocketAide 에 APNs 가 없어 503 이 온다. 그래서 여기서는
    # 도구 결과가 아니라 **기록**을 본다(아래 함수가 읽는다).
    assert result.content, result

    records = [
        record
        for record in notification_records(trace)
        if MARKER in str(record.get("body", {}))
    ]
    assert records, (
        "알림 본문 기록이 없다 — 프록시가 POST /api/external/notifications 본문을 "
        "기록하는지(기록면 계약), 또는 SUT 가 알림을 보냈는지(APPROVAL_GATE_URL 배선) "
        "둘 중 하나가 깨졌다"
    )
    record = records[-1]
    assert record["status"] == EXPECTED_STATUS, (
        f"PocketAide 가 본문을 받아들이지 않았다: {record}"
    )
    body = record.get("body") or {}
    assert body.get("requester") == REQUESTER_TRIMMED, (
        f"받은 본문의 requester 가 공백을 걷은 값이 아니다: {body.get('requester')!r}"
    )
    assert body.get("title") == "x", body
    return record


async def test_tool_schema_advertises_the_requester(session) -> None:
    tools = {tool.name: tool for tool in (await session.list_tools()).tools}
    assert TOOL in tools, sorted(tools)
    schema = tools[TOOL].inputSchema
    properties = schema.get("properties", {})
    assert set(properties) == {"title", "body", "requester"}, sorted(properties)
    required = schema.get("required", [])
    assert len(required) == 2 and set(required) == {"title", "requester"}, required


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    print("--- pocket-aide-notification-send/시나리오 5 (받은 본문 · 입력 스키마) ---")
    with trace_url() as trace:
        async with open_session(url) as session:
            record = await test_notification_body_carries_the_trimmed_requester(session, trace)
            await test_tool_schema_advertises_the_requester(session)
    print(f"ok: test-pocket-aide-notification-send.md#시나리오 5 — {record['body']}")
    print("ok: pocket-aide-notification-send/AC5")


if __name__ == "__main__":
    asyncio.run(run())
