#!/usr/bin/env python3
"""AC ↔ e2e **파일** 1:1 정합성 체커 (매칭 단위가 아니다 — AC를 주검증하지 않는다).

정합성 모델 `tbm_homelab-k3s-mcp-ac-e2e`는 `docs/prd-*.md`의 AC와 `tests/integration/`
최상위 `*.py` **파일**을 완전 1:1(전단사)로 유지할 것을 요구한다. 이 스크립트는 그 판정을
사람의 자기신고가 아니라 **레포의 실제 상태에서 재도출**해, `docs/doc-tracker.md`의 레지스트리와
대조한다. 클러스터도 서드파티 의존성도 필요 없다(표준 라이브러리 전용) — CI의 lint 잡에서 돈다.

세 개의 사실 원천을 읽는다.

1. **AC 전집** — `docs/prd-*.md`의 `### AC<n>:` 헤딩. AC 식별자는 `<domain>/AC<n>`이고
   `<domain>`은 `prd-<domain>.md` 파일명에서 온다.
2. **선언** — 각 매칭 단위 파일 모듈 docstring의 `검증 AC:` (파싱은 `run_all.py`가 소유).
   **정확히 1개**의 AC를 선언한 파일만 그 AC의 "전용 파일"로 세고, 2개 이상을 선언한 파일은
   규칙 2 위반(분할 대기)으로 세며 그 AC들은 여전히 **공백**으로 계수한다 — 겸용 파일은
   전단사를 만들지 못하기 때문이다.
3. **등재** — `docs/doc-tracker.md`의 레지스트리 표·예외 목록·비-AC 파일 목록·집계 블록.

판정하는 것:

* **규칙 1** AC → 전용 파일 유일 (같은 AC를 두 파일이 전용 선언하면 즉시 실패)
* **규칙 2** 파일 → AC 유일 (겸용 파일은 위반으로 계수되고, 레지스트리와 수가 일치해야 한다)
* **규칙 3** 비-AC 파일은 `검증 AC: 없음`을 선언하고 doc-tracker에 등재돼야 한다
* **규칙 5** 참조 무결성 — 선언·레지스트리·예외 목록이 실재하지 않는 AC를 가리키지 않는다
* **규칙 6** 집계 일치 — 레지스트리의 행별 상태와 집계 숫자가 실측과 정확히 같다
* **규칙 7** 테스트 문서 상태 일치 — `docs/test-<domain>.md` 시나리오의 `자동화` 필드가 말하는
  통합 e2e 현황이 실측 파일 집합과 같다(전용 파일이 실재하는데 `(미작성)`이 남아 있거나,
  전용 파일이 없는데 `(미작성)` 없이 파일을 참조하면 위반)
* **하네스 무결성** — 매칭 단위 파일 전부가 `run_all.py`에 정확히 한 번 배차되고,
  각 파일의 `run()`이 그 파일이 정의한 `test_*` 케이스를 **전부 호출한다**
  (만들어 놓고 CI가 실행하지 않는 파일, 그리고 배차는 되지만 자기 케이스를 부르지 않아
  **조용히 통과하는 파일**을 둘 다 구조적으로 막는다)

집계가 실측과 다르면 실패하므로, 파일을 쪼개거나 AC를 추가한 PR은 **같은 PR에서**
레지스트리를 갱신해야 한다. 그것이 이 모델이 요구하는 "집계 일치"다. 같은 이유로 규칙 7이
있다 — e2e 파일을 새로 만든 PR은 그 AC의 테스트 문서에서 `(미작성)` 표기를 같은 PR에서
지워야 한다.
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

AC_HEADING_RE = re.compile(r"^### (AC\d+):", re.MULTILINE)
ROW_RE = re.compile(r"^\| ([a-z0-9-]+/AC\d+) \| ([^|]*) \| ([^|]*) \|$", re.MULTILINE)
AGGREGATE_RE = re.compile(
    r"<!-- ac-e2e-집계 -->(.*?)<!-- /ac-e2e-집계 -->", re.DOTALL
)
AGGREGATE_LINE_RE = re.compile(r"^- (.+): (\d+)$", re.MULTILINE)
FILE_REF_RE = re.compile(r"`([a-z_0-9]+\.py)`")

# --- 규칙 7: 테스트 문서(`docs/test-<domain>.md`)의 자동화 필드 -------------------
SCENARIO_RE = re.compile(r"^### 시나리오 .*$", re.MULTILINE)
DOC_FIELD_RE = re.compile(r"^- \*\*(검증 AC|자동화)\*\*:")
INTEGRATION_REF_RE = re.compile(r"`tests/integration/([a-z_0-9]+\.py)")
UNWRITTEN_MARK = "(미작성)"

AGGREGATE_KEYS = (
    "AC 전집",
    "예외 등재",
    "1:1 대상",
    "매칭 파일(전용)",
    "분할 대기 파일(규칙 2 위반)",
    "공백 AC",
)


def ac_universe() -> list[str]:
    """`docs/prd-*.md`에서 AC 전집을 재도출한다."""
    acs = []
    for prd in sorted(DOCS.glob("prd-*.md")):
        domain = prd.name[len("prd-") : -len(".md")]
        for match in AC_HEADING_RE.finditer(prd.read_text(encoding="utf-8")):
            acs.append(f"{domain}/{match.group(1)}")
    return acs


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
        registry = text.split("### AC 레지스트리")[-1]
        self.rows = {ac: status.strip() for ac, _, status in ROW_RE.findall(registry)}
        self.row_order = [ac for ac, _, _ in ROW_RE.findall(registry)]

        aggregate = AGGREGATE_RE.search(text)
        self.aggregate = (
            {k.strip(): int(v) for k, v in AGGREGATE_LINE_RE.findall(aggregate.group(1))}
            if aggregate
            else {}
        )

        self.exceptions = set(
            re.findall(r"\*\*([a-z0-9-]+/AC\d+)\*\*", _section(text, "🚫 e2e 예외"))
        )
        self.non_ac_files = set(FILE_REF_RE.findall(_section(text, "비-AC 파일")))


def measure() -> tuple[dict, list[str]]:
    """레포의 실제 상태를 재도출한다. (측정값, 치명적 오류 목록)"""
    problems: list[str] = []

    try:
        decls = run_all.declarations()
    except run_all.DeclarationError as exc:
        return {}, [f"규칙 3 위반 — {exc}"]

    dedicated: dict[str, str] = {}
    shared: dict[str, list[str]] = {}
    non_ac: list[str] = []
    for decl in decls:
        if decl.non_ac:
            non_ac.append(decl.name)
        elif len(decl.acs) == 1:
            ac = decl.acs[0]
            if ac in dedicated:
                problems.append(
                    f"규칙 1 위반 — {ac} 를 두 파일이 전용 선언한다: "
                    f"{dedicated[ac]}, {decl.name}"
                )
            dedicated[ac] = decl.name
        else:
            for ac in decl.acs:
                shared.setdefault(ac, []).append(decl.name)

    return {
        "declarations": decls,
        "dedicated": dedicated,
        "shared": shared,
        "non_ac": non_ac,
        "split_pending": [d.name for d in decls if len(d.acs) > 1],
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
    선언한 AC는 레지스트리에서 ✅ 로 세지지만 실제로는 아무것도 단언하지 않는다.
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


def _scenario_automation(text: str) -> list[tuple[str, list[str]]]:
    """테스트 문서의 시나리오별 ``(자동화 필드 본문, 검증 AC 번호들)``.

    필드는 여러 줄로 이어질 수 있으므로 다음 ``- **`` 불릿까지를 한 필드로 본다.
    """
    blocks: list[tuple[str, list[str]]] = []
    for body in SCENARIO_RE.split(text)[1:]:
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
        blocks.append(
            (fields.get("자동화", ""), re.findall(r"AC\d+", fields.get("검증 AC", "")))
        )
    return blocks


def check_test_docs(acs: list[str], dedicated: dict[str, str]) -> list[str]:
    """규칙 7 — 테스트 문서가 말하는 통합 e2e 현황이 실측 파일 집합과 같은지.

    `docs/test-*.md` 를 읽는 게이트가 하나도 없어서, e2e 파일을 만든 PR 이 그 AC 의 테스트
    문서를 갱신하지 않아도 CI 가 초록이었다. 그 사이 문서는 "아직 (미작성)" 이라고 말하고
    파일은 실재하는 상태로 벌어진다 — 2026-09-04 에 그 어긋남이 세 번의 감지를 통과했다.
    이 검사는 그 자리를 기계로 옮긴다. 판정은 **문서의 자기신고가 아니라 실측 파일 집합**
    (`dedicated`) 기준이다.
    """
    problems = []
    for ac in acs:
        domain, number = ac.split("/")
        doc = DOCS / f"test-{domain}.md"
        if not doc.exists():
            problems.append(f"규칙 7 위반 — {ac} 의 테스트 문서 test-{domain}.md 가 없다")
            continue
        blocks = [
            automation
            for automation, declared in _scenario_automation(
                doc.read_text(encoding="utf-8")
            )
            if number in declared
        ]
        if not blocks:
            problems.append(
                f"규칙 7 위반 — test-{domain}.md 에 {ac} 를 검증하는 시나리오가 없다"
            )
            continue
        have = dedicated.get(ac)
        for automation in blocks:
            if have and UNWRITTEN_MARK in automation:
                problems.append(
                    f"규칙 7 위반 — {ac} 의 전용 파일 {have} 이 실재하는데 "
                    f"test-{domain}.md 의 자동화 필드가 아직 {UNWRITTEN_MARK} 이라고 한다"
                )
            if (
                not have
                and INTEGRATION_REF_RE.search(automation)
                and UNWRITTEN_MARK not in automation
            ):
                refs = sorted(set(INTEGRATION_REF_RE.findall(automation)))
                problems.append(
                    f"규칙 7 위반 — {ac} 의 전용 파일이 실측되지 않는데 "
                    f"test-{domain}.md 의 자동화 필드가 {refs} 를 작성된 것처럼 적는다 "
                    f"({UNWRITTEN_MARK} 표기가 빠졌다)"
                )
    return problems


def main() -> int:
    acs = ac_universe()
    ac_set = set(acs)
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
        for ac in decl.acs:
            if ac not in ac_set:
                problems.append(
                    f"규칙 5 위반 — {decl.name} 이 실재하지 않는 AC {ac} 를 선언한다"
                )
    for ac in tracker.rows:
        if ac not in ac_set:
            problems.append(f"규칙 5 위반 — 레지스트리가 실재하지 않는 AC {ac} 를 등재한다")
    for ac in tracker.exceptions:
        if ac not in ac_set:
            problems.append(f"규칙 5 위반 — 예외 목록이 실재하지 않는 AC {ac} 를 등재한다")
    missing_rows = ac_set - set(tracker.rows)
    if missing_rows:
        problems.append(f"규칙 5 위반 — 레지스트리에 없는 AC: {sorted(missing_rows)}")

    # --- 규칙 3: 비-AC 파일 등재 --------------------------------------------
    for name in measured["non_ac"]:
        if name not in tracker.non_ac_files:
            problems.append(
                f"규칙 3 위반 — 비-AC 파일 {name} 이 doc-tracker 에 등재돼 있지 않다(고아)"
            )
    for name in tracker.non_ac_files:
        if name not in measured["non_ac"]:
            problems.append(
                f"규칙 3 위반 — doc-tracker 가 비-AC 로 등재한 {name} 이 실재하지 않거나 "
                f"AC 를 선언한다(고아 등재)"
            )

    # --- 예외: 선언과 겹치면 안 된다 -----------------------------------------
    for ac in sorted(tracker.exceptions):
        if ac in dedicated or ac in shared:
            problems.append(
                f"예외 충돌 — {ac} 는 예외로 등재됐는데 파일이 검증을 선언한다 "
                f"(예외를 해제하거나 선언을 지울 것)"
            )

    # --- 규칙 6: 행별 상태가 실측과 같은가 -----------------------------------
    for ac in acs:
        status = tracker.rows.get(ac, "")
        refs = FILE_REF_RE.findall(status)
        if status.startswith("✅"):
            if not refs or dedicated.get(ac) != refs[0]:
                problems.append(
                    f"규칙 6 위반 — {ac} 레지스트리는 전용 파일 {refs or ['?']} 라고 하는데 "
                    f"실측은 {dedicated.get(ac) or '없음'}"
                )
        elif "분할 대기" in status:
            if not refs or refs[0] not in shared.get(ac, []):
                problems.append(
                    f"규칙 6 위반 — {ac} 레지스트리는 겸용 파일 {refs or ['?']} 라고 하는데 "
                    f"실측은 {shared.get(ac) or '없음'}"
                )
        elif status.startswith("🚫"):
            if ac not in tracker.exceptions:
                problems.append(
                    f"규칙 6 위반 — {ac} 는 🚫 로 표시됐지만 예외 목록에 사유가 없다"
                )
        elif "공백" in status:
            if ac in dedicated or ac in shared:
                problems.append(
                    f"규칙 6 위반 — {ac} 는 공백으로 표시됐지만 파일이 검증을 선언한다"
                )
        else:
            problems.append(f"규칙 6 위반 — {ac} 의 상태 표기를 해석할 수 없다: {status!r}")

    # --- 규칙 7: 테스트 문서의 e2e 현황이 실측과 같은가 -----------------------
    problems += check_test_docs(acs, dedicated)

    # --- 하네스 무결성 -------------------------------------------------------
    problems += check_dispatch(decls)
    problems += check_cases_are_run(decls)

    # --- 집계 ---------------------------------------------------------------
    exceptions = len(tracker.exceptions)
    targets = len(acs) - exceptions
    matched = len(dedicated)
    counted = {
        "AC 전집": len(acs),
        "예외 등재": exceptions,
        "1:1 대상": targets,
        "매칭 파일(전용)": matched,
        "분할 대기 파일(규칙 2 위반)": len(measured["split_pending"]),
        "공백 AC": targets - matched,
    }
    for key in AGGREGATE_KEYS:
        declared = tracker.aggregate.get(key)
        if declared is None:
            problems.append(f"규칙 6 위반 — 집계 블록에 '{key}' 항목이 없다")
        elif declared != counted[key]:
            problems.append(
                f"규칙 6 위반 — 집계 '{key}' 등재 {declared} ≠ 실측 {counted[key]}"
            )

    for key in AGGREGATE_KEYS:
        print(f"{key}: {counted[key]}")
    print(
        "공백 내역: 겸용 파일이 커버 "
        f"{len({ac for ac in shared if ac not in dedicated})} · 케이스 자체 없음 "
        f"{targets - matched - len({ac for ac in shared if ac not in dedicated})}"
    )

    if problems:
        print()
        for problem in problems:
            print(f"FAIL: {problem}")
        return 1

    print(
        "\nOK: 규칙 1(중복 전용)·2·3·5·6·7 위반 없음, 러너 배차 누락·케이스 미호출 없음"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
