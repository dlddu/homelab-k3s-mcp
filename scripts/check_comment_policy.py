#!/usr/bin/env python3
"""주석 비중복성 판정 원장 ↔ 실제 주석 상태 대조 체커.

정합성 모델 `tbm_homelab-k3s-mcp-comment-redundancy`의 to-be 는 `docs/comment-policy.md`
자신이다. 그래서 **그 문서가 자기 자신에 대해 적은 수치가 낡으면 to-be 가 틀린 것**이 되는데,
사람이 쓴 프로즈 수치는 조용히 낡는다 — 실제로 정책 문서가 착지하던 날, 문서가 「판정 대상
주석 848줄」이라 적은 사이에 형제 PR 이 머지되어 963줄이 됐고 아무것도 그것을 알려주지
않았다. 이 스크립트가 그 자리를 메운다.

이 체커가 판정하는 것은 **원장의 무결성**이지 중복 자체가 아니다. 「이 주석이 복원 가능한가」는
정책의 판정 절차대로 사람이 답하고, 기계는 **판정이 끝난 범위가 그 뒤로 변하지 않았는지**만
본다. 변했다면 그 범위는 다시 판정받아야 하고, 그때 원장을 갱신하는 것이 곧 재판정의 기록이다.

클러스터도 서드파티 의존성도 필요 없다(표준 라이브러리 전용) — CI 의 lint 잡에서 돈다.

판정 대상은 **두 표면**이다. 정책의 「지문의 사각지대」 절이 적어 두었듯 줄 주석 지문은
`^\\s*(//|#)` 에 걸리는 줄만 보는데, Python docstring 본문은 줄 접두사로 식별되지 않아
그 지문에 잡히지 않는다. 하나의 정규식을 넓혀 둘을 함께 재는 길은 없으므로(파서가 필요하다)
**표면을 나란히 두고 각각 재측정한다.** 표면이 갈려 있으므로 줄 주석 원장은 흔들리지 않고,
docstring 판정은 슬라이스마다 누적할 수 있다.

줄 주석 표면 — 모델 `tbm_homelab-k3s-mcp-comment-redundancy` 의 as-is 지문과 같은 정의:

* **R1** 원장 행의 범위 파일이 실재하고 정책의 스캔 범위 안이다. 빈 범위는 등재가 아니다.
* **R2** 각 행의 범위를 **모델 as-is 지문과 동일한 추출·정규화·정렬**로 재측정한 주석 줄 수와
  지문이 등재값과 같다. 다르면 그 범위는 판정 이후 주석이 바뀐 것이므로 재판정 대상이다.
* **R3** 행 사이에 같은 파일이 두 번 등재되지 않는다(판정 완료량의 이중 계상 방지).
* **R4** 합계 마커 == 원장 행들의 주석 줄 수 합(양방향 미러). 범위를 더하거나 빼면 같은 PR 에서
  합계도 움직여야 한다.

docstring 표면 — `ast` 로 뜯은 module·class·function docstring 의 본문(빈 줄과 기계가 읽는
선언 줄은 제외). 모델 as-is 는 **아직 이 표면을 보지 않는다**(bash 스크립트가 파서를 돌리지
않는다) — 그래서 재감지가 늘어남을 알려주지 못하고, 그 자리를 R8 이 메운다:

* **R5** docstring 원장 행의 범위가 실재하는 `.py` 이고 스캔 범위 안이다.
* **R6** 각 행의 재측정 줄 수·지문이 등재값과 같다(R2 와 같은 성질).
* **R7** docstring 행 사이 이중 등재 금지(R3 과 같은 성질).
* **R8** docstring 합계 마커 == 행 합이고, **합계 + 잔량 마커 == 실측 전체**. 잔량은
  「아직 판정받지 않은 docstring 줄 수」이며, 새 파일이 docstring 을 들고 들어오면 이 수가
  움직여 CI 가 멈춘다. 그때 마커를 갱신하는 행위가 곧 **「이만큼은 아직 판정받지 않았다」는
  명시적 선언**이다 — 사각지대가 조용히 커지는 성질을 없애는 것이 이 규칙의 목적이다.

통과하면 **두 표면의 현재 인구조사를 출력한다.** 그 수치는 문서 프로즈에 적지 않는다 —
낡는 형태를 없애는 것이 이 게이트의 목적이고, 최신값이 필요하면 여기서 읽는다.
"""

