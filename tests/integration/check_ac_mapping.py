#!/usr/bin/env python3
"""테스트 시나리오 ↔ e2e **파일** 1:1 정합성 체커 (매칭 단위가 아니다).

정합성 모델 `tbm_homelab-k3s-mcp-scenario-e2e`는 `docs/test-*.md`의 테스트 시나리오
(`### 시나리오 <N>:` 헤딩)와 `tests/integration/` 최상위 `*.py` **파일**을 완전 1:1(전단사)로
유지할 것을 요구한다. 이 스크립트는 그 판정을 사람의 자기신고가 아니라 **레포의 실제 상태에서
재도출**해, `docs/doc-tracker.md`의 레지스트리와 대조한다. 클러스터도 서드파티 의존성도 필요
없다(표준 라이브러리 전용) — CI의 lint 잡에서 돈다.

세 개의 사실 원천을 읽는다.

1. **시나리오 전집** — `docs/test-*.md`의 `### 시나리오 <N>:` 헤딩. 시나리오 식별자는
   `test-<domain>.md#시나리오 <N>`이고 `<domain>`은 파일명에서 온다. 서수 표기이므로
   **문서 중간에 시나리오를 끼워 넣으면 뒤 번호가 전부 밀려 식별자가 바뀐다** — 그 사고를
   잡으려고 레지스트리 행의 **제목까지** 헤딩과 대조한다(아래 규칙 6).
2. **선언** — 각 매칭 단위 파일 모듈 docstring의 `검증 시나리오:` (파싱은 `run_all.py`가 소유).
   **정확히 1개**의 시나리오를 선언한 파일만 그 시나리오의 "전용 파일"로 세고, 2개 이상을
   선언한 파일은 규칙 2 위반(분할 대기)으로 세며 그 시나리오들은 여전히 **공백**으로 계수한다 —
   겸용 파일은 전단사를 만들지 못하기 때문이다.
3. **등재** — `docs/doc-tracker.md`의 레지스트리 표·예외 목록·구현 대기 표·비-시나리오 파일
   목록·집계 블록.

판정하는 것:

* **규칙 1** 시나리오 → 전용 파일 유일 (같은 시나리오를 두 파일이 전용 선언하면 즉시 실패)
* **규칙 2** 파일 → 시나리오 유일 (겸용 파일은 위반으로 계수되고, 레지스트리와 수가 일치해야 한다)
* **규칙 3** 비-시나리오 파일은 `검증 시나리오: 없음`을 선언하고 doc-tracker에 등재돼야 한다
* **규칙 4** 예외(영구 면제)는 사유·대체 검증 수단과 함께 등재돼야 하고, 등재된 시나리오는
  파일이 없어도 drift가 아니다
* **규칙 5** 참조 무결성 — 선언·레지스트리·예외 목록·구현 대기 표가 실재하지 않는 시나리오를
  가리키지 않는다
* **규칙 6** 집계·행 일치 — 레지스트리의 행별 상태·**제목**과 집계 숫자가 실측과 정확히 같다.
  **미등재 공백은 실패다** — 모델 불변식이 (시나리오 수 − 예외 수 − 구현 대기 수) = (매칭 파일
  수)이므로, 전용 파일이 없는 시나리오는 예외나 구현 대기 중 하나로 반드시 등재돼야 한다.
* **규칙 7** 테스트 문서 상태 일치 — 각 시나리오의 `자동화` 필드가 말하는 통합 e2e 현황이 실측
  파일 집합과 같다(전용 파일이 실재하는데 `(미작성)`이 남아 있거나, 전용 파일이 없는데
  `(미작성)` 없이 파일을 참조하면 위반)
* **하네스 무결성** — 매칭 단위 파일 전부가 `run_all.py`에 정확히 한 번 배차되고,
  각 파일의 `run()`이 그 파일이 정의한 `test_*` 케이스를 **전부 호출한다**
  (만들어 놓고 CI가 실행하지 않는 파일, 그리고 배차는 되지만 자기 케이스를 부르지 않아
  **조용히 통과하는 파일**을 둘 다 구조적으로 막는다)

집계가 실측과 다르면 실패하므로, 파일을 쪼개거나 시나리오를 추가한 PR은 **같은 PR에서**
레지스트리를 갱신해야 한다. 그것이 이 모델이 요구하는 "집계 일치"다. 같은 이유로 규칙 7이
있다 — e2e 파일을 새로 만든 PR은 그 시나리오의 테스트 문서에서 `(미작성)` 표기를 같은 PR에서
지워야 한다.

> **2026-09-08 개정** — 판정 축이 **AC에서 테스트 시나리오로** 옮겨졌다(모델이
> `tbm_homelab-k3s-mcp-ac-e2e` → `tbm_homelab-k3s-mcp-scenario-e2e`로 개명). AC ↔ 시나리오 층은
> 이제 이 게이트의 판정 대상이 **아니다**(제품 문서 체계의 몫). 어느 시나리오가 어떤 AC를
> 검증하는지는 `docs/test-*.md`의 `검증 AC` 필드가 그대로 들고 있으므로, 파일 → AC 는
> 파일 → 시나리오 → AC 로 한 홉에 복원된다. 파일명은 개명하지 않았다 — 매핑의 확인 지점은
> 파일명이 아니라 모듈 docstring 선언이다.
"""

