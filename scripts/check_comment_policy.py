#!/usr/bin/env python3
"""주석 비중복성 판정 원장 ↔ 실제 주석 상태 대조 체커.

줄 주석 표면 — 모델 `tbm_homelab-k3s-mcp-comment-redundancy` 의 as-is 지문과 같은 정의:

* **R1** 원장 행의 범위 파일이 실재하고 정책의 스캔 범위 안이다. 빈 범위는 등재가 아니다.
* **R2** 각 행의 범위를 **모델 as-is 지문과 동일한 추출·정규화·정렬**로 재측정한 주석 줄 수와
  지문이 등재값과 같다. 다르면 그 범위는 판정 이후 주석이 바뀐 것이므로 재판정 대상이다.
* **R3** 행 사이에 같은 파일이 두 번 등재되지 않는다(판정 완료량의 이중 계상 방지).
* **R4** **범위 불변식** — 주석 줄을 가진 실측 파일은 **정확히 한 행**에 속한다(전수). 어느 행에도
  없는 파일이 주석을 들고 들어오면 여기서 멈춘다. 등재됐는데 지금 주석이 0줄인 파일은
  통과시킨다.

docstring 표면(D) — `ast` 로 뜯은 module·class·function docstring 의 본문(빈 줄과 기계가 읽는
선언 줄은 제외). 새 파일이 조용히 표면을 넓히는 것은 R8 이 막는다:

* **R5** docstring 원장 행의 범위가 실재하는 `.py` 이고 스캔 범위 안이다.
* **R6** 각 행의 재측정 줄 수·지문이 등재값과 같다(R2 와 같은 성질).
* **R7** docstring 행 사이 이중 등재 금지(R3 과 같은 성질).
* **R8** docstring 표면의 **범위 불변식**(R4 와 같은 성질·같은 함수). 새 파일이 docstring 을 들고
  들어오면 그 파일의 행이 없어 CI 가 멈춘다.

위 여덟은 **등재 범위가 변했는지**만 재고 그 판정이 **무엇을 물었는지**는 보지 않는다.
그 자리는 원장의 `판정 축` 칸이 담고, R9 가 그 칸의 표기를 강제한다:

* **R9** 모든 행의 `판정 축` 칸이 `—` 이거나 `①②③④` 의 **정규 순서 부분열**이다(`①②`·`③④`·
  `①②③④` 등). 빈 칸도, 순서가 뒤집힌 표기도, 정의에 없는 기호도 받지 않는다 — 진척을 세는
  유일한 자리라 표기가 흔들리면 집계가 조용히 갈린다.
* **R10** 원장에는 **표와 「읽는 법」 절만** 둔다. 경위·병합 재측정·인계 문단은 그 패스의
  `passes/` 파일로 간다.

줄 끝 주석 표면(E) — 코드 뒤에 오는 주석. 모델 as-is 의 `eol=줄/파일` 과 **같은 규칙**이다
(`eol_lines` 의 docstring 참조):

* **R12** 줄 끝 주석 원장 행의 범위가 실재하고, 이 표면이 **보는** 파일이다(R1·R5 와 같은
  성질). 보지 않는 파일을 등재하면 그 행이 영원히 0줄로 남아 「판정했다」는 거짓이 된다.
* **R13** 각 행의 재측정 줄 수·지문이 등재값과 같다(R2·R6 과 같은 성질).
* **R14** 줄 끝 주석 행 사이 이중 등재 금지(R3·R7 과 같은 성질).
* **R15** 줄 끝 주석 표면의 **범위 불변식**(R4·R8 과 같은 성질·같은 함수).
"""
from __future__ import annotations

import ast
import hashlib
import io
import os
import pathlib
import re
import subprocess
import sys
import tokenize
from collections.abc import Callable

HERE = pathlib.Path(__file__).resolve().parent
REPO_ROOT = HERE.parent
POLICY_DIR = REPO_ROOT / "docs" / "comment-policy"
POLICY_README = POLICY_DIR / "README.md"
LEDGER = POLICY_DIR / "ledger.md"

