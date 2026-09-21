"""자격증명 값 비노출 — 발급 도구 셋을 부른 뒤 레코드 전문에서 값을 찾아 0건이다.

검증 시나리오: test-event-log.md#시나리오 4
실행 대상: primary
추가 인자: trace

레코드는 SUT 파드 stdout 의 ``msg="tool call"`` slog 한 줄이다(``internal/eventlog``). Go 단위
``internal/mcp/eventlog_test.go::TestRecordsCarryNoCredentialsPayloadsOrBodies`` 가 가짜
통합 위에서 같은 사실을 in-process 로 단언한다 — 이 파일이 재는 것은 **배포된 서버가 실제로
쓴 줄**이다. 값이 타입상 실릴 자리가 없다는 것과, 핸들러·로거 어느 층도 그 줄에 값을 덧붙이지
않았다는 것은 다른 관측이다.

**네거티브를 공허하지 않게 하는 두 단언이 먼저다.** ⑴ 응답에 값이 있다 — GitHub 토큰·Grafana
토큰은 `.env` 본문에서, AWS 는 설정 본문에서 뽑는다. 값을 응답에서 뽑지 못하면 「레코드에
없다」는 아무것도 흐르지 않는 서버로도 통과하므로 그 자리에서 멈춘다. ⑵ 레코드가 도구당
**정확히 하나** 늘었다 — 이 파일은 레인을 선언하지 않아 레인들이 끝난 뒤 단독으로 돌므로,
호출 전후 도구별 레코드 수의 차가 곧 이 호출의 레코드다. 레코드가 늘지 않았으면 검색할 전문이
없는 것이고, 그것도 통과가 아니다.

**바늘은 응답·픽스처·기록 세 출처에서 모은다.** 응답에서 뽑은 발급 값 셋, ci.yml 시크릿이
서버에 쥐어 준 API 키(`GRAFANA_ISSUER_TOKEN`·MinIO 베이스 키 — ``_helpers.BASE_ACCESS_KEY_ID``),
그리고 http-trace 가 기록한 STS 발급 access key id. 마지막 것은 응답 어디에도 없는
**서버 내부의** 자격증명이라 기록 프록시 없이는 값을 알 수 없고, 그래서 이 파일이 `trace` 를
받는다. JWT 는 값을 알 수 없다 — 이 배포는 인증을 끈 primary 라 호출자 JWT 가 없고, 서버가
GitHub 에 제시하는 App JWT 는 서버 안에서 서명돼 밖으로 나오지 않는다. 그래서 JWT 는 값이
아니라 **모양**(`eyJ` 로 시작하는 세 마디)으로 훑는다. 호출자 JWT 의 비노출은 ``eventlog.Principal``
이 subject 만 담는 타입이라는 것과 ``internal/server/eventlog_test.go`` 의 API 키 단언이
맡는다.
"""

from __future__ import annotations

import asyncio
import re
import subprocess
import time
from collections import Counter

from _aws_config import EXPECTED_CONTENT, ROLE_ARN
from _helpers import (
    BASE_ACCESS_KEY_ID,
    assume_role_records,
    base_url,
    fetch_trace,
    open_session,
    parse_env_resource,
    trace_url,
    wait_for_healthz,
)

SERVER_NAMESPACE = "homelab-k3s-mcp"
SERVER_DEPLOYMENT = "homelab-k3s-mcp"

RECORD_MARK = 'msg="tool call"'

#: ci.yml 「Create test Grafana secret」 의 GRAFANA_ISSUER_TOKEN 과 같아야 한다.
GRAFANA_ISSUER_TOKEN = "glsa_mock_issuer"

TOOLS = ("github_app_installation_token", "grafana_token", "aws_config_get")

#: 응답이 돌아온 뒤 kubelet 의 로그 읽기가 그 줄을 보여 주기까지의 창.
RECORD_BUDGET = 15.0