from __future__ import annotations

import ast
import pathlib
import re
import sys

import run_all

HERE = pathlib.Path(__file__).resolve().parent
REPO_ROOT = HERE.parent.parent
DOCS = REPO_ROOT / "docs"
TRACKER = DOCS / "doc-tracker.md"

#: `docs/test-<domain>.md` 의 시나리오 헤딩. 번호와 제목을 함께 딴다.
SCENARIO_HEADING_RE = re.compile(r"^### 시나리오 (\d+):[ \t]*(.*)$", re.MULTILINE)
#: 시나리오 식별자 — 레지스트리·예외·구현 대기가 공유하는 표기.
SCENARIO_ID = r"test-[a-z0-9-]+\.md#시나리오 \d+"
ROW_RE = re.compile(rf"^\| ({SCENARIO_ID}) \| ([^|]*) \| ([^|]*) \|$", re.MULTILINE)
BOLD_SCENARIO_RE = re.compile(rf"\*\*({SCENARIO_ID})\*\*")
AGGREGATE_RE = re.compile(
    r"<!-- scenario-e2e-집계 -->(.*?)<!-- /scenario-e2e-집계 -->", re.DOTALL
)
AGGREGATE_LINE_RE = re.compile(r"^- (.+): (\d+)$", re.MULTILINE)
FILE_REF_RE = re.compile(r"`([a-z_0-9]+\.py)`")

# --- 규칙 7: 테스트 문서(`docs/test-<domain>.md`)의 자동화 필드 -------------------
DOC_FIELD_RE = re.compile(r"^- \*\*(검증 AC|자동화)\*\*:")
INTEGRATION_REF_RE = re.compile(r"`tests/integration/([a-z_0-9]+\.py)")
UNWRITTEN_MARK = "(미작성)"

AGGREGATE_KEYS = (
    "시나리오 전집",
    "예외 등재",
    "구현 대기 등재",
    "1:1 대상",
    "매칭 파일(전용)",
    "분할 대기 파일(규칙 2 위반)",
    "공백 시나리오",
)


def scenario_universe() -> dict[str, str]:
    """`docs/test-*.md`에서 시나리오 전집을 재도출한다. 식별자 → 제목."""
    scenarios: dict[str, str] = {}
    for doc in sorted(DOCS.glob("test-*.md")):
        for number, title in SCENARIO_HEADING_RE.findall(
            doc.read_text(encoding="utf-8")
        ):
            scenarios[f"{doc.name}#시나리오 {number}"] = title.strip()
    return scenarios


