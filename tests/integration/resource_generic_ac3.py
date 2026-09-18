"""절단과 이어보기 — 기본 100 · `continue` 로 나머지 · 상한 초과는 클램프가 아니라 거부.

검증 시나리오: test-resource-generic.md#시나리오 3
실행 대상: primary
병렬 레인: resource-generic

모집단은 `tests/k8s/kind/resource-generic-fixture.yaml` 의 페이징 ConfigMap 120개다 —
왜 120개이고 왜 전용 레이블로 좁히는지는 그 파일의 머리말이 진다.

⚠️ 그 레이블로 좁히는 대가가 이 파일에 떨어진다: apiserver 는 술어가 비어 있을 때만
`remainingItemCount` 를 채우므로(비어 있지 않은 셀렉터에서는 부정확한 값을 주느니 생략한다)
**남은 건수를 응답의 `remaining` 으로 잴 수 없다.** 그래서 2회차가 실제로 돌려준 행 수로 잰다.

「클램프되지 않고 거부」는 거부 하나만 봐서는 성립하지 않는다 — 인자를 아예 못 받는 구현도
그 단독 관측을 통과한다. 그래서 상한값 `limit=500` 이 **정상 응답**임을 나란히 보여, 거부가
상한을 넘긴 것에 대한 것이지 인자 자체에 대한 것이 아님을 성립시킨다. 상한 초과는 도구가
k8s 에 닿기 전에 인자 검증으로 거부하므로 도구 결과가 아니라 `McpError`(-32602) 로 온다.

선행 조건(120개가 다 섰는가)은 도구가 아니라 kubectl 로 세운다 — 전제를 검증 대상 자신으로
세우면 「픽스처가 아직 안 섰다」와 「페이징이 틀렸다」가 구분되지 않는다.
"""

from __future__ import annotations

import asyncio
import subprocess
import time

from mcp import ClientSession
from mcp.shared.exceptions import McpError

from _helpers import base_url, open_session, wait_for_healthz
from _workload import NAMESPACE

#: 픽스처가 페이징 ConfigMap 에만 다는 레이블. 모집단을 정확히 120 으로 고정한다.
PAGING_SELECTOR = "homelab-k3s-mcp.test/paging=true"

#: 같은 픽스처가 세우는 개수. 기본 페이지(100)보다 커야 1회차가 잘린다.
PAGE_FIXTURE_COUNT = 120

#: `internal/k8s/resource.go` 의 `ListDefaultLimit` — `limit` 미지정의 기본 페이지.
DEFAULT_LIMIT = 100

#: 같은 파일의 `ListMaxLimit` 과 그것을 넘긴 값. 넘긴 쪽은 거부돼야 한다.
MAX_LIMIT = 500
OVER_MAX_LIMIT = 1000


def _column(payload: dict, name: str) -> int:
    columns = [column["name"] for column in payload["columns"]]
    assert name in columns, f"{name} 칸이 없다: {columns}"
    return columns.index(name)


def _names(payload: dict) -> list[str]:
    index = _column(payload, "Name")
    return [row[index] for row in payload["rows"]]


def test_resource_generic_ac3_precondition_has_120_paged_configmaps(
    timeout: float = 120.0,
) -> list[str]:
    """시나리오 3 사전 조건 — 페이징 ConfigMap 120개가 실제로 서 있다.

    개수가 100 이하로 떨어지면 1회차가 잘리지 않아 이 파일의 나머지 단언이 전부 공전한다.
    그래서 「120 이다」와 「120 > 기본 페이지」를 둘 다 건다 — 뒤엣것은 픽스처가 줄어들면
    조용히 무의미해지는 것을 막는 자기 방어다.
    """
    deadline = time.monotonic() + timeout
    last = "<no probe yet>"
    while time.monotonic() < deadline:
        probe = subprocess.run(
            [
                "kubectl", "-n", NAMESPACE, "get", "configmap",
                "-l", PAGING_SELECTOR,
                "-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}",
            ],
            capture_output=True,
            text=True,
        )
        names = sorted(name for name in probe.stdout.split() if name)
        if probe.returncode == 0 and len(names) == PAGE_FIXTURE_COUNT:
            assert PAGE_FIXTURE_COUNT > DEFAULT_LIMIT, (
                f"픽스처 {PAGE_FIXTURE_COUNT}개가 기본 페이지 {DEFAULT_LIMIT} 를 넘지 않는다 "
                f"— 절단이 관측되지 않아 이 시나리오가 공전한다"
            )
            print(f"precondition ok: {PAGE_FIXTURE_COUNT}개 ({names[0]} … {names[-1]})")
            return names
        last = f"{len(names)}개" if probe.returncode == 0 else probe.stderr.strip()
        time.sleep(2)
    raise RuntimeError(
        f"{NAMESPACE} 의 {PAGING_SELECTOR} ConfigMap 이 {timeout:.0f}s 안에 "
        f"{PAGE_FIXTURE_COUNT}개가 되지 않았다 (마지막 관측: {last!r})"
    )


