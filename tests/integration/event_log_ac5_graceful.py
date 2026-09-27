"""수집기 미설정·도달 불가에서의 graceful — 수집기 없는 하네스에서 기동·도구 응답·stdout 레코드가 그대로다.

검증 시나리오: test-event-log.md#시나리오 8
실행 대상: primary

그 전제를 단언 없이 두면 이 파일은 아무 배포에서나 통과하는 ``ping`` 스모크가 된다.

레코드는 호출 전후 ``tool=ping`` 레코드 수의 차로 집는다 — 이 파일은 레인을 선언하지 않아 레인들이
끝난 뒤 단독으로 돌므로 그 사이 남이 ``ping`` 을 부르지 않는다.
"""

from __future__ import annotations

import asyncio
import json
import re
import subprocess

from _eventlog import exactly_one_new, records, select, wait_for_new
from _helpers import base_url, open_session, wait_for_healthz

SERVER_NAMESPACE = "homelab-k3s-mcp"
SERVER_DEPLOYMENT = "homelab-k3s-mcp"

TOOL = "ping"

#: 수집 경로의 구성 요소 이름 — 파드 이름(클러스터 층)과 서버 env·args(설계 층) 양쪽에서 찾아 0건이어야 한다.
COLLECTOR_RE = re.compile(r"alloy|loki|collector|sink", re.IGNORECASE)


def _kubectl_json(*args: str) -> dict:
    return json.loads(subprocess.check_output(["kubectl", *args, "-o", "json"], text=True))


def test_the_harness_is_the_unconfigured_variant() -> None:
    pods = _kubectl_json("get", "pods", "-A")
    collectors = sorted(
        f"{item['metadata']['namespace']}/{item['metadata']['name']}"
        for item in pods["items"]
        if COLLECTOR_RE.search(item["metadata"]["name"])
    )
    assert not collectors, f"하네스에 수집기 파드가 있다 — 「미설정」 변형이 아니다: {collectors}"


def test_the_server_has_no_axis_that_could_tell_the_variants_apart() -> dict:
    deploy = _kubectl_json("-n", SERVER_NAMESPACE, "get", "deploy", SERVER_DEPLOYMENT)
    hits: list[str] = []
    for container in deploy["spec"]["template"]["spec"]["containers"]:
        for env in container.get("env") or []:
            if COLLECTOR_RE.search(env["name"]) or COLLECTOR_RE.search(str(env.get("value", ""))):
                hits.append(f"{container['name']} env {env['name']}")
        for word in (container.get("args") or []) + (container.get("command") or []):
            if COLLECTOR_RE.search(word):
                hits.append(f"{container['name']} argv {word}")
    assert not hits, f"서버가 수집기 설정을 갖는다 — 두 변형이 갈리는 축이 생겼다: {hits}"
    return deploy


def test_the_server_started_and_stayed_up(deploy: dict) -> None:
    available = deploy["status"].get("availableReplicas", 0)
    assert available >= 1, f"가용 레플리카가 {available} — 기동 실패\n{json.dumps(deploy['status'])}"
    labels = deploy["spec"]["selector"]["matchLabels"]
    selector = ",".join(f"{k}={v}" for k, v in sorted(labels.items()))
    pods = _kubectl_json("-n", SERVER_NAMESPACE, "get", "pods", "-l", selector)
    restarts = {
        f"{pod['metadata']['name']}/{status['name']}": status["restartCount"]
        for pod in pods["items"]
        for status in pod["status"].get("containerStatuses") or []
    }
    assert restarts, f"셀렉터 {selector} 에 파드가 없다"
    assert all(count == 0 for count in restarts.values()), f"재시작이 있었다: {restarts}"


async def test_the_tool_answers_and_leaves_exactly_one_record(session) -> None:
    before = len(select(records(SERVER_NAMESPACE, SERVER_DEPLOYMENT), tool=TOOL))

    result = await session.call_tool(TOOL, {})
    assert result.isError is False, result
    assert result.content and result.content[0].text == "pong", result.content

    found = wait_for_new(SERVER_NAMESPACE, SERVER_DEPLOYMENT, before, tool=TOOL)
    record = exactly_one_new(found, before, TOOL)
    assert record["result"] == "success", record
    assert record["reason"] == "", record
    assert "principal" in record and "target.kind" in record, record


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    print("--- event-log/시나리오 8 (수집기 없는 하네스 · 변형 실재 → 기동·응답·레코드) ---")
    test_the_harness_is_the_unconfigured_variant()
    deploy = test_the_server_has_no_axis_that_could_tell_the_variants_apart()
    test_the_server_started_and_stayed_up(deploy)
    async with open_session(url) as session:
        await test_the_tool_answers_and_leaves_exactly_one_record(session)
    print("ok: test-event-log.md#시나리오 8")


if __name__ == "__main__":
    asyncio.run(run())
