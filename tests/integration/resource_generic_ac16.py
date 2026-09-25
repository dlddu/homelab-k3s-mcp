"""민감 종류는 읽기도 쓰기도 승인을 거친다 — 여섯 verb 전부, list 는 예외, 값은 가려진다.

검증 시나리오: test-resource-generic.md#시나리오 16
실행 대상: primary
병렬 레인: gate-tunnels

여섯 verb(`get`·`watch`·`create`·`update`·`patch`·`delete`)를 **한 파일에서 전부** 태운다.
일부만 떼어 닫으면 「민감 종류는 모든 동사에서 막힌다」가 아니라 「그 동사에서 막힌다」만
증명되고, 빠진 동사가 바로 새는 자리가 된다.

**「k8s 호출 카운트 0」 절에 대하여.** 시나리오는 미승인 거부에서 k8s 호출이 0 이기를
요구한다. 호출 수 자체는 SUT 안의 사실이라 e2e 가 셀 수 없고(그 자리는 Go 단위의
몫이다 — ``internal/mcp/resource_test.go`` 의 게이트 분기 테스트들), apiserver 감사
프록시는 이 하네스에 없다. 그래서 이 파일은 **밖에서 관측 가능한 등가물**을 단언한다:
거부된 create 는 객체를 만들지 않았고, 거부된 update·patch 는 `resourceVersion` 을
움직이지 않았으며, 거부된 delete 뒤에도 객체가 그대로 있다. 「호출은 갔지만 아무 일도
없었다」와 「호출이 가지 않았다」를 이 층에서 가를 수 없다는 사실을 숨기지 않으려고
여기 적는다 — 이 파일이 증명하는 것은 **거부가 상태에 닿지 않았다**까지다.

미승인은 사람 없이 재야 하므로 **승인 요청을 띄운 뒤 판정을 직접 REJECTED 로 내려**
만든다(`resource_generic_ac5.py` 의 게이트 대조군과 같은 방식). 자동 거부 모드
(`AUTO_REJECT`)를 쓰지 않는 이유는 `_refuse` 의 docstring 에 있다 — 그 모드는 primary
배포의 요청에는 걸리지 않는다.
"""

from __future__ import annotations

import asyncio
import base64
import json
import subprocess

from mcp.shared.exceptions import McpError

from _gatekeeper import (
    decide,
    gatekeeper_url,
    list_requests,
    wait_for_pending,
)
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"

GATED_SECRET = "rg-ac16-secret"
CREATED_SECRET = "rg-ac16-created"
CONTROL_MAP = "rg-ac16-configmap"
# name 이 없는 list 범위 watch 에는 대상 이름이 context 에 없다 — 이 셀렉터가 그 요청의 마커다.
WATCH_SELECTOR = "rg-ac16=list-scope"

TOKEN = "rg-ac16-token-5b93ce07af21"
MASK_MARK = f"(masked, {len(TOKEN)}B)"


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


def _delete(kind: str, name: str) -> None:
    subprocess.run(
        ["kubectl", "-n", NAMESPACE, "delete", kind, name, "--ignore-not-found"],
        check=False,
        capture_output=True,
    )


def _exists(kind: str, name: str) -> bool:
    return (
        subprocess.run(
            ["kubectl", "-n", NAMESPACE, "get", kind, name], capture_output=True
        ).returncode
        == 0
    )


def _resource_version(kind: str, name: str) -> str:
    return _kubectl(
        "-n", NAMESPACE, "get", kind, name, "-o", "jsonpath={.metadata.resourceVersion}"
    ).strip()


def _seed() -> None:
    _apply(
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {"name": GATED_SECRET, "namespace": NAMESPACE},
            "stringData": {"token": TOKEN},
        }
    )
    _apply(
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {"name": CONTROL_MAP, "namespace": NAMESPACE},
            "data": {"key": "control"},
        }
    )
    _delete("secret", CREATED_SECRET)


def _gated_calls() -> list[tuple[str, dict, str]]:
    """여섯 verb 각각 한 호출. 좌표는 전부 같은 민감 종류다."""
    coordinate = {"apiVersion": "v1", "kind": "Secret", "namespace": NAMESPACE}
    return [
        ("resource_get", {**coordinate, "name": GATED_SECRET}, GATED_SECRET),
        ("resource_watch", {**coordinate, "watchSeconds": 2, "labelSelector": WATCH_SELECTOR}, WATCH_SELECTOR),
        (
            "resource_create",
            {
                "manifest": {
                    "apiVersion": "v1",
                    "kind": "Secret",
                    "metadata": {"name": CREATED_SECRET, "namespace": NAMESPACE},
                    "stringData": {"token": TOKEN},
                }
            },
            CREATED_SECRET,
        ),
        (
            "resource_update",
            {
                **coordinate,
                "name": GATED_SECRET,
                "manifest": {
                    "apiVersion": "v1",
                    "kind": "Secret",
                    "metadata": {"name": GATED_SECRET, "namespace": NAMESPACE},
                    "stringData": {"token": "replaced"},
                },
            },
            GATED_SECRET,
        ),
        (
            "resource_patch",
            {
                **coordinate,
                "name": GATED_SECRET,
                "patchType": "merge",
                "patch": {"stringData": {"token": "patched"}},
            },
            GATED_SECRET,
        ),
        ("resource_delete", {**coordinate, "name": GATED_SECRET}, GATED_SECRET),
    ]


