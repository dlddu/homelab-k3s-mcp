"""노출 경로가 인증 경계를 우회하지 않는다 — 무인증 /metrics 는 표만 내고, /mcp 는 여전히 401 이며, 끈 변형도 정상이다.

검증 시나리오: test-metrics.md#시나리오 5
실행 대상: auth-variant

시나리오의 「인증이 켜진 기본 배포」는 이 변형이다 — 주 배포는 kind 오버레이가 인증을 내려 두어
``/mcp`` 의 401 을 볼 수 없다(``auth-fixture.yaml`` 머리말). 메트릭 리스너에는 파드 포트로 닿는다
(``_metrics.pod_port_forward``). 「그 응답으로 도구 실행·리소스 조회가 가능한지」는 세 겹으로
잰다: ⑴ 무인증 ``GET /metrics`` 가 200 이되 본문의 모든 샘플이 네 패밀리의 것이고 라벨 전집이
닫혀 있다(``assert_label_universe``) — 도구 출력이나 클러스터 객체가 실릴 자리가 없다 ⑵ 같은
리스너에 ``/mcp`` 를 부르면 404 다(``server.MetricsApp`` 의 mux 는 ``GET /metrics`` 한 경로) ⑶ 본
배포의 ``/mcp`` 는 무인증이면 401 그대로이고, 키를 실으면 ``pong`` 이다 — 경계가 살아 있다는
대조군이지 죽은 엔드포인트가 아니다.

「노출을 끈 변형」은 이 파일이 세우고 거둔다(``tests/k8s/kind/metrics-off-variant.yaml`` —
``auth-fixture.yaml`` 과 같은 구성에 ``METRICS_DISABLED=1``). 끈 것이 실재함은 서버가 남기는
경고 줄(``METRICS_DISABLED is set``)로 먼저 확인한다 — 그 줄이 없으면 아래의 「9090 이 닫혀 있다」는
아직 뜨지 않은 리스너와 구별되지 않는다. 그 다음 기동(가용 레플리카 ≥ 1 · 재시작 0) · ``ping`` →
``pong`` · 파드 9090 으로의 GET 이 응답 없이 실패함을 잰다. 네임스페이스는 ``finally`` 에서
지운다(``--wait=false`` — 다음 파일이 기다릴 이유가 없다).
"""

from __future__ import annotations

import asyncio
import json
import pathlib
import subprocess
import time

import httpx

from _auth_variant import API_KEY
from _helpers import base_url, open_session, port_forward, wait_for_healthz
from _metrics import METRICS_PORT, Snapshot, assert_label_universe, metrics_url, pod_port_forward, scrape

AUTH_NAMESPACE = "homelab-k3s-mcp-auth"
AUTH_DEPLOYMENT = "homelab-k3s-mcp-auth"
AUTH_METRICS_LOCAL_PORT = 19091

OFF_MANIFEST = pathlib.Path(__file__).resolve().parent.parent / "k8s" / "kind" / "metrics-off-variant.yaml"
OFF_NAMESPACE = "homelab-k3s-mcp-metrics-off"
OFF_DEPLOYMENT = "homelab-k3s-mcp-metrics-off"
OFF_LOCAL_PORT = 18088
OFF_METRICS_LOCAL_PORT = 19093
OFF_LOG_MARK = "METRICS_DISABLED is set"

TOOLS_CALL = {"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": "ping", "arguments": {}}}
MCP_HEADERS = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}