async def test_resource_generic_ac3_first_page_truncates(
    session: ClientSession,
) -> tuple[list[str], str]:
    """시나리오 3 — `limit` 미지정 1회차가 100건 + 절단 표시 + `continue` 토큰."""
    result = await session.call_tool(
        "resource_list",
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "labelSelector": PAGING_SELECTOR,
        },
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload is not None, result

    assert payload["limit"] == DEFAULT_LIMIT, (
        f"기본 페이지가 {DEFAULT_LIMIT} 이 아니다: {payload['limit']}"
    )
    names = _names(payload)
    assert len(names) == DEFAULT_LIMIT, (
        f"1회차가 {DEFAULT_LIMIT}건이 아니다: {len(names)}건"
    )
    assert payload["truncated"] is True, f"절단 표시가 없다: {payload['truncated']}"
    token = payload["continue"]
    assert token, f"`continue` 토큰이 비었다: {payload}"

    # 절단은 구조화 응답뿐 아니라 사람이 읽는 본문에도 보여야 한다 — 도구를 쓰는 쪽이
    # 구조화 필드를 보지 않고도 「덜 받았다」를 알 수 있어야 한다는 것이 이 시나리오의 요지다.
    text = result.content[0].text
    assert f"(truncated at limit={DEFAULT_LIMIT}" in text, text[-300:]
    assert token in text, text[-300:]

    print(f"first page ok: {len(names)}건, 절단 표시 + continue {token[:24]}…")
    return names, token


async def test_resource_generic_ac3_continue_returns_the_rest(
    session: ClientSession, first_page: list[str], token: str, expected: list[str]
) -> None:
    """시나리오 3 — `continue` 재조회가 나머지 20건을 돌려주고 두 페이지가 전집을 덮는다.

    개수만 보면 같은 100건을 두 번 받아도 통과할 수 있는 자리가 아니다(20 ≠ 100). 그래도
    이름까지 보는 것은 **두 페이지가 겹치지 않고 합집합이 픽스처 전집과 같다**는 것이
    「이어보기」의 실제 요구이기 때문이다 — 토큰을 무시하고 두 번째 페이지를 새로 뜨는
    구현은 개수는 맞출 수 있어도 이 단언은 통과하지 못한다.
    """
    result = await session.call_tool(
        "resource_list",
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "labelSelector": PAGING_SELECTOR,
            "continue": token,
        },
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload is not None, result

    rest = _names(payload)
    remaining = PAGE_FIXTURE_COUNT - DEFAULT_LIMIT
    assert len(rest) == remaining, f"2회차가 {remaining}건이 아니다: {len(rest)}건"
    assert payload["truncated"] is False, f"2회차가 또 잘렸다: {payload}"
    assert not payload["continue"], f"2회차에 `continue` 가 남아 있다: {payload['continue']}"

    overlap = sorted(set(first_page) & set(rest))
    assert not overlap, f"두 페이지가 겹친다: {overlap}"
    assert sorted(first_page + rest) == expected, (
        "두 페이지의 합집합이 픽스처 전집과 다르다: "
        f"{sorted(set(expected) ^ set(first_page + rest))}"
    )

    print(f"continue ok: {len(rest)}건, 합집합 {len(first_page) + len(rest)} = 전집")


async def test_resource_generic_ac3_over_max_limit_is_rejected_not_clamped(
    session: ClientSession,
) -> None:
    """시나리오 3 — `limit=1000` 은 클램프되지 않고 거부되며, 상한값 자체는 정상 응답이다."""
    accepted = await session.call_tool(
        "resource_list",
        {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "namespace": NAMESPACE,
            "labelSelector": PAGING_SELECTOR,
            "limit": MAX_LIMIT,
        },
    )
    assert accepted.isError is False, accepted
    payload = accepted.structuredContent
    assert payload["limit"] == MAX_LIMIT, payload
    assert len(_names(payload)) == PAGE_FIXTURE_COUNT, (
        f"상한값 호출이 전집을 돌려주지 않았다: {len(_names(payload))}건"
    )
    assert payload["truncated"] is False, payload

    try:
        await session.call_tool(
            "resource_list",
            {
                "apiVersion": "v1",
                "kind": "ConfigMap",
                "namespace": NAMESPACE,
                "labelSelector": PAGING_SELECTOR,
                "limit": OVER_MAX_LIMIT,
            },
        )
    except McpError as exc:
        message = str(exc)
        assert f"limit must be <= {MAX_LIMIT}" in message, message
        print(f"over-max rejection ok: {message}")
    else:
        raise AssertionError(
            f"limit={OVER_MAX_LIMIT} 가 거부되지 않았다 — 클램프됐을 수 있다"
        )


async def run() -> None:
    url = base_url()

    print("--- resource-generic/시나리오 3 (사전 조건: 페이징 픽스처 120개) ---")
    expected = test_resource_generic_ac3_precondition_has_120_paged_configmaps()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- resource-generic/시나리오 3 (1회차 절단) ---")
        first_page, token = await test_resource_generic_ac3_first_page_truncates(session)
        print("--- resource-generic/시나리오 3 (이어보기) ---")
        await test_resource_generic_ac3_continue_returns_the_rest(
            session, first_page, token, expected
        )
        print("--- resource-generic/시나리오 3 (상한 초과는 거부) ---")
        await test_resource_generic_ac3_over_max_limit_is_rejected_not_clamped(session)
        print("ok: test-resource-generic.md#시나리오 3")


if __name__ == "__main__":
    asyncio.run(run())
