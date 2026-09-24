"""컬렉션 일괄 삭제 — 겨냥한 선택만 사라지고, 그 선택이 승인 화면에 통째로 실린다.

검증 시나리오: test-resource-generic.md#시나리오 12
실행 대상: primary
병렬 레인: gate-objects

「다른 레이블 2개는 남는다」가 **실 apiserver 층에서만** 관측된다는 점이 이 파일의
존재 이유다.
단위 층의 가짜는 자기가 받은 셀렉터를 기록할 뿐이라, 셀렉터를 흘리는 구현과 지키는
구현을 구별하지 못한다 — 구별하는 것은 apiserver 뿐이다.

대상은 이 파일이 스스로 세우는 일회용 ConfigMap 이다(기준선 워크로드는 건드리지
않는다). ConfigMap 을 쓰는 것은 자매 ``resource_generic_ac10.py`` 가 단건 삭제에서
쓰는 것과 같은 이유다 — 종료 유예도 파이널라이저도 없어 「사라졌다」가 곧 관측이고,
그 관측에 파드 수명주기의 흔들림이 섞이지 않는다.

**세 그룹을 레이블로 갈라 둔다.** ``doomed`` 5개는 승인받아 지우는 선택, ``spared``
2개는 같은 네임스페이스·다른 레이블의 대조군, ``grown`` 은 승인과 실행 사이에 늘어나는
선택이다. 그룹을 나누지 않고 한 집합을 재사용하면 앞 케이스의 삭제가 뒷 케이스의 전제를
지워, 실패가 「거부되지 않았다」가 아니라 「대상이 없다」로 나타난다.

각 그룹은 케이스 직전에 **레이블째 지우고 다시 세운다** — 재실행 가능성을 위한 청소가
아니라 그 자리의 판별식을 세우는 장치다.

``workload-test`` 를 보는 다른 레인과 동시에 돌아도 되는 것은 삭제·승인 화면·대조군이 전부
``rg-ac12-group`` 레이블로 좁혀져 있어서다 — 이 레이블 없이 셀렉터를 쓰는 케이스를 더하면
남의 객체가 선택에 섞인다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess
import time

from mcp.shared.exceptions import McpError

from _gatekeeper import decide, gatekeeper_url, list_requests, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE

LABEL = "rg-ac12-group"
DOOMED = [f"rg-ac12-doomed-{i}" for i in range(1, 6)]
SPARED = [f"rg-ac12-spared-{i}" for i in range(1, 3)]
GROWN = [f"rg-ac12-grown-{i}" for i in range(1, 3)]
LATECOMER = "rg-ac12-grown-3"
#: 아무것도 달고 있지 않은 레이블 값. 0건 케이스의 마커이기도 하다 — 승인 요청이
#: 만들어졌다면 context 의 labelSelector 줄에 이 문자열이 실려 있을 것이다.
ABSENT = "nothing-carries-this"

DELETION_BUDGET = 60


def _kubectl(*args: str, input: str | None = None) -> str:
    proc = subprocess.run(
        ["kubectl", "-n", NAMESPACE, *args],
        capture_output=True,
        text=True,
        check=True,
        input=input,
    )
    return proc.stdout


def _config_map(name: str, group: str) -> None:
    _kubectl(
        "apply", "-f", "-",
        input=json.dumps(
            {
                "apiVersion": "v1",
                "kind": "ConfigMap",
                "metadata": {
                    "name": name,
                    "namespace": NAMESPACE,
                    "labels": {LABEL: group},
                },
                "data": {"key": "value"},
            }
        ),
    )


def _group(group: str, names: list[str]) -> None:
    """레이블 그룹을 매니페스트가 아니라 **정확히 이 이름들**로 되돌린다."""
    _kubectl("delete", "configmap", "-l", f"{LABEL}={group}", "--ignore-not-found")
    for name in names:
        _config_map(name, group)


def _exists(name: str) -> bool:
    return (
        subprocess.run(
            ["kubectl", "-n", NAMESPACE, "get", "configmap", name],
            capture_output=True,
            text=True,
        ).returncode
        == 0
    )


def _pending_count(gate: str, marker: str) -> int:
    return sum(
        1 for r in list_requests(gate, status="PENDING") if marker in r.get("context", "")
    )


async def test_a_call_without_a_namespace_asks_nobody(session, gate: str) -> None:
    before = _pending_count(gate, LABEL)
    try:
        await session.call_tool(
            "resource_delete_collection",
            {"apiVersion": "v1", "kind": "ConfigMap", "labelSelector": f"{LABEL}=doomed"},
        )
    except McpError as exc:
        assert "namespace is required" in str(exc), exc
    else:
        raise AssertionError("namespace 없는 컬렉션 삭제가 거부되지 않았다")
    assert _pending_count(gate, LABEL) == before, "거부가 승인 요청을 만들었다"
    assert all(_exists(name) for name in DOOMED), "거부된 호출이 객체를 지웠다"


async def test_a_selector_that_matches_nothing_asks_nobody(session, gate: str) -> None:
    result = await session.call_tool(
        "resource_delete_collection",
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "labelSelector": f"{LABEL}={ABSENT}",
        },
    )
    assert result.isError is False, f"0건 셀렉터가 에러로 돌아왔다: {result}"
    payload = result.structuredContent
    assert payload["targets"] == 0, payload
    assert payload["accepted"] is False, payload
    assert "matched no objects" in payload["detail"], payload
    assert _pending_count(gate, ABSENT) == 0, (
        "0건 셀렉터가 승인 요청을 만들었다 — 사람이 판정할 것이 없는 화면이다"
    )


async def test_the_screen_names_the_selection_and_only_it_goes(session, gate: str) -> None:
    task = asyncio.create_task(
        session.call_tool(
            "resource_delete_collection",
            {
                "apiVersion": "v1",
                "kind": "ConfigMap",
                "namespace": NAMESPACE,
                "labelSelector": f"{LABEL}=doomed",
            },
        )
    )
    row = await wait_for_pending(gate, "rg-ac12-doomed")
    context = row["context"]
    # 수와 이름을 한 케이스에서 같이 재는 것은 의도다. 둘은 같은 승인 화면의 두 면이고,
    # 승인 댄스를 한 번 더 태워 떼어 놓으면 「이 화면을 보고 승인한 결과가 저 삭제다」는
    # 연결이 끊긴다 — 시나리오가 요구하는 것은 그 연결이다.
    assert "targets: 5" in context, f"대상 수가 화면에 없다:\n{context}"
    for name in DOOMED:
        assert f"{NAMESPACE}/{name}" in context, f"{name} 이 화면에 없다:\n{context}"
    for name in SPARED:
        assert name not in context, f"선택 밖 {name} 이 화면에 올라왔다:\n{context}"

    await decide(gate, row["id"], "APPROVED")
    result = await task
    assert result.isError is False, result
    assert result.structuredContent["accepted"] is True, result.structuredContent

    deadline = time.monotonic() + DELETION_BUDGET
    while time.monotonic() < deadline and any(_exists(name) for name in DOOMED):
        time.sleep(2)
    left = [name for name in DOOMED if _exists(name)]
    assert not left, f"승인된 선택이 {DELETION_BUDGET}초 안에 사라지지 않았다: {left}"
    gone = [name for name in SPARED if not _exists(name)]
    assert not gone, f"같은 네임스페이스의 다른 레이블까지 사라졌다: {gone}"


async def test_a_selection_that_grew_after_approval_is_refused(session, gate: str) -> None:
    task = asyncio.create_task(
        session.call_tool(
            "resource_delete_collection",
            {
                "apiVersion": "v1",
                "kind": "ConfigMap",
                "namespace": NAMESPACE,
                "labelSelector": f"{LABEL}=grown",
            },
        )
    )
    row = await wait_for_pending(gate, "rg-ac12-grown")
    assert "targets: 2" in row["context"], row["context"]
    # 늦게 오는 객체는 **승인 요청이 관측된 뒤에** 만든다. 그 관측이 곧 서버가 목록
    # 스냅샷을 이미 떴다는 증거이고, 그보다 먼저 만들면 스냅샷이 3개를 담아 이 케이스가
    # 재려는 창 자체가 없어진다.
    _config_map(LATECOMER, "grown")
    await decide(gate, row["id"], "APPROVED")

    try:
        await task
    except McpError as exc:
        assert "the selection changed after it was approved" in str(exc), exc
        assert "2 target(s) → 3" in str(exc), exc
    else:
        raise AssertionError("승인 뒤 늘어난 선택이 그대로 실행됐다")
    still = [name for name in [*GROWN, LATECOMER] if _exists(name)]
    assert still == [*GROWN, LATECOMER], f"거부된 호출이 객체를 지웠다: {still}"


async def run() -> None:
    url = base_url()
    _group("doomed", DOOMED)
    _group("spared", SPARED)
    _group("grown", GROWN)
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- resource-generic/시나리오 12 (namespace 누락) ---")
            await test_a_call_without_a_namespace_asks_nobody(session, gate)
            print("--- resource-generic/시나리오 12 (0건 셀렉터) ---")
            await test_a_selector_that_matches_nothing_asks_nobody(session, gate)
            print("--- resource-generic/시나리오 12 (대상 수·이름과 선택적 삭제) ---")
            await test_the_screen_names_the_selection_and_only_it_goes(session, gate)
            print("--- resource-generic/시나리오 12 (승인 뒤 늘어난 선택) ---")
            await test_a_selection_that_grew_after_approval_is_refused(session, gate)
    print("ok: test-resource-generic.md#시나리오 12")


if __name__ == "__main__":
    asyncio.run(run())