# 아래 정의들이 모델 tbm_homelab-k3s-mcp-comment-redundancy 의 as-is 버전 스크립트와 같은지는
# **바이트 일치**로 확인한다 — 같은 트리에서 이 게이트의 인구조사와 모델 지문 스크립트의
# `lines=/files=/unclassified=` 가 어긋나면 그중 하나가 틀린 것이다.
FIXED_EXCLUDE_RE = re.compile(
    r"^docs/|\.md$"
    r"|(^|/)(vendor|node_modules|dist|build|target|\.venv|venv|__pycache__|\.next|coverage)/"
    r"|(^|/)(package-lock\.json|yarn\.lock|pnpm-lock\.yaml|go\.sum|uv\.lock|poetry\.lock"
    r"|Cargo\.lock|Pipfile\.lock)$"
)
SCOPE_EXCLUDE_FENCE = "comment-scope-exclude"
GENERATED_RE = re.compile(r"DO NOT EDIT|@generated")
GENERATED_HEAD_LINES = 5

DIRECTIVE_RE = re.compile(
    r":#!|//go:|nolint|eslint-|@ts-|prettier-ignore|/// <reference|istanbul ignore"
    r"|c8 ignore|# ?noqa|# ?type:|# ?pragma|# ?pylint:|# ?fmt:|shellcheck |# ?syntax="
    r"|yaml-language-server:|검증 AC:|mock-exception:"
)

# LC_ALL=C 의 `[[:space:]]` 에서 줄바꿈을 뺀 것. `\s` 를 쓰면 유니코드 공백까지 걸려 모델의
# ERE 보다 넓어진다 — 넓은 쪽이 안전해 보이지만 두 정의가 갈리는 것 자체가 이 파일이 막으려는
# 실패다.
_INDENT = r"[ \t\v\f\r]*"
_SUFFIX = r"(\.(example|sample|template|tmpl|tpl|dist|in|j2))?$"
# 순서가 규칙의 일부다 — `requirements.txt` 는 `#` 군에 들어야 하고, 아래 NONE 군의 `\.txt` 에
# 먼저 걸리면 주석이 통째로 사라진다.
LANGUAGE_GROUPS = (
    (
        re.compile(
            r"\.(go|rs|java|kt|kts|scala|groovy|gradle|swift|c|h|cc|cpp|hpp|cs|m|js|jsx"
            r"|mjs|cjs|ts|tsx|mts|cts|css|scss|less|proto|jsonc)"
            + _SUFFIX
            + r"|(^|/)(go\.mod|go\.work|tsconfig[^/]*\.json|jsconfig[^/]*\.json"
            r"|\.devcontainer/[^/]*\.json)$"
        ),
        re.compile(_INDENT + r"(//|/\*|\*([ \t]|$)|\{/\*)"),
    ),
    (
        re.compile(
            r"\.(py|pyi|rb|sh|bash|zsh|fish|pl|r|ya?ml|toml|tf|tfvars|hcl|cfg|conf|ini"
            r"|mk|dockerfile|nix|awk|sed)"
            + _SUFFIX
            + r"|(^|/)(Makefile|GNUmakefile|Dockerfile[^/]*|Containerfile|Caddyfile"
            r"|\.gitignore|\.dockerignore|\.gitattributes|\.helmignore|\.editorconfig"
            r"|\.env[^/]*|CODEOWNERS|requirements[^/]*\.txt)$"
        ),
        re.compile(_INDENT + r"#"),
    ),
    (re.compile(r"\.(sql|lua|hs|elm|ada|adb)" + _SUFFIX), re.compile(_INDENT + r"--")),
    (
        re.compile(r"\.(html?|vue|svelte|astro)" + _SUFFIX),
        re.compile(_INDENT + r"(<!--|//|/\*|\*([ \t]|$)|\{/\*)"),
    ),
    (re.compile(r"\.(xml|svg|xhtml|plist|xsd|xsl)" + _SUFFIX), re.compile(_INDENT + r"<!--")),
    (
        re.compile(
            r"\.(json|jsonl|ndjson|csv|tsv|txt|avsc|snap|golden|pem|crt|key|pub|patch"
            r"|diff|log|lock|sum|mod|map|http)"
            + _SUFFIX
            + r"|(^|/)(LICENSE[^/]*|NOTICE|AUTHORS|\.nvmrc|\.node-version|\.python-version"
            r"|\.tool-versions|\.gitkeep|py\.typed)$"
        ),
        None,
    ),
)
SCOPE_LABEL = "레포 전체 − 제외 세 부류(복원 원본 · 편집 불가 · 주석 문법 없는 데이터·자산)"

