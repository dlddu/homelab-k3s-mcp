"""Deployed-server e2e for github-commit-status/AC3 (입력 검증이 상류보다 앞선다).

검증 시나리오: test-github-commit-status.md#시나리오 3
실행 대상: primary
병렬 레인: github-app

이 시나리오의 주장은 반환값이 아니라 **일어나지 않은 요청**에 있다. 그래서 판정
근거는 도구 응답이 아니라 github-mock 의 요청 로그이고, 로그를 직접 읽는 파일이
자매 `github_app_installation_token_ac5.py` 에 이어 둘째가 된다. 아직 공유 모듈로
빼지 않은 것은 그쪽이 노브(`/_admin/config`)까지 쓰는 반면 이쪽은 요청 로그만
읽어, 지금 합치면 공유 표면이 두 파일의 합집합이 되기 때문이다.
"""

from __future__ import annotations

import asyncio
import os
from typing import Any

import httpx
from mcp import ClientSession

from _helpers import base_url, open_session, wait_for_healthz

# tests/k8s/kind/github-mock.yaml 의 admin 표면. 포트 8093 은 ci.yml 의
# "Run integration tests (primary deployment)" 스텝이 여는 포트포워드와 짝이다.
MOCK_URL = os.environ.get("GITHUB_MOCK_URL", "http://127.0.0.1:8093").rstrip("/")

# 유효한 한 벌. 각 케이스는 여기서 한 필드만 비틀어, 거부가 그 필드 때문이라는 것이
# 나머지 다섯 케이스의 통과로 교차 확인되게 한다.
VALID_ARGUMENTS = {
    "repository": "homelab-k3s-mcp",
    "sha": "0" * 40,
    "state": "success",
    "context": "homelab-k3s-mcp/e2e",
}


def _without(field: str) -> dict[str, Any]:
    arguments = dict(VALID_ARGUMENTS)
    arguments.pop(field)
    return arguments


def _replacing(field: str, value: str) -> dict[str, Any]:
    arguments = dict(VALID_ARGUMENTS)
    arguments[field] = value
    return arguments


# 여섯 케이스는 시나리오가 세는 것과 같은 여섯이다. 기대 문면을 케이스마다 따로
# 적는 것이 이 파일의 네거티브 컨트롤이다 — "상류 요청 0" 은 서버가 아예 미설정이라
# 모든 호출을 거부하는 배포에서도 성립하므로, 거부가 **입력 검증**에서 왔다는 것은
# 필드를 지목하는 문면만이 가른다.
CASES = [
    ("repository 누락", _without("repository"), "repository is required"),
    ("축약 sha", _replacing("sha", "abc1234"), "40-character hex commit sha"),
    ("허용되지 않는 state", _replacing("state", "ok"), 'state "ok" must be one of'),
    ("context 누락", _without("context"), "context is required"),
    ("141자 description", _replacing("description", "d" * 141), "above the 140 limit"),
    ("http 아닌 target_url", _replacing("target_url", "ftp://x"), "absolute http(s) URL"),
]


def reset_recorded() -> None:
    httpx.delete(f"{MOCK_URL}/_admin/requests", timeout=10.0).raise_for_status()


def recorded() -> list[dict[str, Any]]:
    response = httpx.get(f"{MOCK_URL}/_admin/requests", timeout=10.0)
    response.raise_for_status()
    return response.json()["requests"]


def refusal_text(result) -> str:
    assert result.isError is True, f"call did not refuse: {result}"
    assert result.content, result
    block = result.content[0]
    assert block.type == "text", block
    return block.text


async def test_github_commit_status_ac3_invalid_input_never_reaches_github(
    session: ClientSession,
) -> None:
    """AC: github-commit-status/AC3

    여섯 호출을 한 창에 몰아 넣고 로그를 **끝에 한 번** 읽는다. 케이스마다 재면
    "다섯은 막고 하나는 흘렸다" 를 그 하나의 자리에서 잡을 수 있지만, 상류로 새는
    요청이 토큰 발급처럼 앞선 단계에서 날아갈 수도 있어 창을 좁히면 오히려 놓친다.
    """
    reset_recorded()

    for label, arguments, expected in CASES:
        result = await session.call_tool("github_commit_status_create", arguments)
        text = refusal_text(result)
        assert expected in text, f"{label}: refusal text = {text!r}, expected {expected!r}"

    leaked = recorded()
    assert leaked == [], f"invalid input reached the upstream: {leaked}"


async def test_github_commit_status_ac3_recorder_sees_a_call_that_passes_validation(
    session: ClientSession,
) -> None:
    """포지티브 컨트롤 — 같은 측정이 통과한 호출은 실제로 잡는다.

    위 단언은 "0 건" 이라, 로그가 어떤 이유로든 비어 있기만 하면 통과한다(스텁이
    기록을 멈췄거나, 배포가 GitHub App 미설정이라 검증 이전에 거부하거나). 유효한
    한 벌은 검증과 접두사 검사를 지나 상류에 닿으므로, 그때 로그가 **비지 않는
    것**이 그 두 실패 모드를 배제한다. 호출 자체의 성패는 재지 않는다 — 이 시나리오의
    주장이 아니고, `POST …/statuses/{sha}` 핸들러는 아직 시나리오 1 의 몫이다.
    """
    reset_recorded()
    await session.call_tool("github_commit_status_create", dict(VALID_ARGUMENTS))

    assert recorded(), (
        "a call that passes validation recorded nothing upstream — "
        "the zero-request assertion above would hold vacuously"
    )
    reset_recorded()


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- github-commit-status/AC3 ---")
        try:
            await test_github_commit_status_ac3_invalid_input_never_reaches_github(session)
            await test_github_commit_status_ac3_recorder_sees_a_call_that_passes_validation(
                session
            )
        finally:
            # 요청 로그는 mock 의 공유 프로세스 상태다. 레인이 한 번에 한 파일을
            # 돌리므로 남겨 두면 다음 파일이 자기 것이 아닌 기록을 보게 된다.
            reset_recorded()
        print("ok: github-commit-status/AC3")


if __name__ == "__main__":
    asyncio.run(run())
