"""자격증명 값이 응답 밖으로 새지 않는다 — 네 표면 전부에서 토큰이 보이지 않는다.

검증 시나리오: test-approval-gate.md#시나리오 10
실행 대상: primary
병렬 레인: gate-objects

시나리오가 세는 표면은 넷이다 — **승인 요청 `context`** · **SUT 서버 로그** ·
**에러 메시지** · **`resource_list` 응답**. 그리고 토큰이 등장해도 되는 자리는 하나다:
승인을 받은 정상 `resource_get` 응답. 이 파일은 그 다섯 자리를 한 번에 잰다.

표면을 하나라도 빼면 이 시나리오는 증명되지 않는다 — 값이 로그로만 새는 구현과
context 로만 새는 구현은 서로 다른 결함이고, 어느 하나만 막아도 나머지는 그대로
통과한다. 그래서 표면별로 파일을 쪼개지 않고 여기서 함께 단언한다.

쓰기 경로(`resource_create`·`resource_patch`)의 값은 **어디에도** 나오지 않아야 한다.
게이트가 민감 종류 쓰기의 인자를 `(masked, NB)` 로 바꿔 넣기 때문이다
(``internal/mcp/gate.go::maskCredentialValues``) — 그래서 「없다」만 재지 않고
**「키 이름과 바이트 수는 있다」**까지 함께 잰다. 전부 가려 버리는 구현은 값을 숨기지만
운영자에게서 판단 근거까지 뺏으므로 그것도 이 AC 의 통과가 아니다.

실패 경로는 승인 **뒤** 실행이 깨지는 자리다: 판정을 기다리는 동안 대상을 지우면
게이트의 실행 직전 재확인이 실패하고, 그 에러 문면이 네 번째 표면이 된다.
"""

from __future__ import annotations

import asyncio
import base64
import json
import subprocess

from mcp.shared.exceptions import McpError

from _gatekeeper import decide, gatekeeper_url, wait_for_pending
from _helpers import base_url, open_session, wait_for_healthz

NAMESPACE = "workload-test"
SERVER_NAMESPACE = "homelab-k3s-mcp"
SERVER_DEPLOYMENT = "homelab-k3s-mcp"

READ_SECRET = "gk-ac10-read"
WRITE_SECRET = "gk-ac10-write"
DOOMED_SECRET = "gk-ac10-doomed"

#: 네 표면 전부에서 찾을 바늘. 레포 어디에도 없는 문자열이라 grep 이 곧 판정이다.
TOKEN = "gk-ac10-needle-4c81f2be9d07"
ENCODED_TOKEN = base64.b64encode(TOKEN.encode()).decode()

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


def _secret(name: str) -> dict:
    return {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {"name": name, "namespace": NAMESPACE},
        "stringData": {"token": TOKEN},
    }


def _delete_secret(name: str) -> None:
    subprocess.run(
        ["kubectl", "-n", NAMESPACE, "delete", "secret", name, "--ignore-not-found"],
        check=False,
        capture_output=True,
    )


def _server_logs() -> str:
    return subprocess.check_output(
        ["kubectl", "-n", SERVER_NAMESPACE, "logs", f"deploy/{SERVER_DEPLOYMENT}",
         "--tail=1000"],
        text=True,
    )


def _assert_clean(surface: str, text: str) -> None:
    assert TOKEN not in text, f"{surface} 에 토큰 평문이 있다: {text[:400]}"
    assert ENCODED_TOKEN not in text, f"{surface} 에 인코딩된 토큰이 있다: {text[:400]}"


async def _dance(session, gate, tool: str, args: dict, marker: str):
    """도구를 띄우고 PENDING context 를 수집한 뒤 승인한다. (context, 결과) 를 준다."""
    task = asyncio.create_task(session.call_tool(tool, args))
    row = await wait_for_pending(gate, marker)
    await decide(gate, row["id"], "APPROVED")
    return row.get("context", ""), await task


