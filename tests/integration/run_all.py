#!/usr/bin/env python3
"""통합 e2e 파일 목록 순회 러너 (매칭 단위가 아니다 — AC를 주검증하지 않는다).

`tests/integration/` 최상위의 **매칭 단위 파일**을 자동 발견해, 각 파일이 모듈
docstring에 신고한 실행 대상(`실행 대상:`)별로 골라 차례로 실행한다. CI가 파일을
이름으로 나열하지 않게 되므로,

* AC를 전용 파일로 쪼갤 때마다 `.github/workflows/ci.yml`을 고칠 필요가 없고,
* 스텝 추가를 잊어 **새 파일이 조용히 실행되지 않는 일**이 생기지 않는다.

후자는 `check_ac_mapping.py`가 이 모듈의 배차 결과를 그대로 읽어 "매칭 단위 파일
전부가 정확히 한 번 배차된다"를 CI에서 강제하는 것으로 못박는다.

## 파일이 신고하는 것 (모듈 docstring)

```
검증 AC: <domain>/AC<n>[, <domain>/AC<m> ...]   # 또는  검증 AC: 없음 (스모크/인프라)
실행 대상: primary | auth-variant | oauth-variant
추가 인자: trace                                # 선택 — http-trace 프록시 URL을 argv[2]로 받는다
실행 순서: <정수>                                # 선택 — 기본 50, 작을수록 먼저
```

`실행 대상`은 그 파일이 어느 배포를 상대로 도는지다. `primary`는 모든 자격증명이
배선된 주 배포(`homelab-k3s-mcp` 네임스페이스), `auth-variant`는 인증을 켜고 자격증명을
하나도 붙이지 않은 변형(`tests/k8s/kind/auth-fixture.yaml`), `oauth-variant`는 실 OIDC
발급자(dex)를 가리키도록 `MCP_OAUTH_*`를 세팅한 변형(`tests/k8s/kind/oidc-fixture.yaml`의
`homelab-k3s-mcp-oauth`)이다. 디스커버리 라우트는 OAuth가 구성된 경우에만 걸리므로
`platform-auth-safety/AC2`는 마지막 것에서만 관측된다.

## 사용

```
python tests/integration/run_all.py --group primary \
    --base-url http://127.0.0.1:8080 --trace-url http://127.0.0.1:8090
python tests/integration/run_all.py --group auth-variant --base-url http://127.0.0.1:8088
python tests/integration/run_all.py --group oauth-variant --base-url http://127.0.0.1:8089
python tests/integration/run_all.py --group primary --list      # 드라이런(배차 목록만)
```
"""

from __future__ import annotations

import argparse
import ast
import pathlib
import subprocess
import sys
from dataclasses import dataclass

HERE = pathlib.Path(__file__).resolve().parent

#: 매칭 단위가 아닌 파일 — 공유 헬퍼와 이 하네스 자신.
NOT_MATCHING_UNIT = {"_helpers.py", "run_all.py", "check_ac_mapping.py"}

GROUPS = ("primary", "auth-variant", "oauth-variant")

DEFAULT_ORDER = 50

#: `검증 AC:` 가 AC 대신 취할 수 있는 값 (규칙 3, 비-AC 파일).
NON_AC_MARKER = "없음"


class DeclarationError(Exception):
    """모듈 docstring 선언이 없거나 규약을 벗어났을 때."""


@dataclass(frozen=True)
class Declaration:
    path: pathlib.Path
    acs: tuple[str, ...]
    group: str
    needs_trace: bool
    order: int

    @property
    def name(self) -> str:
        return self.path.name

    @property
    def non_ac(self) -> bool:
        return not self.acs


def matching_unit_paths(root: pathlib.Path = HERE) -> list[pathlib.Path]:
    """매칭 단위 파일(= AC와 1:1로 대응해야 하는 파일) 목록."""
    return sorted(
        p
        for p in root.glob("*.py")
        if p.name not in NOT_MATCHING_UNIT and not p.name.startswith("_")
    )


def _docstring(path: pathlib.Path) -> str:
    return ast.get_docstring(ast.parse(path.read_text(encoding="utf-8"))) or ""