def scenario_automation(doc: pathlib.Path) -> dict[str, str]:
    """테스트 문서의 시나리오별 ``자동화`` 필드 본문. 식별자 → 본문.

    필드는 여러 줄로 이어질 수 있으므로 다음 ``- **`` 불릿까지를 한 필드로 본다.
    """
    text = doc.read_text(encoding="utf-8")
    blocks: dict[str, str] = {}
    numbers = [number for number, _ in SCENARIO_HEADING_RE.findall(text)]
    for number, body in zip(numbers, SCENARIO_HEADING_RE.split(text)[3::3]):
        fields: dict[str, str] = {}
        current: str | None = None
        buffer: list[str] = []
        for line in body.splitlines():
            match = DOC_FIELD_RE.match(line)
            if match:
                if current:
                    fields[current] = "\n".join(buffer)
                current, buffer = match.group(1), [line]
            elif line.startswith("- **"):
                if current:
                    fields[current] = "\n".join(buffer)
                current, buffer = None, []
            elif current is not None:
                buffer.append(line)
        if current:
            fields[current] = "\n".join(buffer)
        blocks[f"{doc.name}#시나리오 {number}"] = fields.get("자동화", "")
    return blocks


def _section(text: str, heading_prefix: str) -> str:
    """`### <heading_prefix>` 로 시작하는 절의 본문(다음 `###` 전까지)."""
    lines = text.splitlines()
    out: list[str] = []
    collecting = False
    for line in lines:
        if line.startswith("### "):
            if collecting:
                break
            collecting = line.startswith(f"### {heading_prefix}")
            continue
        if collecting:
            out.append(line)
    return "\n".join(out)


class Tracker:
    """`docs/doc-tracker.md`의 e2e 렌즈 섹션에서 읽어낸 등재 내용."""

    def __init__(self, text: str) -> None:
        registry = text.split("### 시나리오 레지스트리")[-1]
        rows = ROW_RE.findall(registry)
        self.rows = {ident: status.strip() for ident, _, status in rows}
        self.titles = {ident: title.strip() for ident, title, _ in rows}

        aggregate = AGGREGATE_RE.search(text)
        self.aggregate = (
            {k.strip(): int(v) for k, v in AGGREGATE_LINE_RE.findall(aggregate.group(1))}
            if aggregate
            else {}
        )

        self.exceptions = set(
            BOLD_SCENARIO_RE.findall(_section(text, "🚫 e2e 예외"))
        )
        self.pending = set(BOLD_SCENARIO_RE.findall(_section(text, "⏳ 구현 대기")))
        self.non_scenario_files = set(
            FILE_REF_RE.findall(_section(text, "비-시나리오 파일"))
        )


def measure() -> tuple[dict, list[str]]:
    """레포의 실제 상태를 재도출한다. (측정값, 치명적 오류 목록)"""
    problems: list[str] = []

    try:
        decls = run_all.declarations()
    except run_all.DeclarationError as exc:
        return {}, [f"규칙 3 위반 — {exc}"]

    dedicated: dict[str, str] = {}
    shared: dict[str, list[str]] = {}
    non_scenario: list[str] = []
    for decl in decls:
        if decl.non_scenario:
            non_scenario.append(decl.name)
        elif len(decl.scenarios) == 1:
            scenario = decl.scenarios[0]
            if scenario in dedicated:
                problems.append(
                    f"규칙 1 위반 — {scenario} 를 두 파일이 전용 선언한다: "
                    f"{dedicated[scenario]}, {decl.name}"
                )
            dedicated[scenario] = decl.name
        else:
            for scenario in decl.scenarios:
                shared.setdefault(scenario, []).append(decl.name)

    return {
        "declarations": decls,
        "dedicated": dedicated,
        "shared": shared,
        "non_scenario": non_scenario,
        "split_pending": [d.name for d in decls if len(d.scenarios) > 1],
    }, problems