DOCSTRING_DECL_RE = re.compile(r"^(검증 시나리오|실행 대상|추가 인자|실행 순서|병렬 레인):")

# 표는 절 제목으로 찾는다. 경계 주석 마커를 두지 않는 것은 그것도 손으로 유지하는 좌표이기
# 때문이다 — 표가 어느 절에 속하는지는 제목이 이미 말한다.
LEDGER_SECTION = "## 판정 이력"
DOC_LEDGER_SECTION = "## docstring 판정 이력"
EOL_LEDGER_SECTION = "## 줄 끝 주석 판정 이력"
READING_SECTION = "## 읽는 법"

AXIS_EVIDENCE = {
    "①②": (("복원경로 ①:", "복원경로 ②:"), re.compile(r"복원경로 ①② 판정 @지문 `([0-9a-f]{12})`")),
    "③④": (("복원경로 ③:", "복원경로 ④:"), re.compile(r"복원경로 ③④ 판정 @지문 `([0-9a-f]{12})`")),
}

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


def repo_exclude_res() -> list[str]:
    """정책 본문의 `comment-scope-exclude` 블록(한 줄에 ERE 하나). 없으면 빈 목록.

    ERE 가 깨졌으면 빈 범위로 조용히 뭉개지 않고 멈춘다.
    """
    fence = f"```{SCOPE_EXCLUDE_FENCE}"
    text = POLICY_README.read_text(encoding="utf-8")
    out: list[str] = []
    inside = False
    for line in text.splitlines():
        if not inside:
            if line.strip() == fence:
                inside = True
            continue
        if line.startswith("```"):
            break
        if line.strip():
            out.append(line)
    for pattern in out:
        try:
            re.compile(pattern)
        except re.error as exc:
            raise SystemExit(
                f"{POLICY_README.name} 의 {SCOPE_EXCLUDE_FENCE} 블록에 깨진 ERE 가 있다"
                f" — {pattern!r}: {exc}"
            )
    return out


def language_pattern(rel: str) -> re.Pattern[str] | None:
    """이 경로가 드는 언어군의 주석 줄 ERE. 데이터·자산 군이면 None."""
    for names, comment in LANGUAGE_GROUPS:
        if names.search(rel):
            return comment
    try:
        head = (REPO_ROOT / rel).read_bytes()[:2]
    except OSError:
        return None
    if head == b"#!":
        return LANGUAGE_GROUPS[1][1]
    return None


def is_scannable(rel: str) -> bool:
    """텍스트이고 생성 산출물이 아닌가(제외 부류 ② 의 나머지 절반)."""
    try:
        raw = (REPO_ROOT / rel).read_bytes()
    except OSError:
        return False
    if b"\0" in raw:
        return False
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError:
        return False
    return not GENERATED_RE.search("\n".join(text.splitlines()[:GENERATED_HEAD_LINES]))


def scan_files() -> list[str]:
    """모델 as-is 지문과 동일한 스캔 범위(레포 상대 경로, 정렬)."""
    out = subprocess.run(
        ["git", "ls-files"],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        check=True,
    ).stdout
    exclude = [FIXED_EXCLUDE_RE.pattern] + repo_exclude_res()
    exclude_re = re.compile("|".join(exclude))
    paths = [ln for ln in out.splitlines() if ln and not exclude_re.search(ln)]
    paths = [rel for rel in paths if is_scannable(rel)]
    if not paths:
        raise SystemExit(
            "스캔 범위가 비어 있다 — git ls-files 가 아무 파일도 돌려주지 않았다. "
            "레포 루트에서 실행 중인지 확인할 것."
        )
    return sorted(paths)


def unclassified_files(paths: list[str]) -> list[str]:
    """어느 언어군에도 들지 않은 파일."""
    return sorted(
        rel
        for rel in paths
        if language_pattern(rel) is None
        and not any(names.search(rel) for names, comment in LANGUAGE_GROUPS if comment is None)
    )