JWT_RE = re.compile(r"eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+")
ENV_VALUE_RE = re.compile(r"^(?P<key>[A-Z_]+)=(?P<value>\S+)$", re.MULTILINE)


def _records() -> list[str]:
    log = subprocess.check_output(
        ["kubectl", "-n", SERVER_NAMESPACE, "logs", f"deploy/{SERVER_DEPLOYMENT}", "--tail=-1"],
        text=True,
    )
    return [line for line in log.splitlines() if RECORD_MARK in line]


def _by_tool(records: list[str]) -> Counter:
    counts: Counter = Counter()
    for line in records:
        for tool in TOOLS:
            if f"tool={tool} " in line:
                counts[tool] += 1
    return counts


def _env_value(env_text: str, key: str) -> str:
    for match in ENV_VALUE_RE.finditer(env_text):
        if match.group("key") == key:
            return match.group("value")
    raise AssertionError(f"{key}= 가 .env 본문에 없다:\n{env_text}")


async def test_the_issued_values_are_in_the_responses_and_not_in_the_records(
    session, trace: str
) -> None:
    before = _by_tool(_records())

    github = await session.call_tool("github_app_installation_token", {})
    assert github.isError is False, github
    github_token = _env_value(parse_env_resource(github)[0], "GITHUB_TOKEN")

    grafana = await session.call_tool("grafana_token", {})
    assert grafana.isError is False, grafana
    grafana_token = _env_value(parse_env_resource(grafana)[0], "GRAFANA_TOKEN")

    aws = await session.call_tool("aws_config_get", {})
    assert aws.isError is False, aws
    assert aws.structuredContent["content"] == EXPECTED_CONTENT, aws.structuredContent

    issued_keys = {
        record["sts"]["issuedAccessKeyId"]
        for record in assume_role_records(fetch_trace(trace), ROLE_ARN)
        if record["sts"].get("issuedAccessKeyId")
    }
    assert issued_keys, (
        f"http-trace 에 {ROLE_ARN} 의 AssumeRole 발급 키가 없다 — 서버 내부 자격증명 바늘이 "
        f"비면 그 축의 검색이 공허해진다"
    )

    deadline = time.monotonic() + RECORD_BUDGET
    while True:
        records = _records()
        after = _by_tool(records)
        if all(after[tool] > before[tool] for tool in TOOLS) or time.monotonic() >= deadline:
            break
        time.sleep(0.5)
    for tool in TOOLS:
        assert after[tool] - before[tool] == 1, (
            f"{tool} 의 레코드가 {before[tool]} → {after[tool]} — 호출당 정확히 하나여야 한다"
        )
    fresh = {
        tool: [line for line in records if f"tool={tool} " in line][before[tool]:]
        for tool in TOOLS
    }
    for tool, lines in fresh.items():
        assert len(lines) == 1 and "result=success" in lines[0], (tool, lines)

    text = "\n".join(line for lines in fresh.values() for line in lines)
    needles = {
        "GitHub 발급 토큰": github_token,
        "Grafana 발급 토큰": grafana_token,
        **{f"AWS 설정 본문 {line!r}": line for line in EXPECTED_CONTENT.splitlines() if line},
        "Grafana 발급 API 키": GRAFANA_ISSUER_TOKEN,
        "MinIO 베이스 키": BASE_ACCESS_KEY_ID,
        **{f"STS 발급 키 {key}": key for key in sorted(issued_keys)},
    }
    for label, needle in needles.items():
        assert needle not in text, f"레코드에 {label}이 실렸다:\n{text}"
    assert not JWT_RE.search(text), f"레코드에 JWT 모양의 값이 실렸다:\n{text}"


async def run() -> None:
    url = base_url()
    trace = trace_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- event-log/시나리오 4 (발급 도구 셋 → 레코드 전문 검색) ---")
        await test_the_issued_values_are_in_the_responses_and_not_in_the_records(
            session, trace
        )
    print("ok: test-event-log.md#시나리오 4")


if __name__ == "__main__":
    asyncio.run(run())