def check_dispatch(decls) -> list[str]:
    """매칭 단위 파일 전부가 러너에 정확히 한 번 배차되는지."""
    problems = []
    dispatched: list[str] = []
    for group in run_all.GROUPS:
        dispatched += [d.name for d in run_all.dispatch_plan(group)]
    expected = {d.name for d in decls}
    missing = expected - set(dispatched)
    if missing:
        problems.append(
            f"하네스 위반 — 러너가 배차하지 않는 매칭 단위 파일: {sorted(missing)} "
            f"(CI가 실행하지 않는 파일이 된다)"
        )
    duplicated = {name for name in dispatched if dispatched.count(name) > 1}
    if duplicated:
        problems.append(f"하네스 위반 — 두 번 이상 배차되는 파일: {sorted(duplicated)}")
    return problems


def check_cases_are_run(decls) -> list[str]:
    """각 파일의 ``run()`` 이 그 파일이 정의한 ``test_*`` 케이스를 전부 호출하는지.

    배차만으로는 부족하다 — 파일 하나에 케이스 하나인 구조에서는 디스패처가 케이스를
    부르는 줄을 빠뜨려도 그 파일은 여전히 exit 0 이라 CI가 초록으로 통과한다. 그 파일이
    선언한 시나리오는 레지스트리에서 ✅ 로 세지지만 실제로는 아무것도 단언하지 않는다.
    AST 만 보므로 클러스터도 서드파티 임포트도 필요 없다.
    """
    problems = []
    for decl in decls:
        tree = ast.parse(decl.path.read_text(encoding="utf-8"))
        top = [
            n
            for n in tree.body
            if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef))
        ]
        cases = [n.name for n in top if n.name.startswith("test_")]
        if not cases:
            continue
        run = next((n for n in top if n.name == "run"), None)
        if run is None:
            problems.append(
                f"하네스 위반 — {decl.name} 은 케이스 {cases} 를 정의하는데 run() 이 없다"
            )
            continue
        called = {
            n.func.id
            for n in ast.walk(run)
            if isinstance(n, ast.Call) and isinstance(n.func, ast.Name)
        }
        missing = [case for case in cases if case not in called]
        if missing:
            problems.append(
                f"하네스 위반 — {decl.name} 의 run() 이 호출하지 않는 케이스: {missing} "
                f"(배차돼도 아무것도 단언하지 않고 통과한다)"
            )
    return problems


def check_test_docs(scenarios: dict[str, str], dedicated: dict[str, str]) -> list[str]:
    """규칙 7 — 각 시나리오의 `자동화` 필드가 말하는 e2e 현황이 실측 파일 집합과 같은지.

    `docs/test-*.md` 를 읽는 게이트가 하나도 없어서, e2e 파일을 만든 PR 이 그 시나리오의
    자동화 필드를 갱신하지 않아도 CI 가 초록이었다. 그 사이 문서는 "아직 (미작성)" 이라고
    말하고 파일은 실재하는 상태로 벌어진다 — 2026-09-04 에 그 어긋남이 세 번의 감지를
    통과했다. 이 검사는 그 자리를 기계로 옮긴다. 판정은 **문서의 자기신고가 아니라 실측
    파일 집합**(`dedicated`) 기준이다.
    """
    problems = []
    automation: dict[str, str] = {}
    for doc in sorted(DOCS.glob("test-*.md")):
        automation.update(scenario_automation(doc))
    for scenario in scenarios:
        field = automation.get(scenario, "")
        have = dedicated.get(scenario)
        if have and UNWRITTEN_MARK in field:
            problems.append(
                f"규칙 7 위반 — {scenario} 의 전용 파일 {have} 이 실재하는데 "
                f"자동화 필드가 아직 {UNWRITTEN_MARK} 이라고 한다"
            )
        if not have and INTEGRATION_REF_RE.search(field) and UNWRITTEN_MARK not in field:
            refs = sorted(set(INTEGRATION_REF_RE.findall(field)))
            problems.append(
                f"규칙 7 위반 — {scenario} 의 전용 파일이 실측되지 않는데 "
                f"자동화 필드가 {refs} 를 작성된 것처럼 적는다 "
                f"({UNWRITTEN_MARK} 표기가 빠졌다)"
            )
    return problems


