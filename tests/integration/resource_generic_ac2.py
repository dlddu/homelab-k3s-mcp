"""resource_* 목록이 apiserver 의 Table 로 오고 객체 전문보다 현저히 작다.

검증 시나리오: test-resource-generic.md#시나리오 2
실행 대상: primary
병렬 레인: resource-generic

크기 비교는 **같은 직렬화 규칙**(`separators` 고정) 으로 재서 인코딩 차이가 비율에 섞이지
않게 했다. 비율 하한은 `MIN_SHRINK_FACTOR` 하나로 모아 두었다 — `managedFields` 때문에
실제 비율은 한 자릿수 후반 이상이 나오지만, 픽스처가 늘거나 줄어도 「현저히」가 무너지지
않는 선을 고른다.

행의 모든 칸이 스칼라인지(= 중첩 객체가 실려 오지 않았는지)와 `managedFields` 문자열이
응답 어디에도 없는지를 함께 본다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from mcp import ClientSession

from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE, WORKLOAD, CRASHLOOP_WORKLOAD

#: 도구 응답이 객체 전문 대비 최소한 이만큼은 작아야 「현저히 작다」로 센다.
MIN_SHRINK_FACTOR = 4.0

#: 두 경로의 크기를 같은 규칙으로 재기 위한 직렬화 옵션.
_DUMP = {"separators": (",", ":"), "sort_keys": True, "ensure_ascii": False}


def _size(value) -> int:
    return len(json.dumps(value, **_DUMP))


def _kubectl_deployments_whole() -> dict:
    """같은 목록의 **객체 전문** — 도구를 거치지 않은 대조군."""
    raw = subprocess.check_output(
        ["kubectl", "get", "deployments", "-n", NAMESPACE, "-o", "json"],
        text=True,
    )
    return json.loads(raw)


async def test_resource_generic_ac2_list_comes_back_as_a_table(
    session: ClientSession,
) -> None:
    """시나리오 2 — 도구 응답이 Table(컬럼 헤더 + 행)이고 kubectl 준하는 컬럼을 보존한다."""
    result = await session.call_tool(
        "resource_list",
        {"apiVersion": "apps/v1", "kind": "Deployment", "namespace": NAMESPACE},
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload is not None, result

    columns = [column["name"] for column in payload["columns"]]
    rows = payload["rows"]
    assert columns, f"빈 열 정의 — Table 협상이 성립하지 않았다: {payload}"
    assert rows, f"Deployment 목록이 비었다: {payload}"

    # 이 둘은 apiserver 의 Deployment Table 정의가 항상 내는 것이라 픽스처가 바뀌어도 흔들리지 않는다.
    for wanted in ("Name", "Ready"):
        assert wanted in columns, f"kubectl 준하는 열이 아니다: {columns}"
    assert len(columns) >= 4, f"열이 너무 적다 — 표가 축약됐다: {columns}"

    names = {row[columns.index("Name")] for row in rows}
    assert {WORKLOAD, CRASHLOOP_WORKLOAD} <= names, (
        f"픽스처 Deployment 가 목록에 없다: {sorted(names)}"
    )

    assert result.content, result
    block = result.content[0]
    assert block.type == "text", block
    assert block.text.startswith("NAME"), f"표 헤더로 시작하지 않는다: {block.text[:80]!r}"

    print(f"table ok: columns={columns} rows={len(rows)}")


async def test_resource_generic_ac2_table_is_much_smaller_than_whole_objects(
    session: ClientSession,
) -> None:
    """시나리오 2 — 같은 목록의 객체 전문과 견줘 도구 응답이 현저히 작다."""
    result = await session.call_tool(
        "resource_list",
        {"apiVersion": "apps/v1", "kind": "Deployment", "namespace": NAMESPACE},
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload is not None, result

    whole = _kubectl_deployments_whole()
    # 두 경로가 같은 대상을 보고 있다는 것부터 고정한다 — 다른 좌표를 견주면 크기 비교가
    # 아무것도 말하지 않는다.
    columns = [column["name"] for column in payload["columns"]]
    tool_names = {row[columns.index("Name")] for row in payload["rows"]}
    whole_names = {item["metadata"]["name"] for item in whole["items"]}
    assert tool_names == whole_names, (
        f"두 경로가 다른 목록을 보고 있다: 도구 {sorted(tool_names)} vs "
        f"객체 전문 {sorted(whole_names)}"
    )

    tool_size = _size(payload)
    whole_size = _size(whole["items"])
    factor = whole_size / tool_size
    assert factor >= MIN_SHRINK_FACTOR, (
        f"도구 응답 {tool_size}B 가 객체 전문 {whole_size}B 대비 {factor:.1f}배로만 작다 "
        f"— {MIN_SHRINK_FACTOR}배 이상이어야 「현저히 작다」로 센다"
    )

    print(f"size ok: table {tool_size}B vs whole objects {whole_size}B ({factor:.1f}x)")


async def test_resource_generic_ac2_rows_carry_no_objects(
    session: ClientSession,
) -> None:
    """시나리오 2 — 작아진 이유가 표이기 때문임을 형태로 확인한다."""
    result = await session.call_tool(
        "resource_list",
        {"apiVersion": "apps/v1", "kind": "Deployment", "namespace": NAMESPACE},
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload is not None, result

    for row in payload["rows"]:
        for cell in row:
            assert not isinstance(cell, (dict, list)), (
                f"행에 중첩 객체가 실려 왔다 — 표가 아니라 객체다: {cell!r}"
            )

    serialized = json.dumps(payload, **_DUMP)
    for leaked in ("managedFields", "last-applied-configuration"):
        assert leaked not in serialized, f"표 응답에 {leaked} 가 실려 있다"

    print("shape ok: rows are scalars, no object noise")


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- resource-generic/시나리오 2 (표 형식) ---")
        await test_resource_generic_ac2_list_comes_back_as_a_table(session)
        print("--- resource-generic/시나리오 2 (객체 전문 대비 크기) ---")
        await test_resource_generic_ac2_table_is_much_smaller_than_whole_objects(session)
        print("--- resource-generic/시나리오 2 (행은 스칼라다) ---")
        await test_resource_generic_ac2_rows_carry_no_objects(session)
        print("ok: test-resource-generic.md#시나리오 2")


if __name__ == "__main__":
    asyncio.run(run())
