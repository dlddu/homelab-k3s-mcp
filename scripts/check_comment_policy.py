#!/usr/bin/env python3
"""주석 비중복성 판정 원장 ↔ 실제 주석 상태 대조 체커.

정합성 모델 `tbm_homelab-k3s-mcp-comment-redundancy`의 to-be 는 `docs/comment-policy/`
디렉터리다. 그래서 **그 문서가 자기 자신에 대해 적은 수치가 낡으면 to-be 가 틀린 것**이 되는데,
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
* **R4** **범위 불변식** — 주석 줄을 가진 실측 파일은 **정확히 한 행**에 속한다(전수). 어느 행에도
  없는 파일이 주석을 들고 들어오면 여기서 멈춘다. R1~R3 은 **등재된 행의 범위만** 재측정하므로
  어느 행에도 없는 파일은 원리적으로 검사 대상이 아니고, 승인 게이트 슬라이스가 새 파일 넷으로
  주석 192 줄을 들고 들어왔을 때 이 게이트는 rc=0 이었다. 그 자리를 이 규칙이 메운다 —
  손으로 적은 잔량 마커가 아니라 **행의 존재 자체**로. 등재됐는데 지금 주석이 0줄인 파일은
  통과시킨다: 판정해서 비운 사실의 기록이라 행에서 지우면 그 이력이 사라진다.

docstring 표면 — `ast` 로 뜯은 module·class·function docstring 의 본문(빈 줄과 기계가 읽는
선언 줄은 제외). 모델 as-is 는 **아직 이 표면을 보지 않는다**(bash 스크립트가 파서를 돌리지
않는다) — 그래서 재감지가 늘어남을 알려주지 못하고, 그 자리를 R8 이 메운다:

* **R5** docstring 원장 행의 범위가 실재하는 `.py` 이고 스캔 범위 안이다.
* **R6** 각 행의 재측정 줄 수·지문이 등재값과 같다(R2 와 같은 성질).
* **R7** docstring 행 사이 이중 등재 금지(R3 과 같은 성질).
* **R8** docstring 표면의 **범위 불변식**(R4 와 같은 성질·같은 함수). 새 파일이 docstring 을 들고
  들어오면 그 파일의 행이 없어 CI 가 멈춘다. 행을 더하며 판정 축을 `—` 로 두는 행위가 곧
  **「이만큼은 아직 판정받지 않았다」는 명시적 선언**이다 — 사각지대가 조용히 커지는 성질을
  없애는 것이 이 규칙의 목적이고, 그 선언의 자리가 공유 마커에서 **자기 파일의 행**으로 옮겨졌다.

위 여덟은 **등재 범위가 변했는지**만 재고 그 판정이 **무엇을 물었는지**는 보지 않는다.
그 자리는 원장의 `판정 축` 칸이 담고, R9 가 그 칸의 표기를 강제한다:

* **R9** 모든 행의 `판정 축` 칸이 `—` 이거나 `①②③④` 의 **정규 순서 부분열**이다(`①②`·`③④`·
  `①②③④` 등). 빈 칸도, 순서가 뒤집힌 표기도, 정의에 없는 기호도 받지 않는다 — 진척을 세는
  유일한 자리라 표기가 흔들리면 집계가 조용히 갈린다.

  **왜 칸으로 세는가.** 2026-09-26 이전까지 이 자리는 결과 칸의 산문(`복원경로 ③:` 토큰 +
  지문 앵커)과 손으로 적은 축별 잔량 마커 넷이었다. 그 형태는 두 가지를 낳았다 — ⑴ 모든 PR 이
  같은 마커 줄을 고쳐 **파일 집합이 서로소인 PR 끼리도 반드시 충돌**했고, 그 충돌을 피하려고
  판정 슬라이스가 한두 행으로 쪼그라들었다. ⑵ 진척이 산문에 있어 기계가 읽으려면 토큰 매칭에
  기대야 했고, 토큰은 재판정으로 낡은 판정을 계속 「완료」로 인증했다. 칸은 둘 다 없앤다 —
  행마다 갈라져 있어 충돌하지 않고, 값이 곧 진척이다.

  **미판정(`—`)은 실패가 아니다.** 판정하지 않았다는 사실을 행에 남기는 것은 정상이고, 그 행은
  다음 판정 슬라이스가 가져간다. 이 게이트가 막는 것은 *묻지 않은 것이 조용히 착지하는 것*
  하나이지 「아직 묻지 않았다」가 아니다.

* **R10** 원장에는 **표와 「읽는 법」 절만** 둔다. 경위·병합 재측정·인계 문단은 그 패스의
  `passes/` 파일로 간다. 그 산문이 원장에 쌓이면 모든 PR 이 같은 파일의 같은 구역을 고치고,
  무엇보다 **판정 결과와 그때그때의 서사가 한 파일에서 섞여** 어느 쪽이 사실인지 흐려진다.

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
POLICY_DIR = REPO_ROOT / "docs" / "comment-policy"
POLICY_README = POLICY_DIR / "README.md"
LEDGER = POLICY_DIR / "ledger.md"

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
# ⚠️ `검증 시나리오:` 는 여기 있고 DIRECTIVE_RE 에 있지 않다. 2026-09-08 에 e2e 1:1 판정 축이
# AC → 테스트 시나리오로 옮겨지며 그 선언 필드가 개명됐는데, DIRECTIVE_RE 는 모델
# tbm_homelab-k3s-mcp-comment-redundancy 의 as-is 버전 스크립트와 **글자 그대로 같아야 하는
# 넷** 중 하나라 한쪽만 고칠 수 없다. 이 표면(docstring)은 그 모델이 보지 않으므로(위 docstring
# 표면 설명 참조) 체커 국소인 여기에 두는 것이 두 정의를 갈라지지 않게 하는 유일한 자리다.
# DIRECTIVE_RE 의 `검증 AC:` 는 이제 어느 표면에도 매칭되지 않는 죽은 패턴이며, 두 정의를 함께
# 옮기는 정리는 그 모델의 몫이다.
DOCSTRING_DECL_RE = re.compile(r"^(검증 시나리오|실행 대상|추가 인자|실행 순서|병렬 레인):")

# 표는 절 제목으로 찾는다. 경계 주석 마커를 두지 않는 것은 그것도 손으로 유지하는 좌표이기
# 때문이다 — 표가 어느 절에 속하는지는 제목이 이미 말한다.
LEDGER_SECTION = "## 판정 이력"
DOC_LEDGER_SECTION = "## docstring 판정 이력"
READING_SECTION = "## 읽는 법"

# 판정 축 주장의 **근거**(R11). 결과 칸이 그 축을 물었다는 토큰을 담고, 그 판정을 어느 지문에서
# 내렸는지를 앵커로 못박는다. 축 칸이 진실이지만, 그 진실이 손으로 고쳐 쓰이면 묻지 않은 것이
# 「완료」가 된다 — 앵커를 현재 지문에 묶어 두는 것이 재판정으로 낡은 판정을 자동으로 떨어뜨린다.
AXIS_EVIDENCE = {
    "①②": (("복원경로 ①:", "복원경로 ②:"), re.compile(r"복원경로 ①② 판정 @지문 `([0-9a-f]{12})`")),
    "③④": (("복원경로 ③:", "복원경로 ④:"), re.compile(r"복원경로 ③④ 판정 @지문 `([0-9a-f]{12})`")),
}

# 판정 축 칸의 유효 표기: `—`(미판정) 또는 ①②③④ 의 정규 순서 부분열.
AXES = "①②③④"
VALID_AXES = {"—"} | {
    "".join(AXES[i] for i in range(4) if mask >> i & 1)
    for mask in range(1, 16)
}

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


def section_table(text: str, heading: str) -> list[str]:
    """한 절(`## ...`) 안의 첫 표에서 데이터 행만 돌려준다."""
    try:
        start = text.index(heading) + len(heading)
    except ValueError:
        raise SystemExit(f"{LEDGER.name}: 「{heading}」 절을 찾지 못했다.")
    rest = text[start:]
    nxt = rest.find("\n## ")
    body = rest if nxt < 0 else rest[:nxt]
    table = [ln.strip() for ln in body.splitlines() if ln.strip().startswith("|")]
    out = []
    for index, line in enumerate(table):
        cells = [c.strip() for c in line.strip("|").split("|")]
        if all(set(c) <= set("-: ") and c for c in cells):
            continue  # 구분선
        if index + 1 < len(table):
            nxt_cells = [c.strip() for c in table[index + 1].strip("|").split("|")]
            if nxt_cells and all(set(c) <= set("-: ") and c for c in nxt_cells):
                continue  # 구분선 바로 앞 = 헤더
        out.append(line)
    return out


def parse_ledger(text: str, heading: str) -> list[dict]:
    """판정 이력 표를 행 목록으로. 헤더 행과 구분선은 버린다."""
    rows: list[dict] = []
    for line in section_table(text, heading):
        cells = [c.strip() for c in line.strip("|").split("|")]
        if len(cells) != 6:
            raise SystemExit(
                f"{LEDGER.name}: 판정 이력 행의 열 수가 6(판정일·범위·줄 수·지문·판정 축·결과)이"
                f" 아니다 -> {line}"
            )
        count = cells[2].strip("`")
        if not count.isdigit():
            raise SystemExit(f"{LEDGER.name}: 주석 줄 수가 정수가 아니다 ({count!r}).")
        paths = [m.group(1) for m in BACKTICKED_RE.finditer(cells[1])]
        rows.append(
            {
                # 행은 순번이 아니라 범위로 가리킨다(행 순서는 첫 파일 경로 사전순).
                "date": f"{cells[0]}({paths[0] if paths else '빈 범위'})",
                "paths": paths,
                "lines": int(count),
                "fingerprint": cells[3].strip("`"),
                "axis": cells[4],
                "result": cells[5],
                "row": line,
            }
        )
    if not rows:
        raise SystemExit(f"{LEDGER.name}: 「{heading}」 에서 행을 하나도 읽지 못했다(파싱 실패).")
    return rows


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


def check_scope(
    rows: list[dict],
    measured: dict[str, int],
    rule: str,
    surface: str,
) -> None:
    """범위 불변식(R4·R8) — 주석을 가진 실측 파일은 정확히 한 행에 속한다.

    두 표면이 **같은 함수**를 쓰는 것이 의도다. 규칙을 복사해 두 벌로 두면 한쪽만 조용히
    느슨해지는 경로가 남는다 — ③④ 래칫이 한 표면에만 서 있던 동안이 정확히 그 상태였다.
    """
    registered = {rel for row in rows for rel in row["paths"]}
    missing = sorted(rel for rel, n in measured.items() if n > 0 and rel not in registered)
    if missing:
        fail(
            rule,
            f"{surface} 표면에서 어느 원장 행에도 없는 파일이 주석을 들고 있다"
            f" {len(missing)}개: {', '.join(missing[:8])}"
            f"{' …' if len(missing) > 8 else ''}."
            " 그 파일의 행을 더하고 판정 축을 `—` 로 둘 것 — 「아직 판정하지 않았다」를"
            " diff 에 남기는 것이 옛 잔량 마커의 자리를 대신한다.",
        )
    # 등재됐는데 실측에 없는 파일은 **주석 0줄임을 증명**해야 통과한다. 판정해서 비운 파일을
    # 행에 남기는 것은 이력이지만, 스캔 범위에서 사라진 파일이 조용히 남는 것은 낡음이다.
    ghosts = sorted(rel for rel in registered if rel not in measured)
    if ghosts:
        fail(
            rule,
            f"{surface} 표면의 원장이 스캔 범위 밖 파일을 등재하고 있다"
            f" {len(ghosts)}개: {', '.join(ghosts[:8])}"
            f"{' …' if len(ghosts) > 8 else ''}. 파일이 사라졌거나 이름이 바뀌었으면"
            " 그 범위는 다시 판정받아야 한다.",
        )


def check_axis(rows: list[dict], rule: str, surface: str) -> dict[str, int]:
    """R9 — 판정 축 칸의 표기를 강제하고 축별 집계를 돌려준다."""
    bad = [f"{row['date']}: {row['axis']!r}" for row in rows if row["axis"] not in VALID_AXES]
    if bad:
        fail(
            rule,
            f"{surface} 표면에 판정 축 표기가 유효하지 않은 행 {len(bad)}개:"
            f" {', '.join(bad[:6])}{' …' if len(bad) > 6 else ''}."
            " 값은 `—`(미판정) 이거나 `①②③④` 의 정규 순서 부분열이어야 한다.",
        )
    tally = {"done": 0, "unjudged": 0, "lines_done": 0, "hist": {}}
    for row in rows:
        tally["hist"][row["axis"]] = tally["hist"].get(row["axis"], 0) + 1
        if row["axis"] == AXES:
            tally["done"] += 1
            tally["lines_done"] += row["lines"]
        if row["axis"] == "—":
            tally["unjudged"] += 1
    return tally


def axis_histogram(tally: dict) -> str:
    """축별 행 수를 한 줄로. 이전 형식의 「축별 잔량 마커」가 읽히던 자리를 대신한다."""
    order = sorted(tally["hist"], key=lambda a: (a == "—", -len(a), a))
    return " · ".join(f"{a} {tally['hist'][a]}행" for a in order)


def check_axis_evidence(rows: list[dict], rule: str, surface: str) -> None:
    """R11 — 판정 축 칸이 결과 칸의 **현재 지문에 묶인 근거**와 정확히 일치한다.

    양방향인 것이 요점이다. ⑴ 근거 없이 축을 적으면 묻지 않은 것이 완료로 계수되고,
    ⑵ 근거가 있는데 축을 비우면 진척이 집계에서 사라진다. 그리고 앵커가 **그 행의 현재
    지문**에 묶이므로, 재판정으로 주석이 바뀐 행은 축이 자동으로 낡는다 — 이 원장의 재판정
    관례가 결과 칸을 덮어쓰지 않고 옛 문면을 접미사로 남기는 것이라, 토큰의 *존재만* 세면
    어제 59줄에서 내린 판정이 오늘 79줄이 된 같은 행을 계속 인증한다.
    """
    problems: list[str] = []
    for row in rows:
        backed = ""
        for axis, (tokens, anchor_re) in AXIS_EVIDENCE.items():
            has_prose = all(token in row["result"] for token in tokens)
            anchored = row["fingerprint"] in set(anchor_re.findall(row["result"]))
            if has_prose and anchored:
                backed += axis
        backed = "".join(a for a in AXES if a in backed) or "—"
        if backed != row["axis"]:
            problems.append(f"{row['date']} 칸 `{row['axis']}` != 근거 `{backed}`")
    if problems:
        fail(
            rule,
            f"{surface} 표면에 판정 축 칸과 근거가 어긋난 행 {len(problems)}개:"
            f" {'; '.join(problems[:5])}{' …' if len(problems) > 5 else ''}."
            " 축을 적으려면 결과 칸이 그 축의 판정을 담고 「복원경로 <축> 판정 @지문"
            " <그 행의 현재 지문>」 으로 못박아야 한다. 재판정으로 지문이 움직였으면 그 축은"
            " 낡은 것이므로 칸을 `—` 로 되돌리고 새 내용을 다시 물을 것.",
        )


def check_structure(text: str) -> None:
    """R10 — 원장에는 표와 「읽는 법」 절만 둔다."""
    allowed_headings = {"# 주석 비중복성 판정 원장", LEDGER_SECTION, DOC_LEDGER_SECTION,
                        READING_SECTION}
    section = None
    strays: list[str] = []
    for lineno, line in enumerate(text.splitlines(), 1):
        if line.startswith("#"):
            if line.strip() not in allowed_headings:
                fail("R10", f"{LEDGER.name}:{lineno} 원장에 허용되지 않은 절 제목: {line.strip()}")
            section = line.strip()
            continue
        if section in (LEDGER_SECTION, DOC_LEDGER_SECTION):
            if line.strip() and not line.strip().startswith("|"):
                strays.append(f"{lineno}: {line.strip()[:60]}")
    if strays:
        fail(
            "R10",
            f"{LEDGER.name} 의 판정 이력 절에 표 아닌 산문이 있다 {len(strays)}줄:"
            f" {' / '.join(strays[:4])}{' …' if len(strays) > 4 else ''}."
            " 경위·병합 재측정·인계 문단은 그 패스의 `passes/` 파일로 옮길 것"
            " — 원장에는 표와 「읽는 법」만 둔다.",
        )


def main() -> int:
    for required in (POLICY_README, LEDGER):
        if not required.exists():
            print(f"[R1] 정책 SSOT 가 없다: {required.relative_to(REPO_ROOT)}", file=sys.stderr)
            return 1

    text = LEDGER.read_text(encoding="utf-8")
    rows = parse_ledger(text, LEDGER_SECTION)
    doc_rows = parse_ledger(text, DOC_LEDGER_SECTION)

    in_scope = set(scan_files())
    hits = comment_lines(sorted(in_scope))
    doc_hits = docstring_lines(sorted(in_scope))

    # R1·R2·R3 — 줄 주석 표면
    ledger_sum = check_ledger(rows, in_scope, comment_lines, ("R1", "R2", "R3"))
    # R5·R6·R7 — docstring 표면
    doc_sum = check_ledger(
        doc_rows, in_scope, docstring_lines, ("R5", "R6", "R7"), only_python=True
    )

    # R4·R8 — 범위 불변식. 실측은 파일별 줄 수로 잰다(0줄 파일과 범위 밖 파일을 가르기 위해).
    measured = {rel: 0 for rel in in_scope}
    for hit in hits:
        measured[hit.split(":", 1)[0]] += 1
    doc_measured = {rel: 0 for rel in in_scope if rel.endswith(".py")}
    for hit in doc_hits:
        doc_measured[hit.split(":", 1)[0]] += 1
    check_scope(rows, measured, "R4", "줄 주석")
    check_scope(doc_rows, doc_measured, "R8", "docstring")

    # R9 — 판정 축 표기
    tally = check_axis(rows, "R9", "줄 주석")
    doc_tally = check_axis(doc_rows, "R9", "docstring")

    # R10 — 원장 구조
    check_structure(text)

    # R11 — 판정 축 주장 ↔ 근거 일치(양방향)
    check_axis_evidence(rows, "R11", "줄 주석")
    check_axis_evidence(doc_rows, "R11", "docstring")

    if failures:
        for line in failures:
            print(line, file=sys.stderr)
        print(
            f"\nFAIL: {len(failures)}건 — {census(hits)} · {docstring_census(doc_hits)}",
            file=sys.stderr,
        )
        return 1

    share = (tally["lines_done"] * 100.0 / len(hits)) if hits else 0.0
    doc_share = (doc_tally["lines_done"] * 100.0 / len(doc_hits)) if doc_hits else 0.0
    print(
        f"OK: 규칙 R1~R11 위반 없음 — {census(hits)}"
        f" · 원장 {len(rows)}행 / 등재 범위 {ledger_sum}줄"
        f" · 네 경로 판정 완료 {tally['done']}행 {tally['lines_done']}줄({share:.1f}%)"
        f" · 미판정(—) {tally['unjudged']}행"
        f" · 축별 {axis_histogram(tally)}"
    )
    for row in rows:
        print(
            f"  {row['date']}  {row['lines']:>4}줄  {row['fingerprint']}"
            f"  {row['axis']:<4}  {' '.join(row['paths'])}"
        )
    print(f"  전체 지문: {fingerprint(hits)}")
    print(
        f"     {docstring_census(doc_hits)}"
        f" · 원장 {len(doc_rows)}행 / 등재 범위 {doc_sum}줄"
        f" · 네 경로 판정 완료 {doc_tally['done']}행"
        f" {doc_tally['lines_done']}줄({doc_share:.1f}%)"
        f" · 미판정(—) {doc_tally['unjudged']}행"
        f" · 축별 {axis_histogram(doc_tally)}"
    )
    for row in doc_rows:
        print(
            f"  {row['date']}  {row['lines']:>4}줄  {row['fingerprint']}"
            f"  {row['axis']:<4}  {' '.join(row['paths'])}"
        )
    print(f"  전체 docstring 지문: {fingerprint(doc_hits)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
