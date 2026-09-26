#!/usr/bin/env python3
"""E2E 모킹 지점 대조와 차단 원장 완비 체커.

R7 의 계기: 상한을 `5`→`4` 로 내린 PR 이 같은 문서의 개수 산문 네 자리 중 하나만 고쳤고,
나머지 셋은 거짓이 된 채 네 게이트를 전부 통과했다.
"""

from __future__ import annotations

import datetime
import pathlib
import re
import subprocess
import sys

HERE = pathlib.Path(__file__).resolve().parent
REPO_ROOT = HERE.parent
POLICY = REPO_ROOT / "docs" / "e2e-mocking-policy.md"

SCAN_PATHSPECS = ("tests", ".github/workflows")
CATEGORIES = ("UPS", "IMG", "GATE")
NO_VALUE = "—"

LEDGER_OPEN = "<!-- mock-exception-원장 -->"
LEDGER_CLOSE = "<!-- /mock-exception-원장 -->"
CAP_OPEN = "<!-- mock-exception-상한 -->"
CAP_CLOSE = "<!-- /mock-exception-상한 -->"
BLOCKERS_OPEN = "<!-- mock-blocker-원장 -->"
BLOCKERS_CLOSE = "<!-- /mock-blocker-원장 -->"
BLOCKER_COLUMNS = ("ID", "출처", "등록일", "해소 방향", "선행", "재검토")
MODEL_PATTERN = r"tbm_[a-z0-9][a-z0-9_-]*"
TASK_PATTERN = MODEL_PATTERN + r"/rct_[a-z0-9][a-z0-9_-]*"

# 모킹 지점을 가리키는 토큰. 모델 as-is 지문의 패턴에서 주석 대안만 뺀 것이다
# (주석은 ANNOTATION_RE 로 따로 읽는다).
#
# 끝의 `(?![A-Za-z0-9_])` 가 없으면 `e2e-mocking-policy.md` 라는 **정책 문서 경로 자체**가
# `e2e-mock` 으로 잡혀, 등재를 가리키는 주석이 미등재 모킹으로 보고된다(R2 오탐). 하이픈은
# 일부러 배제 목록에서 뺐다 — `github-mock-script` 같은 파생 이름에서는 `github-mock` 을
# 계속 잡아야 하기 때문이다.
ID_TOKEN_RE = re.compile(
    r"(?:[A-Za-z0-9_-]*-mock|dear-baby-fixture|MCP_AUTH_DISABLED|DISABLE_SECURITY_PLUGIN)"
    r"(?![A-Za-z0-9_])"
)
ANNOTATION_RE = re.compile(r"#\s*mock-exception:\s*([A-Za-z0-9_-]+)")
BACKTICKED_RE = re.compile(r"`([^`]+)`")

failures: list[str] = []


def fail(rule: str, message: str) -> None:
    failures.append(f"[{rule}] {message}")


def scan_files() -> list[pathlib.Path]:
    """모델 as-is 지문과 동일한 스캔 범위."""
    out = subprocess.run(
        ["git", "ls-files", "--", *SCAN_PATHSPECS],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        check=True,
    ).stdout
    paths = [
        REPO_ROOT / line
        for line in out.splitlines()
        if line and not line.endswith("_test.go")
    ]
    if not paths:
        raise SystemExit(
            "스캔 범위가 비어 있다 — git ls-files 가 tests/·.github/workflows/ 에서 "
            "아무 파일도 돌려주지 않았다. 레포 루트에서 실행 중인지 확인할 것."
        )
    return sorted(paths)


def ledger_block(text: str) -> str:
    """마커 사이 본문. 정규식 끝 앵커(`$`)를 쓰지 않는다 — 멀티라인에서 줄 끝에도 붙어
    표가 조용히 0행으로 파싱되고, 0행은 '위반 0' 으로 보여 초록으로 새어 나간다."""
    if LEDGER_OPEN not in text or LEDGER_CLOSE not in text:
        raise SystemExit(f"{POLICY.name}: 원장 마커({LEDGER_OPEN} … )를 찾지 못했다.")
    return text.split(LEDGER_OPEN, 1)[1].split(LEDGER_CLOSE, 1)[0]


