#!/usr/bin/env python3
"""허브 도달 가능 문서 집계 ↔ `docs/` 실측 대조 체커.

`docs/doc-tracker.md` 의 「문서 공개」 표에 있는 「허브 도달 가능 문서」 줄은 두 가지를
주장한다 — ⑴ `docs/` 의 마크다운 문서가 **전부** 허브(`index.html`)에서 도달 가능하고
⑵ 그 수와 종류별 내역이 맞다는 것. 같은 문서의 「대기·주의 항목」은 이 줄이 어긋나는 원인을
하나로 지목한다: **문서를 추가하면서 허브 집계를 함께 올리지 않는 것.**

그 지목은 맞았지만, 지키는 것이 사람뿐이라 실제로는 지켜지지 않았다. `docs/comment-policy/passes/`
에 판정 패스 문서가 하나씩 들어온 네 번의 PR 이 **연속으로** 이 줄을 건드리지 않았고, 그동안
집계는 34 에 멈춘 채 실측은 38 이 됐다. 네 번 모두 CI 는 초록이었다 — 이 줄을 읽는 기계가
없었기 때문이다. 이 스크립트가 그 자리를 메운다.

`scripts/check_comment_policy.py` 가 원장 프로즈에 대해, `tests/integration/check_ac_mapping.py`
가 시나리오 레지스트리에 대해 하는 일과 같은 성질이다 — **사람의 자기신고를 레포의 실제 상태에서
재도출한 값과 대조한다.**

판정하는 것:

* **D1** `docs/**/*.md` 가 전부 다섯 종류 중 하나로 분류된다. 분류되지 않는 문서가 있으면
  실패다 — 인벤토리 표에 종류를 추가하고 이 스크립트의 산식을 함께 갱신해야 한다는 뜻이다.
  (조용히 「정책」으로 흘려보내면 표가 말하는 종류와 집계가 갈라진다.)
* **D2** 허브 `index.html` 에서 시작한 도달 가능 집합이 `docs/**/*.md` 전체와 같다. 도달
  경로는 **이행적**이다 — 허브가 직접 링크하지 않아도 도달 가능한 문서가 그 문서를 링크하면
  도달 가능하다(`README.md` → `ledger.md` → `passes/*.md` 가 실제 그 형태다). 허브 직접 링크만
  세면 도달 가능성을 과소평가한다.
* **D3** `.md` 를 가리키는 상대 링크가 전부 실재하는 파일을 가리킨다(= 「끊긴 링크 0」).
* **D4** 위 줄이 주장하는 총계·종류별 내역·끊긴 링크 수가 D1~D3 의 실측과 정확히 같다.

D2 와 D4 를 함께 두는 것이 중요하다. 집계 숫자만 파일 개수와 맞추면 **「도달 가능」이라는 말이
검사되지 않은 채 남는다** — 문서 하나가 링크에서 떨어져 나가도 개수는 그대로이므로 통과한다.
반대로 도달성만 보면 줄의 숫자가 낡는 것을 못 잡는다. 줄이 둘 다 주장하므로 둘 다 잰다.
"""

from __future__ import annotations

import pathlib
import re
import sys
from collections import deque

HERE = pathlib.Path(__file__).resolve().parent
REPO_ROOT = HERE.parent
DOCS = REPO_ROOT / "docs"
TRACKER = sorted((DOCS / "doc-tracker").glob("[0-9][0-9][0-9][0-9]-[0-9][0-9].md"))[-1]
HUB = DOCS / "index.html"

#: `reader.html?doc=<docs 기준 상대경로>.md` 규약은 doc-tracker 의 「이 레포에 맞춘 뷰어 규약」
#: 절이 정의한다.
HUB_LINK_RE = re.compile(r'href="reader\.html\?doc=([^"#]+\.md)')
MD_LINK_RE = re.compile(r"\]\(\s*<?([^)\s<>]+\.md)")
#: 형태가 바뀌면 파싱 실패로 멈춘다(조용히 0 으로 떨어지지 않게).
CLAIM_RE = re.compile(
    r"^\|[ \t]*허브 도달 가능 문서[ \t]*\|.*?\*\*(\d+)[ \t]*/[ \t]*(\d+)\*\*[ \t]*"
    r"\(가치[ \t]*(\d+)[ \t]*\+[ \t]*PRD[ \t]*(\d+)[ \t]*\+[ \t]*테스트[ \t]*(\d+)"
    r"[ \t]*\+[ \t]*상태 추적[ \t]*(\d+)[ \t]*\+[ \t]*정책[ \t]*(\d+)\)[ \t]*,"
    r"[ \t]*끊긴 링크[ \t]*(\d+)",
    re.MULTILINE,
)

#: PRD(도구)와 PRD(공통)는 표에서만 갈리고 집계에서는 한 칸이므로 여기서도 합친다.
CATEGORIES = ("가치", "PRD", "테스트", "상태 추적", "정책")
POLICY_PREFIXES = ("comment-policy/",)
POLICY_FILES = ("e2e-mocking-policy.md",)


def categorize(rel: str) -> str | None:
    """`docs/` 기준 상대경로 → 종류. 분류 불가면 None(= D1 실패)."""
    if rel == "values.md":
        return "가치"
    if rel.startswith("doc-tracker/"):
        return "상태 추적"
    if "/" not in rel and rel.startswith("prd-"):
        return "PRD"
    if "/" not in rel and rel.startswith("test-"):
        return "테스트"
    if rel in POLICY_FILES or rel.startswith(POLICY_PREFIXES):
        return "정책"
    return None


