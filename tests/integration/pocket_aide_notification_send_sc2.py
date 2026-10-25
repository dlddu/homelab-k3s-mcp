"""Deployed-server e2e for pocket-aide-notification-send/AC2 (입력 검증이 PocketAide 보다 앞선다).

검증 시나리오: test-pocket-aide-notification-send.md#시나리오 2
실행 대상: primary

주 배포에는 PocketAide 가 배선돼 있지 않다. 그래서 유효한 제목이 미설정 거부로 끝나는 것을 함께
단언한다 — 잘못된 제목의 거부가 그 미설정 거부와 다른 문구라는 것이, 검증이 PocketAide 설정보다
앞선다는 이 시나리오의 주장이다.
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _helpers import base_url, open_session, wait_for_healthz

TOOL = "pocket_aide_notification_send"

CASES = [
    ("title 누락", {}, "invalid notification: title is required"),
    ("공백뿐인 title", {"title": "   "}, "invalid notification: title is required"),
    ("121자 title", {"title": "가" * 121}, "invalid notification: title must be 120 characters or fewer"),
]


def refusal_text(result) -> str:
    assert result.isError is True, f"call did not refuse: {result}"
    assert result.content, result
    block = result.content[0]
    assert block.type == "text", block
    return block.text


async def test_pocket_aide_notification_send_ac2_bad_titles_refused_first(
    session: ClientSession,
) -> None:
    for label, arguments, expected in CASES:
        text = refusal_text(await session.call_tool(TOOL, arguments))
        assert text == expected, f"{label}: refusal text = {text!r}, expected {expected!r}"

    text = refusal_text(await session.call_tool(TOOL, {"title": "valid"}))
    assert "invalid notification" not in text and "pocket-aide unavailable" in text, (
        f"a valid title should pass validation and stop at the unconfigured client: {text!r}"
    )

    survivor = await session.call_tool("ping", {})
    assert survivor.isError is False and survivor.content[0].text == "pong", survivor


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- pocket-aide-notification-send/AC2 ---")
        await test_pocket_aide_notification_send_ac2_bad_titles_refused_first(session)
        print("ok: pocket-aide-notification-send/AC2")


if __name__ == "__main__":
    asyncio.run(run())