def parse_ledger(text: str) -> list[dict]:
    """허용목록 표를 행 목록으로. 헤더 행과 구분선은 버린다."""
    rows: list[dict] = []
    lines = [ln.strip() for ln in ledger_block(text).splitlines()]
    table = [ln for ln in lines if ln.startswith("|") and ln.endswith("|")]
    for index, line in enumerate(table):
        cells = [c.strip() for c in line.strip("|").split("|")]
        if all(set(c) <= set("-: ") and c for c in cells):
            continue
        if index + 1 < len(table):
            nxt = [c.strip() for c in table[index + 1].strip("|").split("|")]
            if nxt and all(set(c) <= set("-: ") and c for c in nxt):
                continue
        if len(cells) != 4:
            raise SystemExit(f"{POLICY.name}: 허용목록 행의 열 수가 4가 아니다 -> {line}")
        rows.append(
            {
                "id": cells[0].strip("`"),
                "category": cells[1].strip("`"),
                "sites": [m.group(1) for m in BACKTICKED_RE.finditer(cells[2])],
                "alternative": [m.group(1) for m in BACKTICKED_RE.finditer(cells[3])],
                "alternative_raw": cells[3],
            }
        )
    if not rows:
        raise SystemExit(f"{POLICY.name}: 허용목록에서 행을 하나도 읽지 못했다(파싱 실패).")
    return rows


def parse_cap(text: str) -> int:
    if CAP_OPEN not in text or CAP_CLOSE not in text:
        raise SystemExit(f"{POLICY.name}: 상한 마커({CAP_OPEN} … )를 찾지 못했다.")
    raw = text.split(CAP_OPEN, 1)[1].split(CAP_CLOSE, 1)[0].strip()
    if not raw.isdigit():
        raise SystemExit(f"{POLICY.name}: 등재 상한이 정수가 아니다 ({raw!r}).")
    return int(raw)


def next_content_line(lines: list[str], index: int) -> str | None:
    """`index` 다음의 첫 비어 있지 않은 줄."""
    for line in lines[index + 1 :]:
        if line.strip():
            return line
    return None


COUNT_WORD_RE = re.compile(r"등재|상한|허용목록")
COUNTED_RE = re.compile(r"(?<![0-9A-Za-z_#.\-/])[0-9]{1,3}\s*[행건개](?![0-9A-Za-z_])")
BARE_COUNT_RE = re.compile(
    r"^[^.()`·,/\n]{0,12}?(?<![0-9A-Za-z_#.\-/])[0-9]{1,3}(?![0-9A-Za-z_.\-/])"
)
COUNT_WINDOW = 40


def prose_outside_markers(text: str) -> str:
    """마커 블록(허용목록 표·상한 값·차단 원장)을 도려낸 나머지 산문.

    도려내는 것이지 잘라내는 것이 아니다 — 블록을 같은 길이의 공백으로 덮어 줄 번호와
    문장 경계를 보존한다. 잘라내면 표의 마지막 낱말과 다음 문단의 숫자가 한 창 안에
    들어와 없는 위반이 생기고, 보고하는 줄 번호도 원문과 어긋난다.
    """
    out = list(text)
    for open_marker, close_marker in (
        (LEDGER_OPEN, LEDGER_CLOSE),
        (CAP_OPEN, CAP_CLOSE),
        (BLOCKERS_OPEN, BLOCKERS_CLOSE),
    ):
        start = text.find(open_marker)
        end = text.find(close_marker)
        if start == -1 or end == -1 or end < start:
            continue
        for index in range(start, end + len(close_marker)):
            if out[index] != "\n":
                out[index] = " "
    return "".join(out)