def normalize(base: pathlib.PurePosixPath, target: str) -> str:
    """링크 대상을 `docs/` 기준 상대경로로. 링크는 그것을 담은 문서 위치 기준이다."""
    raw = target if str(base) == "." else (base / target).as_posix()
    parts: list[str] = []
    for segment in raw.split("/"):
        if segment == "..":
            if parts:
                parts.pop()
        elif segment not in (".", ""):
            parts.append(segment)
    return "/".join(parts)


def md_links(rel: str) -> list[str]:
    """문서 하나가 가리키는 `.md` 대상들(`docs/` 기준 상대경로). 외부 URL 은 뺀다."""
    base = pathlib.PurePosixPath(rel).parent
    out: list[str] = []
    for target in MD_LINK_RE.findall((DOCS / rel).read_text(encoding="utf-8")):
        if target.startswith(("http://", "https://", "mailto:", "/")):
            continue
        out.append(normalize(base, target))
    return out


def reachable(universe: set[str]) -> tuple[set[str], list[str]]:
    """허브에서 이행적으로 도달 가능한 집합과, 실재하지 않는 링크 대상 목록."""
    broken: list[str] = []
    seeds = []
    for target in sorted(set(HUB_LINK_RE.findall(HUB.read_text(encoding="utf-8")))):
        if target in universe:
            seeds.append(target)
        else:
            broken.append(f"{HUB.name} -> {target}")

    seen = set(seeds)
    queue = deque(seeds)
    while queue:
        current = queue.popleft()
        for target in md_links(current):
            if target not in universe:
                broken.append(f"{current} -> {target}")
                continue
            if target not in seen:
                seen.add(target)
                queue.append(target)

    # 도달 불가 문서 안의 끊긴 링크도 끊긴 링크다. BFS 는 그 문서를 펴 보지 않으므로 따로 돈다.
    for rel in sorted(universe - seen):
        for target in md_links(rel):
            if target not in universe:
                broken.append(f"{rel} -> {target}")

    return seen, sorted(set(broken))


def main() -> int:
    problems: list[str] = []
    universe = {p.relative_to(DOCS).as_posix() for p in DOCS.rglob("*.md")}

    # --- D1: 분류 ------------------------------------------------------------
    counts = dict.fromkeys(CATEGORIES, 0)
    for rel in sorted(universe):
        kind = categorize(rel)
        if kind is None:
            problems.append(
                f"D1 위반 — {rel} 을 다섯 종류(가치·PRD·테스트·상태 추적·정책) 중 "
                f"어디에도 넣을 수 없다. doc-tracker 의 「문서 인벤토리」 표와 이 "
                f"스크립트의 categorize() 를 함께 갱신할 것."
            )
            continue
        counts[kind] += 1

    # --- D2/D3: 도달성과 끊긴 링크 -------------------------------------------
    seen, broken = reachable(universe)
    unreachable = sorted(universe - seen)
    if unreachable:
        problems.append(
            f"D2 위반 — 허브에서 도달할 수 없는 문서: {unreachable} "
            f"(허브에 링크를 추가하거나, 이미 도달 가능한 문서가 링크하게 할 것)"
        )
    for link in broken:
        problems.append(f"D3 위반 — 끊긴 링크: {link}")

    total = len(universe)
    print(f"docs/ 마크다운 문서: {total}")
    for kind in CATEGORIES:
        print(f"  {kind}: {counts[kind]}")
    print(f"허브 도달 가능: {len(seen)} / {total} · 끊긴 링크 {len(broken)}")

    # --- D4: 집계 줄이 실측과 같은가 -----------------------------------------
    claim = CLAIM_RE.search(TRACKER.read_text(encoding="utf-8"))
    if claim is None:
        problems.append(
            "D4 위반 — doc-tracker 의 「허브 도달 가능 문서」 줄을 읽지 못했다. 형태는 "
            "`| 허브 도달 가능 문서 | ✅ **N / N** (가치 a + PRD b + 테스트 c + "
            "상태 추적 d + 정책 e), 끊긴 링크 f |` 여야 한다."
        )
    else:
        declared_reachable, declared_total = int(claim[1]), int(claim[2])
        declared = {
            "가치": int(claim[3]),
            "PRD": int(claim[4]),
            "테스트": int(claim[5]),
            "상태 추적": int(claim[6]),
            "정책": int(claim[7]),
        }
        declared_broken = int(claim[8])
        if declared_total != total:
            problems.append(
                f"D4 위반 — 집계 총계 등재 {declared_total} ≠ 실측 {total}"
            )
        if declared_reachable != len(seen):
            problems.append(
                f"D4 위반 — 집계 도달 수 등재 {declared_reachable} ≠ 실측 {len(seen)}"
            )
        for kind in CATEGORIES:
            if declared[kind] != counts[kind]:
                problems.append(
                    f"D4 위반 — 집계 '{kind}' 등재 {declared[kind]} ≠ "
                    f"실측 {counts[kind]}"
                )
        if sum(declared.values()) != declared_total:
            problems.append(
                f"D4 위반 — 집계 내역 합 {sum(declared.values())} ≠ "
                f"등재 총계 {declared_total} (줄 안에서 이미 어긋난다)"
            )
        if declared_broken != len(broken):
            problems.append(
                f"D4 위반 — 끊긴 링크 등재 {declared_broken} ≠ 실측 {len(broken)}"
            )

    if problems:
        print()
        for problem in problems:
            print(f"FAIL: {problem}")
        return 1

    print("\nOK: D1~D4 위반 없음 — 허브 집계가 실측과 같다")
    return 0


if __name__ == "__main__":
    sys.exit(main())