async def test_read_context_is_clean_but_the_response_carries_the_value(session, gate) -> None:
    context, result = await _dance(
        session, gate, "resource_get",
        {"apiVersion": "v1", "kind": "Secret", "namespace": NAMESPACE, "name": READ_SECRET},
        READ_SECRET,
    )
    _assert_clean("읽기 승인 요청 context", context)
    assert result.isError is False, result
    body = result.content[0].text
    assert ENCODED_TOKEN in body, (
        "승인된 Secret 읽기가 값을 돌려주지 않았다 — 이 시나리오의 대조군이 사라지면 "
        "나머지 단언은 「아무것도 흐르지 않는 서버」로도 통과한다"
    )


async def test_write_contexts_carry_only_key_names_and_sizes(session, gate) -> None:
    create_context, created = await _dance(
        session, gate, "resource_create", {"manifest": _secret(WRITE_SECRET)}, WRITE_SECRET
    )
    _assert_clean("생성 승인 요청 context", create_context)
    assert "token" in create_context, f"context 에 키 이름이 없다: {create_context}"
    assert MASK_MARK in create_context, (
        f"context 에 바이트 수 표기가 없다(전부 가리는 구현도 이 AC 를 통과해선 안 된다): "
        f"{create_context}"
    )
    assert created.isError is False, created

    patch_context, patched = await _dance(
        session, gate, "resource_patch",
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "namespace": NAMESPACE,
            "name": WRITE_SECRET,
            "patchType": "merge",
            "patch": {"stringData": {"token": TOKEN}},
        },
        WRITE_SECRET,
    )
    _assert_clean("수정 승인 요청 context", patch_context)
    assert MASK_MARK in patch_context, patch_context
    assert patched.isError is False, patched


async def test_the_failure_message_does_not_carry_the_value(session, gate) -> None:
    task = asyncio.create_task(
        session.call_tool(
            "resource_patch",
            {
                "apiVersion": "v1",
                "kind": "Secret",
                "namespace": NAMESPACE,
                "name": DOOMED_SECRET,
                "patchType": "merge",
                "patch": {"stringData": {"token": TOKEN}},
            },
        )
    )
    row = await wait_for_pending(gate, DOOMED_SECRET)
    _assert_clean("실패 경로의 승인 요청 context", row.get("context", ""))
    _delete_secret(DOOMED_SECRET)
    await decide(gate, row["id"], "APPROVED")
    try:
        await task
    except McpError as exc:
        _assert_clean("실행 실패 에러 메시지", str(exc))
    else:
        raise AssertionError("지워진 Secret 에 대한 승인 실행이 성공으로 보고됐다")


async def test_list_does_not_carry_values(session, gate) -> None:
    result = await session.call_tool(
        "resource_list", {"apiVersion": "v1", "kind": "Secret", "namespace": NAMESPACE}
    )
    assert result.isError is False, result
    body = result.content[0].text
    assert READ_SECRET in body, f"목록에 대상 Secret 이 없다: {body[:400]}"
    _assert_clean("resource_list 응답", body)


async def test_server_logs_do_not_carry_the_value() -> None:
    _assert_clean("SUT 서버 로그", _server_logs())


async def run() -> None:
    url = base_url()
    _apply(_secret(READ_SECRET))
    _apply(_secret(DOOMED_SECRET))
    _delete_secret(WRITE_SECRET)
    wait_for_healthz(url)

    with gatekeeper_url() as gate:
        async with open_session(url) as session:
            print("--- approval-gate/시나리오 10 (읽기: context 는 비고 응답에는 있다) ---")
            await test_read_context_is_clean_but_the_response_carries_the_value(session, gate)
            print("--- approval-gate/시나리오 10 (쓰기: 키 이름과 바이트 수만) ---")
            await test_write_contexts_carry_only_key_names_and_sizes(session, gate)
            print("--- approval-gate/시나리오 10 (실패 경로의 에러 문면) ---")
            await test_the_failure_message_does_not_carry_the_value(session, gate)
            print("--- approval-gate/시나리오 10 (목록) ---")
            await test_list_does_not_carry_values(session, gate)
            print("--- approval-gate/시나리오 10 (서버 로그) ---")
            await test_server_logs_do_not_carry_the_value()
    print("ok: test-approval-gate.md#시나리오 10")


if __name__ == "__main__":
    asyncio.run(run())
