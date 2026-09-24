"""resource_* 읽기 축이 **실 apiserver 를 상대로** 답하는지 확인 (규칙 3 비-시나리오 파일).

검증 시나리오: 없음 (스모크/인프라)
실행 대상: primary
실행 순서: 1

`smoke.py` 가 도구가 **광고되는지**를 보는 자리라면, 이 파일은 읽기 축 두 도구가 실제로
**클러스터에서 값을 가져오는지**를 본다. 둘을 나눈 이유는 `resource_list` 가 광고된 채로
빈 표를 돌려주는 상태가 실재했기 때문이다(`rct_20260913-0002` 1차 시도): Table 표현을
요구하는 `Accept` 파라미터가 apiserver 가 아는 것과 어긋나면 요청이 실패하는 대신 **평범한
List 로 답이 오고**, 그 본문은 `metav1.Table` 로 전부 0인 값으로 디코드된다. Go 단위 테스트는
자기가 만든 Table 을 먹이므로 이 어긋남을 볼 수 없다 — 실 apiserver 만 볼 수 있고, 이
레포에서 실 apiserver 가 도는 자리는 여기뿐이다.

그래서 단언은 **행이 있다**에 걸려 있다. 열 정의만 보면 렌더러가 서 있는지는 알 수 있어도
협상이 성립했는지는 알 수 없고, 「비어 있다」는 목록 도구가 결코 지어내서는 안 되는 답이다.
kind 는 픽스처가 아니라 kind 클러스터가 **반드시** 갖는 것(네임스페이스·파드)으로 골라,
픽스처 배치가 바뀌어도 이 파일이 흔들리지 않게 했다.

AC 단위의 검증이 아니다 — 이 파일은 그 공백을 메우려는 것이 아니라, 도구가 **살아는
있는지**를 배포 계층에서 한 번 재는 스모크다.
"""

from __future__ import annotations

import asyncio

from mcp import ClientSession

from _helpers import base_url, open_session, wait_for_healthz


def _table(result) -> tuple[list[str], list[list]]:
    """도구 결과에서 (열 이름, 행) 을 꺼낸다."""
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload is not None, result
    return [column["name"] for column in payload["columns"]], payload["rows"]


async def test_resource_list_returns_the_apiservers_table(
    session: ClientSession,
) -> None:
    """`resource_list` 가 협상된 Table 로 답한다 — 열도 행도 비어 있지 않다."""
    result = await session.call_tool("resource_list", {"apiVersion": "v1", "kind": "Namespace"})
    columns, rows = _table(result)

    assert columns, f"빈 열 정의 — Table 협상이 성립하지 않았다: {result.structuredContent}"
    assert "Name" in columns, f"apiserver 의 열이 아니다: {columns}"
    # 이 배포가 도는 네임스페이스는 반드시 목록에 있다. 「행이 하나라도 있다」보다 강한
    # 단언을 고른 이유는, 다른 좌표를 읽고 있어도 행 수만으로는 구별되지 않기 때문이다.
    names = {row[columns.index("Name")] for row in rows}
    assert "homelab-k3s-mcp" in names, f"자기 네임스페이스가 목록에 없다: {sorted(names)}"


async def test_resource_get_reads_a_pod_that_list_found(
    session: ClientSession,
) -> None:
    """목록 → 이름 → 로그. 옛 `workload_logs` 가 갈음되려면 이 경로가 이어져야 한다.

    `resource_get` 은 이름을 필수로 받으므로(그 도구는 list verb 를 행사하지 않는다) 이름을 얻는 유일한
    경로가 `resource_list` 다. 두 도구를 따로 재면 목록이 빈 표를 돌려주는 동안에도 각자는 「통과」로 보인다.
    """
    listed = await session.call_tool(
        "resource_list",
        {"apiVersion": "v1", "kind": "Pod", "namespace": "homelab-k3s-mcp"},
    )
    columns, rows = _table(listed)
    assert rows, f"파드 목록이 비었다 — 이름을 얻을 경로가 없다: {listed.structuredContent}"

    pod = rows[0][columns.index("Name")]
    logs = await session.call_tool(
        "resource_get",
        {
            "apiVersion": "v1",
            "kind": "Pod",
            "namespace": "homelab-k3s-mcp",
            "name": pod,
            "subresource": "log",
            "tailLines": 5,
        },
    )
    assert logs.isError is False, logs
    assert logs.content, logs
    block = logs.content[0]
    assert block.type == "text", block
    assert block.text.strip(), f"{pod} 의 로그가 비었다"


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- resource_list (스모크: Table 협상) ---")
        await test_resource_list_returns_the_apiservers_table(session)
        print("--- resource_get (스모크: 목록 → 이름 → 로그) ---")
        await test_resource_get_reads_a_pod_that_list_found(session)
        print("resource read smoke ok")


if __name__ == "__main__":
    asyncio.run(run())