from __future__ import annotations

import ast
import hashlib
import pathlib
import re
import subprocess
import sys

HERE = pathlib.Path(__file__).resolve().parent
REPO_ROOT = HERE.parent
POLICY = REPO_ROOT / "docs" / "comment-policy.md"

# 아래 넷은 모델 tbm_homelab-k3s-mcp-comment-redundancy 의 as-is 버전 스크립트와 글자 그대로
# 같은 정의다. 하나라도 갈리면 이 게이트가 강제하는 지문이 모델이 관측하는 지문과 다른 것을
# 재므로, 고칠 때는 모델 정의와 함께 고칠 것.
SCAN_PATHSPECS = ("main.go", "internal", "tests", "scripts")
SCAN_EXCLUDE_RE = re.compile(r"(^|/)(\.venv|vendor|node_modules)/|\.pb\.go$")
COMMENT_RE = re.compile(r"^\s*(//|#)")
DIRECTIVE_RE = re.compile(
    r"//go:|nolint|#!|# ?noqa|# ?type:|# ?pragma|검증 AC:|mock-exception:"
)

# docstring 표면에서만 쓰는 제외 목록. `run_all.py` 가 모듈 docstring 에서 파싱하는 선언
# 필드들이고, DIRECTIVE_RE 의 `검증 AC:` 와 같은 자리에 있다(그쪽은 두 표면이 공유한다).
DOCSTRING_DECL_RE = re.compile(r"^(실행 대상|추가 인자|실행 순서):")

LEDGER_OPEN = "<!-- 판정-원장 -->"
LEDGER_CLOSE = "<!-- /판정-원장 -->"
TOTAL_OPEN = "<!-- 판정-합계 -->"
TOTAL_CLOSE = "<!-- /판정-합계 -->"
DOC_LEDGER_OPEN = "<!-- docstring-원장 -->"
DOC_LEDGER_CLOSE = "<!-- /docstring-원장 -->"
DOC_TOTAL_OPEN = "<!-- docstring-합계 -->"
DOC_TOTAL_CLOSE = "<!-- /docstring-합계 -->"
DOC_REMAINING_OPEN = "<!-- docstring-잔량 -->"
DOC_REMAINING_CLOSE = "<!-- /docstring-잔량 -->"

BACKTICKED_RE = re.compile(r"`([^`]+)`")
FINGERPRINT_LEN = 12

failures: list[str] = []


def fail(rule: str, message: str) -> None:
    failures.append(f"[{rule}] {message}")


def scan_files() -> list[str]:
    """모델 as-is 지문과 동일한 스캔 범위(레포 상대 경로, 정렬)."""
    out = subprocess.run(
        ["git", "ls-files", "--", *SCAN_PATHSPECS],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        check=True,
    ).stdout
    paths = [ln for ln in out.splitlines() if ln and not SCAN_EXCLUDE_RE.search(ln)]
    if not paths:
        raise SystemExit(
            "스캔 범위가 비어 있다 — git ls-files 가 아무 파일도 돌려주지 않았다. "
            "레포 루트에서 실행 중인지 확인할 것."
        )
    return sorted(paths)


def comment_lines(paths: list[str]) -> list[str]:
    """`경로:주석` 줄의 정규화·정렬 목록. as-is 지문과 같은 구성이다.

    지시어 주석(기계가 읽는 것)은 제외한다. 판정 대상이 아니기 때문이다.
    """
    hits: list[str] = []
    for rel in sorted(paths):
        try:
            text = (REPO_ROOT / rel).read_text(encoding="utf-8")
        except (UnicodeDecodeError, FileNotFoundError):
            continue
        for line in text.splitlines():
            if not COMMENT_RE.match(line):
                continue
            entry = f"{rel}:{line}"
            if DIRECTIVE_RE.search(entry):
                continue
            hits.append(re.sub(r"\s+", " ", entry).strip())
    return sorted(hits)