def find_prose_counts(text: str) -> list[tuple[int, str]]:
    """산문이 등재 수·상한을 숫자로 말하는 자리를 (줄 번호, 발췌)로 모은다.

    두 정규식으로 가른다. 세는 말이 붙은 수(`5행`·`4건`)는 창 안 어디에 있든 개수
    주장이다(`COUNTED_RE`). 세는 말이 없는 맨 수는 개수 주장일 수도 아닐 수도 있어
    (`등재 5`·`상한은 그대로 5다` ↔ `불변식 1`·`위 2번`·`MCP_AUTH_DISABLED=1`),
    거리와 문장 경계로 가른다(`BARE_COUNT_RE`) — 개수 주장은 세는 말 바로 뒤에
    조사·부사만 끼고 붙지, 괄호·마침표·역따옴표를 건너뛰지 않는다.
    """
    prose = prose_outside_markers(text)
    hits = []
    for word in COUNT_WORD_RE.finditer(prose):
        window = prose[word.end() : word.end() + COUNT_WINDOW].split("\n\n")[0]
        number = COUNTED_RE.search(window) or BARE_COUNT_RE.match(window)
        if not number:
            continue
        line = prose.count("\n", 0, word.start()) + 1
        hits.append((line, " ".join((word.group() + window[: number.end()]).split())))
    return hits


def parse_blockers(text: str) -> list[dict[str, str]]:
    if text.count(BLOCKERS_OPEN) != 1 or text.count(BLOCKERS_CLOSE) != 1:
        raise ValueError("차단 원장 마커는 시작·끝 각각 하나가 필요하다.")
    start = text.index(BLOCKERS_OPEN) + len(BLOCKERS_OPEN)
    end = text.index(BLOCKERS_CLOSE)
    if end < start:
        raise ValueError("차단 원장 마커 순서가 뒤집혔다.")
    lines = [line.strip() for line in text[start:end].splitlines() if line.strip()]
    table = []
    for line in lines:
        if not line.startswith("|") or not line.endswith("|"):
            raise ValueError("차단 원장 마커 안에는 표만 둘 수 있다.")
        cells = [cell.strip().strip("`").strip() for cell in line[1:-1].split("|")]
        if len(cells) != len(BLOCKER_COLUMNS):
            raise ValueError(f"차단 원장 행은 {len(BLOCKER_COLUMNS)}개 열이어야 한다.")
        table.append(cells)
    if len(table) < 2 or tuple(table[0]) != BLOCKER_COLUMNS:
        raise ValueError("차단 원장 헤더가 없거나 열 이름·순서가 다르다.")
    if not all(re.fullmatch(r":?-{3,}:?", cell) for cell in table[1]):
        raise ValueError("차단 원장 헤더 구분선이 올바르지 않다.")

    rows = []
    seen = set()
    for cells in table[2:]:
        row = dict(zip(BLOCKER_COLUMNS, cells))
        identifier = row["ID"]
        if not re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", identifier) or identifier in seen:
            raise ValueError(f"차단 ID가 잘못됐거나 중복됐다: {identifier!r}")
        seen.add(identifier)
        if any(cell.casefold() in {"", "—", "-", "tbd", "todo", "미정"} for cell in cells):
            raise ValueError(f"{identifier}: 빈 계획 셀 또는 미정 값이 있다.")
        if not re.fullmatch(TASK_PATTERN + r"|policy:#[a-z0-9-]+", row["출처"]):
            raise ValueError(f"{identifier}: 출처는 모델/task ID 또는 policy:#앵커여야 한다.")
        if row["해소 방향"] not in {"real-environment", "exception-registration"}:
            raise ValueError(f"{identifier}: 해소 방향이 허용 값이 아니다.")
        try:
            registered = datetime.date.fromisoformat(row["등록일"])
        except ValueError as exc:
            raise ValueError(f"{identifier}: 등록일이 ISO 날짜가 아니다.") from exc
        if registered.isoformat() != row["등록일"]:
            raise ValueError(f"{identifier}: 등록일은 YYYY-MM-DD여야 한다.")
        review = row["재검토"]
        if re.fullmatch(r"\d{4}-\d{2}-\d{2}", review):
            try:
                deadline = datetime.date.fromisoformat(review)
            except ValueError as exc:
                raise ValueError(f"{identifier}: 재검토 날짜가 유효하지 않다.") from exc
            if not 0 <= (deadline - registered).days <= 90:
                raise ValueError(f"{identifier}: 재검토 날짜는 등록일부터 90일 안이어야 한다.")
        elif review.startswith("file:"):
            path = review.removeprefix("file:")
            parts = path.split("/")
            if not path or any(part in {"", ".", ".."} for part in parts) or re.search(r"\s|[\\:#]", path):
                raise ValueError(f"{identifier}: file 사건은 레포 상대 경로여야 한다.")
        elif not re.fullmatch(r"task:" + TASK_PATTERN + r"|pr:[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+#[1-9][0-9]*", review):
            raise ValueError(f"{identifier}: 재검토는 ISO 날짜 또는 file:/task:/pr: 사건이어야 한다.")
        rows.append(row)
    return rows


