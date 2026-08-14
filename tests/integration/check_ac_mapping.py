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
* **하네스 무결성** — 매칭 단위 파일 전부가 `run_all.py`에 정확히 한 번 배차된다
  (파일을 만들어 놓고 CI가 실행하지 않는 상태를 구조적으로 막는다)

집계가 실측과 다르면 실패하므로, 파일을 쪼개거나 AC를 추가한 PR은 **같은 PR에서**
레지스트리를 갱신해야 한다. 그것이 이 모델이 요구하는 "집계 일치"다.
"""

from __future__ import annotations

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

    # --- 하네스 무결성 -------------------------------------------------------
    problems += check_dispatch(decls)

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

    print("\nOK: 규칙 1(중복 전용)·2·3·5·6 위반 없음, 러너 배차 누락 없음")
    return 0


if __name__ == "__main__":
    sys.exit(main())