def docstring_lines(paths: list[str]) -> list[str]:
    """`경로:docstring 줄` 의 정규화·정렬 목록. 줄 주석 표면과 같은 구성이다.

    빈 줄은 세지 않는다 — 문단 사이 여백은 판정 대상이 아니고, 그것까지 지문에 넣으면
    문단을 옮기기만 해도 재판정 대상이 된다. 기계가 읽는 선언(`검증 AC:` 와 `run_all.py`
    의 나머지 필드)도 제외한다. 파싱에 실패하는 `.py` 는 인구조사를 통째로 못 믿게
    만드므로 조용히 건너뛰지 않고 멈춘다.
    """
    hits: list[str] = []
    for rel in sorted(p for p in paths if p.endswith(".py")):
        try:
            text = (REPO_ROOT / rel).read_text(encoding="utf-8")
        except (UnicodeDecodeError, FileNotFoundError):
            continue
        try:
            tree = ast.parse(text)
        except SyntaxError as exc:
            raise SystemExit(f"{rel}: 파싱 실패로 docstring 표면을 잴 수 없다 — {exc}")
        holders = [tree] + [
            node
            for node in ast.walk(tree)
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef))
        ]
        for holder in holders:
            doc = ast.get_docstring(holder, clean=False)
            if not doc:
                continue
            for line in doc.splitlines():
                if not line.strip():
                    continue
                entry = f"{rel}:{line}"
                if DIRECTIVE_RE.search(entry) or DOCSTRING_DECL_RE.match(line.strip()):
                    continue
                hits.append(re.sub(r"\s+", " ", entry).strip())
    return sorted(hits)


def fingerprint(hits: list[str]) -> str:
    return hashlib.sha256("\n".join(hits).encode("utf-8")).hexdigest()


def marked_block(text: str, open_marker: str, close_marker: str) -> str:
    """마커 사이 본문. 정규식 끝 앵커(`$`)로 뜯지 않는다 — 멀티라인에서 줄 끝에도 붙어
    표가 조용히 0행으로 파싱되고, 0행은 '위반 0' 으로 보여 초록으로 새어 나간다."""
    if open_marker not in text or close_marker not in text:
        raise SystemExit(f"{POLICY.name}: 마커({open_marker} … )를 찾지 못했다.")
    return text.split(open_marker, 1)[1].split(close_marker, 1)[0]


def parse_ledger(text: str, open_marker: str, close_marker: str) -> list[dict]:
    """판정 이력 표를 행 목록으로. 헤더 행과 구분선은 버린다."""
    rows: list[dict] = []
    lines = [ln.strip() for ln in marked_block(text, open_marker, close_marker).splitlines()]
    table = [ln for ln in lines if ln.startswith("|") and ln.endswith("|")]
    for index, line in enumerate(table):
        cells = [c.strip() for c in line.strip("|").split("|")]
        if all(set(c) <= set("-: ") and c for c in cells):
            continue  # 구분선
        if index + 1 < len(table):
            nxt = [c.strip() for c in table[index + 1].strip("|").split("|")]
            if nxt and all(set(c) <= set("-: ") and c for c in nxt):
                continue  # 구분선 바로 앞 = 헤더
        if len(cells) != 5:
            raise SystemExit(f"{POLICY.name}: 판정 이력 행의 열 수가 5가 아니다 -> {line}")
        count = cells[2].strip("`")
        if not count.isdigit():
            raise SystemExit(f"{POLICY.name}: 주석 줄 수가 정수가 아니다 ({count!r}).")
        rows.append(
            {
                # 같은 날 두 범위를 판정하는 일이 흔하므로 날짜만으로는 행을 못 가리킨다.
                "date": f"{cells[0]}(#{len(rows) + 1})",
                "paths": [m.group(1) for m in BACKTICKED_RE.finditer(cells[1])],
                "lines": int(count),
                "fingerprint": cells[3].strip("`"),
                "row": line,
            }
        )
    if not rows:
        raise SystemExit(f"{POLICY.name}: 판정 이력에서 행을 하나도 읽지 못했다(파싱 실패).")
    return rows