def main() -> int:
    scenarios = scenario_universe()
    scenario_set = set(scenarios)
    tracker = Tracker(TRACKER.read_text(encoding="utf-8"))
    measured, problems = measure()
    if not measured:
        for problem in problems:
            print(f"FAIL: {problem}")
        return 1

    decls = measured["declarations"]
    dedicated = measured["dedicated"]
    shared = measured["shared"]

    # --- 규칙 5: 참조 무결성 -------------------------------------------------
    for decl in decls:
        for scenario in decl.scenarios:
            if scenario not in scenario_set:
                problems.append(
                    f"규칙 5 위반 — {decl.name} 이 실재하지 않는 시나리오 "
                    f"{scenario} 를 선언한다"
                )
    for scenario in tracker.rows:
        if scenario not in scenario_set:
            problems.append(
                f"규칙 5 위반 — 레지스트리가 실재하지 않는 시나리오 {scenario} 를 등재한다"
            )
    for scenario in tracker.exceptions:
        if scenario not in scenario_set:
            problems.append(
                f"규칙 5 위반 — 예외 목록이 실재하지 않는 시나리오 {scenario} 를 등재한다"
            )
    for scenario in tracker.pending:
        if scenario not in scenario_set:
            problems.append(
                f"규칙 5 위반 — 구현 대기 표가 실재하지 않는 시나리오 "
                f"{scenario} 를 등재한다"
            )
    missing_rows = scenario_set - set(tracker.rows)
    if missing_rows:
        problems.append(f"규칙 5 위반 — 레지스트리에 없는 시나리오: {sorted(missing_rows)}")

    # --- 예외와 구현 대기는 겹치면 안 된다 (영구 면제 vs 임시 보류) -----------
    both = tracker.exceptions & tracker.pending
    if both:
        problems.append(
            f"규칙 4 위반 — 예외와 구현 대기에 동시에 등재된 시나리오: {sorted(both)} "
            f"(영구 면제와 임시 보류를 섞지 않는다)"
        )

    # --- 규칙 3: 비-시나리오 파일 등재 --------------------------------------
    for name in measured["non_scenario"]:
        if name not in tracker.non_scenario_files:
            problems.append(
                f"규칙 3 위반 — 비-시나리오 파일 {name} 이 doc-tracker 에 "
                f"등재돼 있지 않다(고아)"
            )
    for name in tracker.non_scenario_files:
        if name not in measured["non_scenario"]:
            problems.append(
                f"규칙 3 위반 — doc-tracker 가 비-시나리오로 등재한 {name} 이 "
                f"실재하지 않거나 시나리오를 선언한다(고아 등재)"
            )

    # --- 예외·구현 대기: 선언과 겹치면 안 된다 -------------------------------
    for scenario in sorted(tracker.exceptions | tracker.pending):
        if scenario in dedicated or scenario in shared:
            problems.append(
                f"등재 충돌 — {scenario} 는 예외/구현 대기로 등재됐는데 파일이 검증을 "
                f"선언한다 (등재를 해제하거나 선언을 지울 것)"
            )

    # --- 규칙 6: 행별 제목·상태가 실측과 같은가 ------------------------------
    for scenario, title in scenarios.items():
        declared_title = tracker.titles.get(scenario)
        if declared_title is not None and declared_title != title:
            problems.append(
                f"규칙 6 위반 — {scenario} 레지스트리 제목 {declared_title!r} ≠ "
                f"문서 헤딩 {title!r} (서수가 밀려 식별자가 바뀌었을 수 있다)"
            )
        status = tracker.rows.get(scenario, "")
        refs = FILE_REF_RE.findall(status)
        if status.startswith("✅"):
            if not refs or dedicated.get(scenario) != refs[0]:
                problems.append(
                    f"규칙 6 위반 — {scenario} 레지스트리는 전용 파일 {refs or ['?']} 라고 "
                    f"하는데 실측은 {dedicated.get(scenario) or '없음'}"
                )
        elif "분할 대기" in status:
            if not refs or refs[0] not in shared.get(scenario, []):
                problems.append(
                    f"규칙 6 위반 — {scenario} 레지스트리는 겸용 파일 {refs or ['?']} 라고 "
                    f"하는데 실측은 {shared.get(scenario) or '없음'}"
                )
        elif status.startswith("🚫"):
            if scenario not in tracker.exceptions:
                problems.append(
                    f"규칙 6 위반 — {scenario} 는 🚫 로 표시됐지만 예외 목록에 사유가 없다"
                )
        elif status.startswith("⏳"):
            if scenario not in tracker.pending:
                problems.append(
                    f"규칙 6 위반 — {scenario} 는 ⏳ 로 표시됐지만 구현 대기 표에 "
                    f"근거·해제 조건이 없다"
                )
        else:
            problems.append(
                f"규칙 6 위반 — {scenario} 의 상태 표기를 해석할 수 없다: {status!r}"
            )

    # --- 규칙 7: 테스트 문서의 e2e 현황이 실측과 같은가 -----------------------
    problems += check_test_docs(scenarios, dedicated)

    # --- 하네스 무결성 -------------------------------------------------------
    problems += check_dispatch(decls)
    problems += check_cases_are_run(decls)

    # --- 집계 ---------------------------------------------------------------
    exceptions = len(tracker.exceptions)
    pending = len(tracker.pending)
    targets = len(scenarios) - exceptions - pending
    matched = len(dedicated)
    blank = targets - matched
    counted = {
        "시나리오 전집": len(scenarios),
        "예외 등재": exceptions,
        "구현 대기 등재": pending,
        "1:1 대상": targets,
        "매칭 파일(전용)": matched,
        "분할 대기 파일(규칙 2 위반)": len(measured["split_pending"]),
        "공백 시나리오": blank,
    }
    for key in AGGREGATE_KEYS:
        declared = tracker.aggregate.get(key)
        if declared is None:
            problems.append(f"규칙 6 위반 — 집계 블록에 '{key}' 항목이 없다")
        elif declared != counted[key]:
            problems.append(
                f"규칙 6 위반 — 집계 '{key}' 등재 {declared} ≠ 실측 {counted[key]}"
            )

    # --- 불변식: 미등재 공백은 drift 다 --------------------------------------
    if blank != 0:
        orphans = sorted(
            s
            for s in scenarios
            if s not in dedicated
            and s not in tracker.exceptions
            and s not in tracker.pending
        )
        problems.append(
            f"불변식 위반 — (시나리오 {len(scenarios)} − 예외 {exceptions} − "
            f"구현 대기 {pending}) = {targets} ≠ 매칭 파일 {matched}. "
            f"전용 파일도 등재도 없는 시나리오: {orphans} "
            f"(전용 파일을 저작하거나 예외/구현 대기로 등재할 것)"
        )

    for key in AGGREGATE_KEYS:
        print(f"{key}: {counted[key]}")

    if problems:
        print()
        for problem in problems:
            print(f"FAIL: {problem}")
        return 1

    print(
        "\nOK: 규칙 1(중복 전용)·2·3·4·5·6·7 위반 없음, 불변식 성립, "
        "러너 배차 누락·케이스 미호출 없음"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
