"""변경 스트림 관측 — 창 안의 MODIFIED, 창 상한, resume, 민감 종류의 승인.

검증 시나리오: test-resource-generic.md#시나리오 6
실행 대상: primary

Go 단위(``internal/mcp/resource_test.go`` 의 ``TestWatchWindowBounds`` ·
``TestWatchOnGatedKindRequiresApproval`` · ``TestWatchCarriesTheResumePoint``)가 인자
계약과 게이트 분기를 이미 단언한다. **그 층이 못 보는 것**이 이 파일의 이유다: 실
apiserver 가 열린 창 안에서 `MODIFIED` 를 실제로 밀어 주는지, 창이 닫히면 응답이
돌아오는지, 그리고 `resourceVersion` 을 준 재관측이 실물 스트림에서 **이전 변경을 다시
주지 않는지**.

댄스의 순서가 케이스마다 다르다. 평범한 종류의 창은 호출 즉시 열리므로 **호출 뒤에**
대상을 흔들고, 민감 종류(`kind=Secret`)의 창은 승인이 떨어져야 열리므로 **승인 뒤에**
흔든다. 순서를 바꾸면 변경이 창 밖에서 일어나 이벤트 0 으로 헛실패한다.

**이벤트 수 상한(100)의 발화는 여기서 재지 않는다.** 한 창 안에 100 개를 넘기려면 공유
클러스터에서 비결정적인 양의 쓰기를 밀어 넣어야 하고, 그 자체가 다른 파일의 관측을
흔든다. 그 상한의 발화는 Go 단위의 몫이고, 여기서는 **상한이 응답 계약으로 서 있는 것**
(`truncated` 필드가 있고 이벤트가 그 수를 넘지 않는다)까지를 잰다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from mcp.shared.exceptions import McpError

from _gatekeeper import decide, gatekeeper_url, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"

WATCH_DEPLOYMENT = "rg-ac6-watch"
WATCH_SECRET = "rg-ac6-secret"
LABEL = "app.kubernetes.io/name"

#: ``internal/k8s/resource.go::WatchMaxEvents``.
MAX_EVENTS = 100


def _kubectl(*args: str, check: bool = True) -> str:
    return subprocess.run(
        ["kubectl", *args], text=True, check=check, capture_output=True
    ).stdout


def _apply(manifest: dict) -> None:
    subprocess.run(
        ["kubectl", "apply", "-f", "-"],
        input=json.dumps(manifest),
        text=True,
        check=True,
        capture_output=True,
    )


def _seed() -> None:
    _apply(
        {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {
                "name": WATCH_DEPLOYMENT,
                "namespace": NAMESPACE,
                "labels": {LABEL: WATCH_DEPLOYMENT},
            },
            "spec": {
                "replicas": 1,
                "selector": {"matchLabels": {LABEL: WATCH_DEPLOYMENT}},
                "template": {
                    "metadata": {"labels": {LABEL: WATCH_DEPLOYMENT}},
                    "spec": {
                        "terminationGracePeriodSeconds": 0,
                        "containers": [
                            {"name": "quiet", "image": "registry.k8s.io/pause:3.9"}
                        ],
                    },
                },
            },
        }
    )
    _apply(
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {
                "name": WATCH_SECRET,
                "namespace": NAMESPACE,
                "labels": {LABEL: WATCH_SECRET},
            },
            "stringData": {"generation": "0"},
        }
    )


def _scale(replicas: int) -> None:
    _kubectl("-n", NAMESPACE, "scale", "deployment", WATCH_DEPLOYMENT,
             f"--replicas={replicas}")


def _touch_secret(generation: str) -> None:
    _kubectl(
        "-n", NAMESPACE, "patch", "secret", WATCH_SECRET, "--type=merge",
        "-p", json.dumps({"stringData": {"generation": generation}}),
    )


def _watch_args(kind: str, name: str, seconds: int, resource_version: str | None = None) -> dict:
    args: dict = {
        "apiVersion": "apps/v1" if kind == "Deployment" else "v1",
        "kind": kind,
        "namespace": NAMESPACE,
        "labelSelector": f"{LABEL}={name}",
        "watchSeconds": seconds,
    }
    if resource_version is not None:
        args["resourceVersion"] = resource_version
    return args


def _events(result) -> list[dict]:
    payload = result.structuredContent
    assert "truncated" in payload, f"응답에 이벤트 상한 표기가 없다: {payload}"
    events = payload["events"] or []
    assert len(events) <= MAX_EVENTS, (
        f"이벤트가 상한 {MAX_EVENTS} 를 넘겨 돌아왔다: {len(events)}"
    )
    return events


def _last_resource_version(events: list[dict]) -> str:
    versions = [
        event["object"]["metadata"]["resourceVersion"]
        for event in events
        if event.get("object")
    ]
    assert versions, f"이벤트에 resourceVersion 이 없다: {events}"
    return versions[-1]


async def test_a_modification_lands_inside_the_window(session) -> str:
    """창이 열린 동안 일어난 변경이 MODIFIED 로 오고, 창이 닫히면 응답이 돌아온다."""
    _scale(1)
    task = asyncio.create_task(
        session.call_tool("resource_watch", _watch_args("Deployment", WATCH_DEPLOYMENT, 10))
    )
    await asyncio.sleep(1.0)
    _scale(2)
    result = await task

    assert result.isError is False, result
    assert result.structuredContent["watchSeconds"] == 10, result.structuredContent
    events = _events(result)
    assert any(event["type"] == "MODIFIED" for event in events), (
        f"창 안의 변경이 MODIFIED 로 오지 않았다: {[e['type'] for e in events]}"
    )
    return _last_resource_version(events)


async def test_a_window_longer_than_the_cap_is_refused(session) -> None:
    try:
        await session.call_tool("resource_watch", _watch_args("Deployment", WATCH_DEPLOYMENT, 61))
    except McpError as exc:
        assert "watchSeconds" in str(exc), exc
    else:
        raise AssertionError("watchSeconds=61 이 거부되지 않았다(조용히 잘린 창일 수 있다)")


async def test_resuming_from_a_version_skips_what_was_already_seen(session, since: str) -> None:
    task = asyncio.create_task(
        session.call_tool(
            "resource_watch", _watch_args("Deployment", WATCH_DEPLOYMENT, 10, since)
        )
    )
    await asyncio.sleep(1.0)
    _scale(1)
    result = await task

    assert result.isError is False, result
    events = _events(result)
    assert events, "resume 창에서 아무 이벤트도 오지 않았다"
    for event in events:
        version = event["object"]["metadata"]["resourceVersion"]
        assert int(version) > int(since), (
            f"resume 이 이미 본 변경({since} 이하)을 다시 줬다: {version}"
        )


async def test_a_sensitive_kind_needs_approval_and_keeps_its_caps(session, gate) -> None:
    print("    (미승인 — 승인 요청을 띄우고 판정을 REJECTED 로 내려 거절을 태운다)")
    # 자동 거부 모드(`AUTO_REJECT`)를 쓰지 않는다: 그 모드는 gatekeeper 가 요청의 담당
    # 사용자에게 적용하는 설정이라 `GATEKEEPER_USER_ID` 를 물고 있는 gatekeeper-variant
    # 배포에서만 발화한다(시나리오 9 가 `실행 대상: gatekeeper-variant` 인 이유). primary
    # 의 요청에는 담당자가 없어 모드가 걸리지 않고, 요청은 승인 시한까지 PENDING 으로
    # 남았다가 타임아웃으로 끝난다 — 거부가 아니라 무응답이라 이 절을 재지 못한다.
    refused = asyncio.create_task(
        session.call_tool("resource_watch", _watch_args("Secret", WATCH_SECRET, 10))
    )
    row = await wait_for_pending(gate, WATCH_SECRET)
    await decide(gate, row["id"], "REJECTED")
    try:
        await refused
    except McpError as exc:
        assert "reject" in str(exc).lower(), exc
    else:
        raise AssertionError("승인 없이 Secret 스트림이 열렸다")

    print("    (승인 후 — 이벤트가 온다)")
    task = asyncio.create_task(
        session.call_tool("resource_watch", _watch_args("Secret", WATCH_SECRET, 10))
    )
    row = await wait_for_pending(gate, WATCH_SECRET)
    await decide(gate, row["id"], "APPROVED")
    await asyncio.sleep(1.0)
    _touch_secret("1")
    result = await task
    assert result.isError is False, result
    events = _events(result)
    assert any(event["type"] == "MODIFIED" for event in events), (
        f"승인된 Secret 스트림에 변경이 오지 않았다: {[e['type'] for e in events]}"
    )

    print("    (승인을 받아도 창 상한은 그대로다)")
    task = asyncio.create_task(
        session.call_tool("resource_watch", _watch_args("Secret", WATCH_SECRET, 61))
    )
    row = await wait_for_pending(gate, WATCH_SECRET)
    await decide(gate, row["id"], "APPROVED")
    try:
        await task
    except McpError as exc:
        assert "watchSeconds" in str(exc), exc
    else:
        raise AssertionError("민감 종류에서는 창 상한이 적용되지 않았다")


async def run() -> None:
    url = base_url()
    _seed()
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- resource-generic/시나리오 6 (창 안의 MODIFIED) ---")
            since = await test_a_modification_lands_inside_the_window(session)
            print("--- resource-generic/시나리오 6 (창 상한) ---")
            await test_a_window_longer_than_the_cap_is_refused(session)
            print("--- resource-generic/시나리오 6 (resume) ---")
            await test_resuming_from_a_version_skips_what_was_already_seen(session, since)
            print("--- resource-generic/시나리오 6 (민감 종류) ---")
            await test_a_sensitive_kind_needs_approval_and_keeps_its_caps(session, gate)
    print("ok: test-resource-generic.md#시나리오 6")


if __name__ == "__main__":
    asyncio.run(run())