def parse_total(text: str, open_marker: str, close_marker: str) -> int:
    raw = marked_block(text, open_marker, close_marker).strip()
    if not raw.isdigit():
        raise SystemExit(f"{POLICY.name}: {open_marker} 의 값이 정수가 아니다 ({raw!r}).")
    return int(raw)


def census(hits: list[str]) -> str:
    files = sorted({h.split(":", 1)[0] for h in hits})
    buckets = {"Go 소스": 0, "Go 테스트": 0, "YAML": 0, "Python": 0, "기타": 0}
    for hit in hits:
        path = hit.split(":", 1)[0]
        if path.endswith("_test.go"):
            buckets["Go 테스트"] += 1
        elif path.endswith(".go"):
            buckets["Go 소스"] += 1
        elif path.endswith((".yaml", ".yml")):
            buckets["YAML"] += 1
        elif path.endswith(".py"):
            buckets["Python"] += 1
        else:
            buckets["기타"] += 1
    breakdown = " · ".join(f"{k} {v}" for k, v in buckets.items() if v)
    return f"판정 대상 주석 {len(hits)}줄 / {len(files)}파일 ({breakdown})"


def docstring_census(hits: list[str]) -> str:
    files = sorted({h.split(":", 1)[0] for h in hits})
    return f"판정 대상 docstring {len(hits)}줄 / {len(files)}파일"


def check_ledger(
    rows: list[dict],
    in_scope: set[str],
    extract,
    rules: tuple[str, str, str],
    only_python: bool = False,
) -> int:
    """한 표면의 원장을 R1~R3(줄 주석) / R5~R7(docstring) 로 검사하고 행 합을 돌려준다."""
    existence, remeasure, duplicate = rules
    for row in rows:
        if not row["paths"]:
            fail(existence, f"{row['date']} 행에 범위 파일이 선언돼 있지 않다(빈 범위는 등재가 아니다).")
        for rel in row["paths"]:
            if rel not in in_scope:
                fail(
                    existence,
                    f"{row['date']} 행의 `{rel}` 이 정책 스캔 범위에 없다"
                    f" (범위: {' · '.join(SCAN_PATHSPECS)}). 파일이 사라졌거나 이름이"
                    " 바뀌었으면 그 범위는 다시 판정받아야 한다.",
                )
            elif only_python and not rel.endswith(".py"):
                fail(
                    existence,
                    f"{row['date']} 행의 `{rel}` 은 `.py` 가 아니다 —"
                    " docstring 표면은 Python 파일만 잰다.",
                )

    seen: dict[str, str] = {}
    for row in rows:
        for rel in row["paths"]:
            if rel in seen:
                fail(duplicate, f"`{rel}` 이 {seen[rel]} 행과 {row['date']} 행에 모두 등재돼 있다.")
            else:
                seen[rel] = row["date"]

    for row in rows:
        present = [rel for rel in row["paths"] if rel in in_scope]
        hits = extract(present)
        actual_fingerprint = fingerprint(hits)[:FINGERPRINT_LEN]
        if len(hits) != row["lines"]:
            fail(
                remeasure,
                f"{row['date']} 행의 줄 수가 등재 {row['lines']} != 실측 {len(hits)}."
                " 등재 이후 이 범위가 바뀌었다 — 정책의 판정 절차로 다시 판정하고"
                " 같은 PR 에서 이 행을 갱신할 것.",
            )
        if actual_fingerprint != row["fingerprint"]:
            fail(
                remeasure,
                f"{row['date']} 행의 지문이 등재 `{row['fingerprint']}` !="
                f" 실측 `{actual_fingerprint}`. 줄 수가 같아도 내용이 바뀌면 재판정 대상이다.",
            )
    return sum(row["lines"] for row in rows)