def _kubectl(*args: str, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(["kubectl", *args], check=check, capture_output=True, text=True)


def test_the_exposition_is_only_the_table(metrics: str) -> Snapshot:
    response = httpx.get(f"{metrics}/metrics", timeout=10.0)
    assert response.status_code == 200, (response.status_code, response.text[:200])
    snapshot = Snapshot(response.text)
    assert_label_universe(snapshot)
    for line in response.text.splitlines():
        if not line or line.startswith("#"):
            continue
        assert line.startswith("mcp_"), f"표 밖의 줄이 노출됐다: {line!r}"
    for needle in ('"apiVersion"', '"kind":', "{\n", "pong"):
        assert needle not in response.text, f"노출에 도구 출력·클러스터 객체의 흔적이 있다: {needle!r}"
    return snapshot


def test_the_metrics_listener_serves_no_tool_path(metrics: str) -> None:
    for method, path in (("POST", "/mcp"), ("GET", "/mcp"), ("POST", "/metrics")):
        body = TOOLS_CALL if method == "POST" else None
        response = httpx.request(method, f"{metrics}{path}", json=body, headers=MCP_HEADERS, timeout=10.0)
        assert response.status_code in (404, 405), f"{method} {path} 가 메트릭 리스너에서 {response.status_code}"
        assert '"jsonrpc"' not in response.text, f"{method} {path} 가 메트릭 리스너에서 JSON-RPC 로 답했다: {response.text[:200]}"


def test_mcp_still_requires_auth(url: str) -> None:
    anonymous = httpx.post(f"{url}/mcp", json=TOOLS_CALL, headers=MCP_HEADERS, timeout=10.0)
    assert anonymous.status_code == 401, (anonymous.status_code, anonymous.text[:200])
    with_key = httpx.post(
        f"{url}/mcp", json=TOOLS_CALL, headers={**MCP_HEADERS, "Authorization": f"Bearer {API_KEY}"}, timeout=10.0
    )
    assert with_key.status_code == 200 and '"pong"' in with_key.text, (with_key.status_code, with_key.text[:200])


def _off_variant_started() -> None:
    _kubectl("apply", "-f", str(OFF_MANIFEST))
    _kubectl("-n", OFF_NAMESPACE, "rollout", "status", f"deploy/{OFF_DEPLOYMENT}", "--timeout=120s")


def test_the_off_variant_really_switched_exposure_off() -> None:
    log = _kubectl("-n", OFF_NAMESPACE, "logs", f"deploy/{OFF_DEPLOYMENT}", "--tail=-1").stdout
    assert OFF_LOG_MARK in log, f"변형의 로그에 {OFF_LOG_MARK!r} 가 없다 — 끈 것이 아니다:\n{log[-800:]}"


def test_the_off_variant_started_and_stayed_up() -> None:
    deploy = json.loads(_kubectl("-n", OFF_NAMESPACE, "get", "deploy", OFF_DEPLOYMENT, "-o", "json").stdout)
    available = deploy["status"].get("availableReplicas", 0)
    assert available >= 1, f"가용 레플리카가 {available} — 기동 실패\n{json.dumps(deploy['status'])}"
    pods = json.loads(_kubectl("-n", OFF_NAMESPACE, "get", "pods", "-o", "json").stdout)
    restarts = {
        f"{pod['metadata']['name']}/{status['name']}": status["restartCount"]
        for pod in pods["items"]
        for status in pod["status"].get("containerStatuses") or []
    }
    assert restarts and all(count == 0 for count in restarts.values()), f"재시작이 있었다: {restarts}"


async def test_the_off_variant_still_serves_tools() -> None:
    with port_forward(OFF_NAMESPACE, OFF_DEPLOYMENT, 80, OFF_LOCAL_PORT, "/healthz") as url:
        wait_for_healthz(url)
        async with open_session(url, headers={"Authorization": f"Bearer {API_KEY}"}) as session:
            pong = await session.call_tool("ping", {})
            assert pong.isError is False and pong.content[0].text == "pong", pong


def test_the_off_variant_exposes_nothing() -> None:
    with pod_port_forward(OFF_NAMESPACE, OFF_DEPLOYMENT, METRICS_PORT, OFF_METRICS_LOCAL_PORT, None) as url:
        for attempt in range(3):
            try:
                response = httpx.get(f"{url}/metrics", timeout=3.0)
            except httpx.HTTPError:
                time.sleep(0.5)
                continue
            raise AssertionError(
                f"끈 변형의 9090 이 답했다(시도 {attempt + 1}): {response.status_code} {response.text[:120]!r}"
            )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    with metrics_url(AUTH_NAMESPACE, AUTH_DEPLOYMENT, AUTH_METRICS_LOCAL_PORT) as metrics:
        print("--- metrics/시나리오 5 (무인증 /metrics — 표만, 라벨 전집 닫힘) ---")
        test_the_exposition_is_only_the_table(metrics)
        print("--- metrics/시나리오 5 (메트릭 리스너에 /mcp 없음) ---")
        test_the_metrics_listener_serves_no_tool_path(metrics)
    print("--- metrics/시나리오 5 (/mcp 는 무인증 401 · 키로 pong) ---")
    test_mcp_still_requires_auth(url)

    try:
        _off_variant_started()
        print("--- metrics/시나리오 5 (METRICS_DISABLED 변형 — 끈 것이 실재 · 기동 · 도구 · 9090 닫힘) ---")
        test_the_off_variant_really_switched_exposure_off()
        test_the_off_variant_started_and_stayed_up()
        await test_the_off_variant_still_serves_tools()
        test_the_off_variant_exposes_nothing()
    finally:
        _kubectl("delete", "namespace", OFF_NAMESPACE, "--ignore-not-found", "--wait=false", check=False)
    print("ok: test-metrics.md#시나리오 5")


if __name__ == "__main__":
    asyncio.run(run())