def main() -> int:
    if not POLICY.exists():
        print(f"[R3] 정책 SSOT 가 없다: {POLICY.relative_to(REPO_ROOT)}", file=sys.stderr)
        return 1

    text = POLICY.read_text(encoding="utf-8")
    rows = parse_ledger(text)
    cap = parse_cap(text)
    blockers = []
    try:
        blockers = parse_blockers(text)
    except ValueError as exc:
        fail("B2", str(exc))

    by_id: dict[str, dict] = {}
    for row in rows:
        if row["id"] in by_id:
            fail("R3", f"허용목록에 `{row['id']}` 행이 두 번 있다.")
        by_id[row["id"]] = row

    # R1 — 카테고리 유효성
    for row in rows:
        if row["category"] not in CATEGORIES:
            fail(
                "R1",
                f"`{row['id']}` 의 카테고리 `{row['category']}` 가 허용 코드"
                f"({'·'.join(CATEGORIES)})가 아니다.",
            )

    # R5 — GATE 행의 대체 검증
    for row in rows:
        if row["category"] != "GATE":
            continue
        if not row["alternative"]:
            fail(
                "R5",
                f"`{row['id']}` 는 GATE 인데 대체 검증 산출물이 선언돼 있지 않다"
                f" (현재 {row['alternative_raw']!r}). 게이트 완화 예외는 그 계층이 가리는"
                " 성질을 되찾는 수단과 함께만 존재할 수 있다.",
            )
        for path in row["alternative"]:
            if not (REPO_ROOT / path).exists():
                fail("R5", f"`{row['id']}` 의 대체 검증 산출물 `{path}` 가 실재하지 않는다.")

    # R6 — 상한 래칫(양방향)
    if cap != len(rows):
        fail(
            "R6",
            f"등재 상한 {cap} != 허용목록 행 수 {len(rows)}."
            " 예외를 늘리려면 같은 PR 에서 상한을 올리고, 예외가 사라지면 같이 내릴 것.",
        )

    for line_number, excerpt in find_prose_counts(text):
        fail(
            "R7",
            f"{POLICY.relative_to(REPO_ROOT)}:{line_number} 산문이 개수를 말한다:"
            f" {excerpt!r}. 개수를 말하는 자리는 허용목록 표와 상한 마커 두 곳뿐이다"
            " — 보관 기록은 절대 수치 대신 델타로 적을 것.",
        )

    # 스캔 — 실재하는 모킹 토큰과 표기 주석
    found_ids: dict[str, set[str]] = {}
    annotations: list[tuple[str, int, str, str | None]] = []
    for path in scan_files():
        rel = path.relative_to(REPO_ROOT).as_posix()
        try:
            lines = path.read_text(encoding="utf-8").splitlines()
        except UnicodeDecodeError:
            continue
        for number, line in enumerate(lines):
            for match in ID_TOKEN_RE.finditer(line):
                found_ids.setdefault(match.group(0), set()).add(rel)
            annotation = ANNOTATION_RE.search(line)
            if annotation:
                annotations.append(
                    (rel, number + 1, annotation.group(1), next_content_line(lines, number))
                )

    # R2 — 미등재 모킹
    for token in sorted(found_ids):
        if token not in by_id:
            where = ", ".join(sorted(found_ids[token])[:3])
            fail(
                "R2",
                f"미등재 모킹 토큰 `{token}` 이 코드에 있다 ({where})."
                f" {POLICY.name} 허용목록에 등재하거나 모킹을 제거할 것.",
            )

    # R3 — 고아 등재 · 지점 실재
    for row in rows:
        if row["id"] not in found_ids:
            fail(
                "R3",
                f"고아 등재: `{row['id']}` 가 허용목록에 있으나 스캔 범위의 코드에 없다."
                " 허용목록에서 지우고 상한도 내릴 것.",
            )
        if not row["sites"]:
            fail("R3", f"`{row['id']}` 에 모킹 지점 파일이 선언돼 있지 않다.")
        for site in row["sites"]:
            if site not in found_ids.get(row["id"], set()):
                fail(
                    "R3",
                    f"`{row['id']}` 의 모킹 지점 `{site}` 이 존재하지 않거나 그 ID 를"
                    " 담고 있지 않다.",
                )

    # R4 — 표기 주석 (등재 -> 주석)
    annotated: set[tuple[str, str]] = set()
    for rel, number, code, following in annotations:
        if code not in CATEGORIES:
            fail("R4", f"{rel}:{number} 주석의 코드 `{code}` 가 허용 코드가 아니다.")
            continue
        if following is None:
            fail("R4", f"{rel}:{number} 주석 뒤에 모킹 지점 줄이 없다.")
            continue
        ids = [t for t in ID_TOKEN_RE.findall(following) if t in by_id]
        if not ids:
            fail(
                "R4",
                f"{rel}:{number} 주석의 다음 줄에 등재된 ID 가 없다."
                " 주석은 그 ID 토큰이 나타나는 줄 바로 앞에 둘 것.",
            )
            continue
        for token in ids:
            if by_id[token]["category"] != code:
                fail(
                    "R4",
                    f"{rel}:{number} 주석의 코드 `{code}` 가 `{token}` 의 등재 카테고리"
                    f" `{by_id[token]['category']}` 와 다르다.",
                )
            annotated.add((token, rel))

    for row in rows:
        for site in row["sites"]:
            if (row["id"], site) not in annotated:
                fail(
                    "R4",
                    f"`{row['id']}` 의 모킹 지점 `{site}` 에"
                    f" `# mock-exception: {row['category']} — …` 주석이 없다.",
                )

    site_count = sum(len(row["sites"]) for row in rows)
    if failures:
        for line in failures:
            print(line, file=sys.stderr)
        print(
            f"\nFAIL: {len(failures)}건 — 등재 {len(rows)} · 지점 {site_count} ·"
            f" 실재 토큰 {len(found_ids)} · 주석 {len(annotations)}",
            file=sys.stderr,
        )
        return 1

    print(
        f"OK: 규칙 R1~R7 위반 없음 — 등재 {len(rows)}(상한 {cap}) · 모킹 지점 {site_count} ·"
        f" 실재 토큰 {len(found_ids)} · 표기 주석 {len(annotations)} · 미등재 0 · 고아 0"
    )
    for row in rows:
        alternative = ", ".join(row["alternative"]) or NO_VALUE
        print(f"  {row['category']:<4} {row['id']:<24} 대체 검증: {alternative}")
    print(f"OK: B2 차단 원장 {len(blockers)}행 완비 (등재 상한과 별도; B1·B3는 reconciler 판정)")
    for row in blockers:
        print(f"  {row['ID']}: 선행={row['선행']} · 재검토={row['재검토']}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