def _field(doc: str, key: str) -> str | None:
    prefix = f"{key}:"
    for line in doc.splitlines():
        stripped = line.strip()
        if stripped.startswith(prefix):
            return stripped[len(prefix) :].strip()
    return None


def parse_declaration(path: pathlib.Path) -> Declaration:
    """모듈 docstring의 선언을 읽는다. 규약 위반이면 DeclarationError."""
    doc = _docstring(path)
    raw_acs = _field(doc, "검증 AC")
    if raw_acs is None:
        raise DeclarationError(
            f"{path.name}: 모듈 docstring에 `검증 AC:` 선언이 없다 "
            f"(매칭 단위 파일은 자신이 주검증하는 AC를 신고해야 한다)"
        )
    if raw_acs.startswith(NON_AC_MARKER):
        acs: tuple[str, ...] = ()
    else:
        acs = tuple(part.strip() for part in raw_acs.split(",") if part.strip())
        if not acs:
            raise DeclarationError(f"{path.name}: `검증 AC:` 값이 비어 있다")

    group = _field(doc, "실행 대상")
    if group not in GROUPS:
        raise DeclarationError(
            f"{path.name}: `실행 대상:` 이 {group!r} — {list(GROUPS)} 중 하나여야 한다"
        )

    needs_trace = (_field(doc, "추가 인자") or "") == "trace"

    raw_order = _field(doc, "실행 순서")
    try:
        order = int(raw_order) if raw_order is not None else DEFAULT_ORDER
    except ValueError as exc:
        raise DeclarationError(
            f"{path.name}: `실행 순서:` 가 정수가 아니다 ({raw_order!r})"
        ) from exc

    return Declaration(
        path=path, acs=acs, group=group, needs_trace=needs_trace, order=order
    )


def declarations(root: pathlib.Path = HERE) -> list[Declaration]:
    """매칭 단위 파일 전부의 선언 (실행 순서 → 파일명 순)."""
    parsed = [parse_declaration(p) for p in matching_unit_paths(root)]
    return sorted(parsed, key=lambda d: (d.order, d.name))


def dispatch_plan(group: str, root: pathlib.Path = HERE) -> list[Declaration]:
    """해당 그룹에서 실행될 파일 목록 (실행 순서대로)."""
    return [d for d in declarations(root) if d.group == group]


def _run_one(decl: Declaration, base_url: str, trace_url: str | None) -> int:
    argv = [sys.executable, str(decl.path), base_url]
    if decl.needs_trace:
        if not trace_url:
            print(
                f"error: {decl.name} 는 `추가 인자: trace` 를 신고했는데 "
                f"--trace-url 이 주어지지 않았다",
                file=sys.stderr,
            )
            return 2
        argv.append(trace_url)
    label = ", ".join(decl.acs) if decl.acs else "비-AC(스모크/인프라)"
    print(f"\n===== {decl.name} (AC: {label}) =====", flush=True)
    return subprocess.run(argv).returncode


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--group", choices=GROUPS, required=True)
    parser.add_argument("--base-url", help="이 그룹의 MCP 서버 URL")
    parser.add_argument("--trace-url", help="http-trace 프록시 admin URL")
    parser.add_argument(
        "--list",
        action="store_true",
        help="실행하지 않고 배차 목록만 출력한다(드라이런)",
    )
    args = parser.parse_args(argv)

    plan = dispatch_plan(args.group)

    if args.list:
        for decl in plan:
            extra = " +trace" if decl.needs_trace else ""
            print(f"{decl.name}{extra}")
        return 0

    if not args.base_url:
        parser.error("--base-url 은 (--list 가 아니면) 필수다")
    if not plan:
        print(f"error: {args.group} 그룹에 배차된 파일이 없다", file=sys.stderr)
        return 2

    print(f"러너: {args.group} 그룹 {len(plan)}개 파일 — {[d.name for d in plan]}")
    for decl in plan:
        code = _run_one(decl, args.base_url.rstrip("/"), args.trace_url)
        if code != 0:
            print(f"\nFAIL: {decl.name} (exit {code})", file=sys.stderr)
            return code
    print(f"\nOK: {args.group} 그룹 {len(plan)}개 파일 전부 통과")
    return 0


if __name__ == "__main__":
    sys.exit(main())