def main() -> int:
    if not POLICY.exists():
        print(f"[R1] 정책 SSOT 가 없다: {POLICY.relative_to(REPO_ROOT)}", file=sys.stderr)
        return 1

    text = POLICY.read_text(encoding="utf-8")
    rows = parse_ledger(text, LEDGER_OPEN, LEDGER_CLOSE)
    total = parse_total(text, TOTAL_OPEN, TOTAL_CLOSE)
    doc_rows = parse_ledger(text, DOC_LEDGER_OPEN, DOC_LEDGER_CLOSE)
    doc_total = parse_total(text, DOC_TOTAL_OPEN, DOC_TOTAL_CLOSE)
    doc_remaining = parse_total(text, DOC_REMAINING_OPEN, DOC_REMAINING_CLOSE)

    in_scope = set(scan_files())

    # R1·R2·R3 — 줄 주석 표면
    ledger_sum = check_ledger(rows, in_scope, comment_lines, ("R1", "R2", "R3"))

    # R4 — 합계 미러(양방향)
    if total != ledger_sum:
        fail(
            "R4",
            f"판정 합계 {total} != 원장 행 줄 수 합 {ledger_sum}."
            " 범위를 더하거나 빼면 같은 PR 에서 합계도 움직여야 한다.",
        )

    # R5·R6·R7 — docstring 표면
    doc_sum = check_ledger(
        doc_rows, in_scope, docstring_lines, ("R5", "R6", "R7"), only_python=True
    )

    hits = comment_lines(sorted(in_scope))
    doc_hits = docstring_lines(sorted(in_scope))

    # R8 — docstring 합계 미러 + 미판정 잔량
    if doc_total != doc_sum:
        fail(
            "R8",
            f"docstring 합계 {doc_total} != docstring 원장 행 줄 수 합 {doc_sum}."
            " 범위를 더하거나 빼면 같은 PR 에서 합계도 움직여야 한다.",
        )
    if doc_total + doc_remaining != len(doc_hits):
        fail(
            "R8",
            f"docstring 합계 {doc_total} + 잔량 {doc_remaining} != 실측 전체"
            f" {len(doc_hits)}. docstring 이 늘거나 줄면 같은 PR 에서 잔량도 움직여야"
            " 한다 — 잔량 갱신은 「이만큼은 아직 판정받지 않았다」는 선언이다.",
        )

    if failures:
        for line in failures:
            print(line, file=sys.stderr)
        print(
            f"\nFAIL: {len(failures)}건 — {census(hits)} · {docstring_census(doc_hits)}",
            file=sys.stderr,
        )
        return 1

    share = (ledger_sum * 100.0 / len(hits)) if hits else 0.0
    doc_share = (doc_sum * 100.0 / len(doc_hits)) if doc_hits else 0.0
    print(
        f"OK: 규칙 R1~R8 위반 없음 — {census(hits)}"
        f" · 판정 완료 {ledger_sum}줄({share:.1f}%) / 등재 범위 {len(rows)}"
    )
    for row in rows:
        print(
            f"  {row['date']}  {row['lines']:>4}줄  {row['fingerprint']}"
            f"  {' '.join(row['paths'])}"
        )
    print(f"  전체 지문: {fingerprint(hits)}")
    print(
        f"     {docstring_census(doc_hits)}"
        f" · 판정 완료 {doc_sum}줄({doc_share:.1f}%) / 등재 범위 {len(doc_rows)}"
        f" · 미판정 잔량 {doc_remaining}줄"
    )
    for row in doc_rows:
        print(
            f"  {row['date']}  {row['lines']:>4}줄  {row['fingerprint']}"
            f"  {' '.join(row['paths'])}"
        )
    print(f"  전체 docstring 지문: {fingerprint(doc_hits)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
