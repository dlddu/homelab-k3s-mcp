#!/usr/bin/env python3
"""테스트 시나리오 ↔ e2e **파일** 1:1 정합성 체커 (매칭 단위가 아니다).

각 매칭 단위 파일 모듈 docstring의 `검증 시나리오:` (파싱은 `run_all.py`가 소유).

판정하는 것:

* **규칙 5** 참조 무결성 — 선언·레지스트리·예외 목록·구현 대기 표가 실재하지 않는 시나리오를
  가리키지 않는다
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
TRACKER = sorted((DOCS / "doc-tracker").glob("[0-9][0-9][0-9][0-9]-[0-9][0-9].md"))[-1]

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
    scenarios: dict[str, str] = {}
    for doc in sorted(DOCS.glob("test-*.md")):
        for number, title in SCENARIO_HEADING_RE.findall(
            doc.read_text(encoding="utf-8")
        ):
            scenarios[f"{doc.name}#시나리오 {number}"] = title.strip()
    return scenarios


def scenario_automation(doc: pathlib.Path) -> dict[str, str]:
    """필드는 여러 줄로 이어질 수 있으므로 다음 ``- **`` 불릿까지를 한 필드로 본다.
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
    """그 파일이
    선언한 시나리오는 레지스트리에서 ✅ 로 세지지만 실제로는 아무것도 단언하지 않는다.
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
