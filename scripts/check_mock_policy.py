#!/usr/bin/env python3
"""E2E 모킹 정책 ↔ 실제 모킹 지점 양방향 1:1 체커.

정합성 모델 `tbm_homelab-k3s-mcp-e2e-mock-policy`는 **허용목록에 등재된 모킹 지점의 집합 ==
코드에 실재하는 모킹 지점의 집합**을 요구한다. 이 스크립트는 그 판정을 사람의 자기신고가
아니라 **레포의 실제 상태에서 재도출**해 `docs/e2e-mocking-policy.md`의 허용목록과 대조한다.
클러스터도 서드파티 의존성도 필요 없다(표준 라이브러리 전용) — CI의 lint 잡에서 돈다.

스캔 범위는 `git ls-files -- tests .github/workflows` 에서 `*_test.go` 를 뺀 것으로, 모델의
as-is 지문 범위와 정확히 같다. `internal/` 의 Go 단위 테스트(httptest·fakes)는 그 층위에서
모킹이 정상이므로 범위 밖이다.

판정하는 것:

* **R1** 허용목록의 카테고리는 `UPS`·`IMG`·`GATE` 중 하나다.
* **R2** 스캔 범위에서 발견된 모킹 토큰이 전부 허용목록의 ID다(미등재 = 실패).
* **R3** 등재된 ID가 코드에 실재하고, 선언한 모킹 지점 파일이 존재하며 그 ID 를 담는다
  (고아 등재 = 실패).
* **R4** 모킹 지점마다 `# mock-exception: <CODE>` 주석이 있고 **다음 비어 있지 않은 줄**이
  그 ID 를 담으며 `<CODE>` 가 행의 카테고리와 같다. 역으로 스캔 범위의 모든
  `mock-exception:` 주석도 같은 조건을 만족해야 한다(주석만 있고 미등재인 상태를 막는다).
* **R5** `GATE` 행은 대체 검증 산출물 경로를 선언하고 그 경로가 실재한다. 게이트를 완화한
  예외는 그 계층이 가리는 성질을 되찾는 수단과 **함께만** 존재할 수 있다.
* **R6** 등재 상한 == 허용목록 행 수(양방향). 예외를 늘리려면 같은 PR 에서 상한을 명시적으로
  올려야 하고, 예외가 사라지면 같이 내려야 한다.

허용목록이 실측과 다르면 실패하므로, 모킹을 더하거나 지운 PR 은 **같은 PR 에서** 정책 문서를
갱신해야 한다. 그것이 이 모델이 요구하는 양방향 1:1 이다.
"""

from __future__ import annotations

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
            continue  # 구분선
        if index + 1 < len(table):
            nxt = [c.strip() for c in table[index + 1].strip("|").split("|")]
            if nxt and all(set(c) <= set("-: ") and c for c in nxt):
                continue  # 구분선 바로 앞 = 헤더
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


def main() -> int:
    if not POLICY.exists():
        print(f"[R3] 정책 SSOT 가 없다: {POLICY.relative_to(REPO_ROOT)}", file=sys.stderr)
        return 1

    text = POLICY.read_text(encoding="utf-8")
    rows = parse_ledger(text)
    cap = parse_cap(text)

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
        f"OK: 규칙 R1~R6 위반 없음 — 등재 {len(rows)}(상한 {cap}) · 모킹 지점 {site_count} ·"
        f" 실재 토큰 {len(found_ids)} · 표기 주석 {len(annotations)} · 미등재 0 · 고아 0"
    )
    for row in rows:
        alternative = ", ".join(row["alternative"]) or NO_VALUE
        print(f"  {row['category']:<4} {row['id']:<24} 대체 검증: {alternative}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