async def _refuse(session, gate, tool: str, args: dict, marker: str) -> None:
    """도구 호출을 띄우고, 그것이 만든 승인 요청을 사람 대신 **거절**한다.

    자동 응답 모드(`AUTO_REJECT`)를 쓰지 않는 이유: 그 모드는 gatekeeper 가 **요청의
    담당 사용자**에게 적용하는 설정이라 `GATEKEEPER_USER_ID` 를 물고 있는
    `gatekeeper-variant` 배포에서만 발화한다(그래서 시나리오 9 의
    `approval_gate_ac9.py` 가 `실행 대상: gatekeeper-variant` 다). primary 배포의
    요청에는 담당자가 없어 모드가 걸리지 않고, 요청은 승인 시한(5분)까지 PENDING 으로
    남았다가 타임아웃으로 끝난다 — 거부가 아니라 **무응답**이라 이 시나리오가 재려는
    것을 재지 못한다. 그래서 `resource_generic_ac5.py` 의 게이트 대조군과 같은 방식으로
    판정을 직접 내린다.
    """
    task = asyncio.create_task(session.call_tool(tool, args))
    row = await wait_for_pending(gate, marker)
    await decide(gate, row["id"], "REJECTED")
    try:
        await task
    except McpError as exc:
        assert "reject" in str(exc).lower(), f"{tool}: {exc}"
    else:
        raise AssertionError(f"{tool} 이 거절된 승인 요청 위에서 실행됐다")


async def test_every_gated_verb_is_refused_without_approval(session, gate) -> None:
    before = _resource_version("secret", GATED_SECRET)
    for tool, args, marker in _gated_calls():
        await _refuse(session, gate, tool, args, marker)

    assert not _exists("secret", CREATED_SECRET), "거부된 create 가 객체를 남겼다"
    assert _exists("secret", GATED_SECRET), "거부된 delete 가 객체를 지웠다"
    assert _resource_version("secret", GATED_SECRET) == before, (
        "거부된 update·patch 가 대상을 움직였다"
    )


async def test_an_approved_create_masks_its_values_and_still_writes_them(session, gate) -> None:
    task = asyncio.create_task(
        session.call_tool(
            "resource_create",
            {
                "manifest": {
                    "apiVersion": "v1",
                    "kind": "Secret",
                    "metadata": {"name": CREATED_SECRET, "namespace": NAMESPACE},
                    "stringData": {"token": TOKEN},
                }
            },
        )
    )
    row = await wait_for_pending(gate, CREATED_SECRET)
    context = row.get("context", "")
    assert TOKEN not in context, f"승인 화면에 토큰 평문이 있다: {context}"
    assert "token" in context, f"승인 화면에 키 이름이 없다: {context}"
    assert MASK_MARK in context, (
        f"승인 화면에 바이트 수가 없다 — 전부 가리면 운영자가 판단할 수 없다: {context}"
    )
    await decide(gate, row["id"], "APPROVED")
    result = await task

    assert result.isError is False, result
    stored = _kubectl(
        "-n", NAMESPACE, "get", "secret", CREATED_SECRET,
        "-o", "jsonpath={.data.token}",
    ).strip()
    assert stored, "승인된 생성이 값을 쓰지 않았다"
    assert base64.b64decode(stored).decode() == TOKEN, (
        "가려진 것은 승인 화면뿐이어야 하는데 실제 저장된 값까지 달라졌다"
    )


async def test_list_and_an_ordinary_kind_do_not_ask_for_approval(session, gate) -> None:
    before = {row["id"] for row in list_requests(gate)}

    listed = await session.call_tool(
        "resource_list", {"apiVersion": "v1", "kind": "Secret", "namespace": NAMESPACE}
    )
    assert listed.isError is False, listed
    assert GATED_SECRET in listed.content[0].text, listed.content[0].text

    fetched = await session.call_tool(
        "resource_get",
        {"apiVersion": "v1", "kind": "ConfigMap", "namespace": NAMESPACE, "name": CONTROL_MAP},
    )
    assert fetched.isError is False, fetched

    new_rows = [row for row in list_requests(gate) if row["id"] not in before]
    offenders = [
        row
        for row in new_rows
        if GATED_SECRET in row.get("context", "") or CONTROL_MAP in row.get("context", "")
    ]
    assert not offenders, (
        f"list 또는 평범한 종류의 get 이 승인 요청을 만들었다: {offenders}"
    )


async def run() -> None:
    url = base_url()
    _seed()
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- resource-generic/시나리오 16 (여섯 verb 미승인 거부) ---")
            await test_every_gated_verb_is_refused_without_approval(session, gate)
            print("--- resource-generic/시나리오 16 (승인된 create 의 가림과 값) ---")
            await test_an_approved_create_masks_its_values_and_still_writes_them(session, gate)
            print("--- resource-generic/시나리오 16 (list 와 대조군) ---")
            await test_list_and_an_ordinary_kind_do_not_ask_for_approval(session, gate)
    print("ok: test-resource-generic.md#시나리오 16")


if __name__ == "__main__":
    asyncio.run(run())