def comment_lines(paths: list[str]) -> list[str]:
    """`경로:주석` 줄의 정규화·정렬 목록. as-is 지문과 같은 구성이다."""
    hits: list[str] = []
    for rel in sorted(paths):
        pattern = language_pattern(rel)
        if pattern is None:
            continue
        try:
            text = (REPO_ROOT / rel).read_text(encoding="utf-8")
        except (UnicodeDecodeError, FileNotFoundError):
            continue
        for line in text.splitlines():
            if not pattern.match(line):
                continue
            entry = f"{rel}:{line}"
            if DIRECTIVE_RE.search(entry):
                continue
            hits.append(re.sub(r"\s+", " ", entry).strip())
    return sorted(hits)


def docstring_lines(paths: list[str]) -> list[str]:
    """`경로:docstring 줄` 의 정규화·정렬 목록. 줄 주석 표면과 같은 구성이다.

    빈 줄은 세지 않는다 — 문단 사이 여백은 판정 대상이 아니고, 그것까지 지문에 넣으면
    문단을 옮기기만 해도 재판정 대상이 된다. 파싱에 실패하는 `.py` 는 인구조사를 통째로
    못 믿게 만드므로 조용히 건너뛰지 않고 멈춘다.
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


EOL_DIRECTIVE_RE = re.compile(DIRECTIVE_RE.pattern.replace(r":#!|", "", 1))
EOL_TAGS: tuple[str | None, ...] = ("C", "HASH", "DASH", None, None, None)
EOL_HASH_EXTS = frozenset(
    ("sh", "bash", "zsh", "fish", "rb", "pl", "r", "yaml", "yml", "toml",
     "tf", "tfvars", "hcl", "mk", "nix", "awk")
)
EOL_MAKE_RE = re.compile(r"^(Makefile|GNUmakefile)$|\.mk$")
EOL_SUFFIX_RE = re.compile(r"\.(example|sample|template|tmpl|tpl|dist|in|j2)$")
EOL_OPEN_OK = frozenset(" \t=([{,:")


def eol_scanner(rel: str) -> str | None:
    """이 경로에 붙는 E 스캐너 이름. `None` 이면 **이 표면이 보지 않는 파일**이다.

    멤버십과 스캐너 선택을 한 함수가 답하는 것이 의도다. 「언어군에 들었는가」로만 물으면
    Dockerfile·`.gitignore`류·`.env`·ini/cfg 처럼 `#` 군이지만 줄 중간 `#` 이 주석이 아니라
    스캔하지 않는 파일이 「표면 안」으로 읽히고, 원장이 그 파일을 등재하고도 영원히 0줄로
    남는다 — 묻지 않은 것이 「판정했다」가 되는 자리다. R12·R15 가 이 술어를 쓴다.

    `EOL_TAGS` 는 `LANGUAGE_GROUPS` 와 **같은 순서로 맞선** 태그 목록이고 `None` 은 「그 군은
    E 를 보지 않는다」다. 두 목록의 길이가 갈리면 뒤쪽 군이 조용히 태그를 잃으므로 함께 고칠 것.
    """
    tag = None
    for (names, _), candidate in zip(LANGUAGE_GROUPS, EOL_TAGS):
        if names.search(rel):
            tag = candidate
            break
    else:
        try:
            head = (REPO_ROOT / rel).read_bytes()[:2]
        except OSError:
            return None
        if head != b"#!":
            return None
        tag = "SHEBANG"
    if tag is None:
        return None
    name = EOL_SUFFIX_RE.sub("", os.path.basename(rel))
    ext = name.rsplit(".", 1)[1].lower() if "." in name else ""
    if tag == "C":
        return "slash"
    if tag == "DASH":
        return "dash"
    if ext in ("py", "pyi"):
        return "python"
    if tag == "SHEBANG":
        try:
            shebang = (REPO_ROOT / rel).read_text(encoding="utf-8").split("\n", 1)[0]
        except (UnicodeDecodeError, OSError):
            return None
        return "python" if "python" in shebang else "hash"
    if ext in EOL_HASH_EXTS or EOL_MAKE_RE.search(name) or name.startswith("requirements"):
        return "hash"
    return None


def eol_lines(paths: list[str]) -> list[str]:
    """`경로:주석` 줄의 정규화·정렬 목록. **코드 뒤에 오는** 주석만 담는다.

    파생을 손으로 베끼지 않는다 — `EOL_DIRECTIVE_RE` 는 `DIRECTIVE_RE` 에서 shebang 갈래
    하나만 떼어 만들고, 버전 스크립트도 자기 `DIRECTIVE` 에서 같은 갈래를 떼어 같은 값을 만든다.

    줄머리 주석은 L 몫이라 건너뛴다. 지시자 줄은 버리지만 그 뒤에 붙은 **사유**는 남긴다
    (지시자 뒤에 공백·마커·공백으로 사유를 덧붙인 꼴) — 사유는 기계가 읽지 않는 사람의
    문장이다. 지시자 판정은 `경로:본문` 이 아니라 **본문에만** 걸린다. 경로에 지시자 낱말이 든
    파일이 생기면 그 파일의 줄이 통째로 사라지기 때문이다.

    파싱에 실패하면 조용히 빠지지 않고 멈춘다 — 표면이 한 파일만큼 작아지면
    그 줄들이 판정을 받지 않은 채 초록을 얻는다. `go.mod`·`go.work` 의 `// indirect` 는
    `go mod tidy` 가 쓰고 읽는 표식이라 여기서도 뺀다.
    """
    hits: list[str] = []

    def emit(rel: str, text: str) -> None:
        norm = re.sub(r"\s+", " ", text).strip()
        if norm and not EOL_DIRECTIVE_RE.search(norm):
            hits.append(f"{rel}:{norm}")

    def rescue(rel: str, comment: str, tail: str) -> None:
        """지시자 줄에서 사유 부분만 건져 낸다(없으면 아무것도 내지 않는다)."""
        if EOL_DIRECTIVE_RE.search(comment):
            match = re.search(tail, comment[1:])
            if match:
                emit(rel, comment[1 + match.start():])

    def scan_python(rel: str, src: str) -> None:
        """`tokenize` 로 주석 토큰을 뽑고, 그 앞에 코드가 있는 것만 E 로 낸다."""
        lines = src.splitlines()
        try:
            for tok in tokenize.generate_tokens(io.StringIO(src).readline):
                if tok.type != tokenize.COMMENT:
                    continue
                row, col = tok.start
                if lines[row - 1][:col].strip():
                    emit(rel, tok.string)
                else:
                    rescue(rel, tok.string, r"\s#\s")
        except (tokenize.TokenError, IndentationError, SyntaxError) as exc:
            raise SystemExit(f"{rel}: tokenize 실패로 줄 끝 주석 표면을 잴 수 없다 — {exc}")

    def scan_marker(rel: str, src: str, marker: str, make: bool = False) -> None:
        """`#`·`--` 계열 줄 스캐너.

        `#` 은 앞이 공백일 때만 주석으로 본다(`a#b` 는 주석이 아니다). Makefile 의 `##` 은
        도움말 생성기가 읽는 표식이라 뺀다. 따옴표는 **열리는 자리**(`EOL_OPEN_OK`)에서만
        문자열을 시작한 것으로 보는데, 그러지 않으면 `don't` 의 `'` 가 줄 나머지를 삼킨다.
        """
        for line in src.splitlines():
            stripped = line.lstrip()
            if stripped.startswith(marker):
                rescue(rel, stripped, r"\s" + re.escape(marker) + r"\s")
                continue
            quote = None
            i = 0
            while i < len(line):
                ch = line[i]
                if quote:
                    if ch == "\\" and quote == '"':
                        i += 2
                        continue
                    if ch == quote:
                        quote = None
                elif ch in "'\"" and (i == 0 or line[i - 1] in EOL_OPEN_OK):
                    quote = ch
                elif line.startswith(marker, i) and (marker != "#" or line[i - 1] in " \t"):
                    comment = line[i:]
                    if not (make and re.match(r"^##( |$)", comment)):
                        emit(rel, comment)
                    break
                i += 1

    def scan_slash(rel: str, src: str, line_comments: bool) -> None:
        """C 언어군 줄 스캐너. 문자열(`"` `'` 백틱)과 블록 주석 상태를 줄을 넘어 따라간다.

        `'`·`"` 는 줄 끝에서 닫는 것으로 본다(그 언어들에서 줄을 넘는 것은 백틱뿐이다).
        `line_comments` 가 거짓이면 `//` 를 주석으로 보지 않는다 — `.css` 가 그 경우이고
        블록 주석만 센다.
        """
        quote = None
        block = False
        for line in src.splitlines():
            stripped = line.lstrip()
            if quote in ('"', "'"):
                quote = None
            lead = not block and quote is None and stripped.startswith(("//", "/*", "*", "{/*"))
            if lead and stripped.startswith("//"):
                rescue(rel, stripped[1:], r"\s(--|//)\s")
            i = 0
            code = False
            while i < len(line):
                ch = line[i]
                nxt = line[i + 1] if i + 1 < len(line) else ""
                if block:
                    if ch == "*" and nxt == "/":
                        block = False
                        i += 2
                        continue
                    i += 1
                    continue
                if quote:
                    if ch == "\\":
                        i += 2
                        continue
                    if ch == quote:
                        quote = None
                    i += 1
                    continue
                if ch == "\\":
                    i += 2
                    code = True
                    continue
                if line_comments and ch == "/" and nxt == "/":
                    if code and not lead:
                        emit(rel, line[i:])
                    break
                if ch == "/" and nxt == "*":
                    end = line.find("*/", i + 2)
                    if code and not lead:
                        emit(rel, line[i:] if end < 0 else line[i:end + 2])
                    if end < 0:
                        block = True
                        break
                    i = end + 2
                    continue
                if ch in "\"'`":
                    quote = ch
                if not ch.isspace() and ch != "{":
                    code = True
                i += 1

    for rel in sorted(paths):
        scanner = eol_scanner(rel)
        if scanner is None:
            continue
        try:
            src = (REPO_ROOT / rel).read_text(encoding="utf-8")
        except (UnicodeDecodeError, FileNotFoundError):
            continue
        name = EOL_SUFFIX_RE.sub("", os.path.basename(rel))
        if scanner == "slash":
            mark = len(hits)
            scan_slash(rel, src, not name.endswith(".css"))
            if name in ("go.mod", "go.work"):
                hits[mark:] = [h for h in hits[mark:] if not h.endswith(":// indirect")]
        elif scanner == "dash":
            scan_marker(rel, src, "--")
        elif scanner == "python":
            scan_python(rel, src)
        else:
            scan_marker(rel, src, "#", make=bool(EOL_MAKE_RE.search(name)))
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
            continue
        if index + 1 < len(table):
            nxt_cells = [c.strip() for c in table[index + 1].strip("|").split("|")]
            if nxt_cells and all(set(c) <= set("-: ") and c for c in nxt_cells):
                continue
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


def census(hits: list[str], unclassified: list[str]) -> str:
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
    return (
        f"판정 대상 주석 {len(hits)}줄 / {len(files)}파일 ({breakdown})"
        f" · 미분류 {len(unclassified)}"
        + (f" [{', '.join(unclassified[:5])}]" if unclassified else "")
    )


def docstring_census(hits: list[str]) -> str:
    files = sorted({h.split(":", 1)[0] for h in hits})
    return f"판정 대상 docstring {len(hits)}줄 / {len(files)}파일"


def eol_census(hits: list[str]) -> str:
    files = sorted({h.split(":", 1)[0] for h in hits})
    return f"판정 대상 줄 끝 주석 {len(hits)}줄 / {len(files)}파일"


def check_ledger(
    rows: list[dict],
    in_scope: set[str],
    extract,
    rules: tuple[str, str, str],
    restrict: tuple[Callable[[str], bool], str] | None = None,
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
                    f" (범위: {SCOPE_LABEL}). 파일이 사라졌거나 이름이 바뀌었으면"
                    " 그 범위는 다시 판정받아야 한다.",
                )
            elif restrict is not None and not restrict[0](rel):
                fail(existence, f"{row['date']} 행의 `{rel}` 은 {restrict[1]}")

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
    ⑵ 근거가 있는데 축을 비우면 진척이 집계에서 사라진다.
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
                        EOL_LEDGER_SECTION, READING_SECTION}
    section = None
    strays: list[str] = []
    for lineno, line in enumerate(text.splitlines(), 1):
        if line.startswith("#"):
            if line.strip() not in allowed_headings:
                fail("R10", f"{LEDGER.name}:{lineno} 원장에 허용되지 않은 절 제목: {line.strip()}")
            section = line.strip()
            continue
        if section in (LEDGER_SECTION, DOC_LEDGER_SECTION, EOL_LEDGER_SECTION):
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
    eol_rows = parse_ledger(text, EOL_LEDGER_SECTION)

    scanned = scan_files()
    in_scope = set(scanned)
    unclassified = unclassified_files(scanned)
    hits = comment_lines(sorted(in_scope))
    doc_hits = docstring_lines(sorted(in_scope))
    eol_hits = eol_lines(sorted(in_scope))

    # R1·R2·R3 — 줄 주석 표면
    ledger_sum = check_ledger(rows, in_scope, comment_lines, ("R1", "R2", "R3"))
    # R5·R6·R7 — docstring 표면
    doc_sum = check_ledger(
        doc_rows,
        in_scope,
        docstring_lines,
        ("R5", "R6", "R7"),
        restrict=(
            lambda rel: rel.endswith(".py"),
            "`.py` 가 아니다 — docstring 표면은 Python 파일만 잰다.",
        ),
    )
    # R12·R13·R14 — 줄 끝 주석 표면
    eol_sum = check_ledger(
        eol_rows,
        in_scope,
        eol_lines,
        ("R12", "R13", "R14"),
        restrict=(
            lambda rel: eol_scanner(rel) is not None,
            "줄 끝 주석 표면이 보지 않는 언어군이다 —"
            " MIXED·MARKUP 군과 줄 중간 `#` 이 주석이 아닌 HASH 파일은 이 표면 밖이다"
            "(README 「지문의 사각지대」).",
        ),
    )

    # R4·R8 — 범위 불변식. 실측은 파일별 줄 수로 잰다(0줄 파일과 범위 밖 파일을 가르기 위해).
    measured = {rel: 0 for rel in in_scope}
    for hit in hits:
        measured[hit.split(":", 1)[0]] += 1
    doc_measured = {rel: 0 for rel in in_scope if rel.endswith(".py")}
    for hit in doc_hits:
        doc_measured[hit.split(":", 1)[0]] += 1
    eol_measured = {rel: 0 for rel in in_scope if eol_scanner(rel) is not None}
    for hit in eol_hits:
        eol_measured[hit.split(":", 1)[0]] += 1
    check_scope(rows, measured, "R4", "줄 주석")
    check_scope(doc_rows, doc_measured, "R8", "docstring")
    check_scope(eol_rows, eol_measured, "R15", "줄 끝 주석")

    # R9 — 판정 축 표기
    tally = check_axis(rows, "R9", "줄 주석")
    doc_tally = check_axis(doc_rows, "R9", "docstring")
    eol_tally = check_axis(eol_rows, "R9", "줄 끝 주석")

    # R10 — 원장 구조
    check_structure(text)

    # R11 — 판정 축 주장 ↔ 근거 일치(양방향)
    check_axis_evidence(rows, "R11", "줄 주석")
    check_axis_evidence(doc_rows, "R11", "docstring")
    check_axis_evidence(eol_rows, "R11", "줄 끝 주석")

    if failures:
        for line in failures:
            print(line, file=sys.stderr)
        print(
            f"\nFAIL: {len(failures)}건 — {census(hits, unclassified)}"
            f" · {docstring_census(doc_hits)} · {eol_census(eol_hits)}",
            file=sys.stderr,
        )
        return 1

    share = (tally["lines_done"] * 100.0 / len(hits)) if hits else 0.0
    doc_share = (doc_tally["lines_done"] * 100.0 / len(doc_hits)) if doc_hits else 0.0
    eol_share = (eol_tally["lines_done"] * 100.0 / len(eol_hits)) if eol_hits else 0.0
    print(
        f"OK: 규칙 R1~R15 위반 없음 — {census(hits, unclassified)}"
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
    print(
        f"     {eol_census(eol_hits)}"
        f" · 원장 {len(eol_rows)}행 / 등재 범위 {eol_sum}줄"
        f" · 네 경로 판정 완료 {eol_tally['done']}행"
        f" {eol_tally['lines_done']}줄({eol_share:.1f}%)"
        f" · 미판정(—) {eol_tally['unjudged']}행"
        f" · 축별 {axis_histogram(eol_tally)}"
    )
    for row in eol_rows:
        print(
            f"  {row['date']}  {row['lines']:>4}줄  {row['fingerprint']}"
            f"  {row['axis']:<4}  {' '.join(row['paths'])}"
        )
    print(f"  전체 줄 끝 주석 지문: {fingerprint(eol_hits)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
