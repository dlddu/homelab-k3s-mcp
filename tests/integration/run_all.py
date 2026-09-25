#!/usr/bin/env python3
"""통합 e2e 파일 목록 순회 러너 (매칭 단위가 아니다 — 시나리오를 주검증하지 않는다).

`tests/integration/` 최상위의 **매칭 단위 파일**을 자동 발견해, 각 파일이 모듈
docstring에 신고한 실행 대상(`실행 대상:`)별로 골라 차례로 실행한다.

## 파일이 신고하는 것 (모듈 docstring)

```
검증 시나리오: test-<domain>.md#시나리오 <N>       # 또는  검증 시나리오: 없음 (스모크/인프라)
실행 대상: primary | auth-variant | oauth-variant
추가 인자: trace                                # 선택 — http-trace 프록시 URL을 argv[2]로 받는다
실행 순서: <정수>                                # 선택 — 기본 50, 작을수록 먼저
병렬 레인: <이름>                                # 선택 — 같은 그룹의 다른 레인과 동시에 돈다
```

`실행 대상`은 그 파일이 어느 배포를 상대로 도는지다. `primary`는 모든 자격증명이
배선된 주 배포(`homelab-k3s-mcp` 네임스페이스), `auth-variant`는 인증을 켜고 자격증명을
하나도 붙이지 않은 변형(`tests/k8s/kind/auth-fixture.yaml`), `oauth-variant`는 실 OIDC
발급자(dex)를 가리키도록 `MCP_OAUTH_*`를 세팅한 변형(`tests/k8s/kind/oidc-fixture.yaml`의
`homelab-k3s-mcp-oauth`), `gatekeeper-variant`는 실물 gatekeeper 를 기록 프록시 뒤에
두고 승인 타임아웃을 5초로 줄인 변형(`tests/k8s/kind/gatekeeper-variant.yaml`)이다.
디스커버리 라우트는 OAuth가 구성된 경우에만 걸리므로
`test-platform-auth-safety.md#시나리오 2`는 마지막 것에서만 관측된다.

## 병렬 레인

`병렬 레인` 을 선언한 파일은 **레인끼리 동시에**, 레인 안에서는 `실행 순서` 대로 하나씩 돈다.
선언하지 않은 파일은 지금처럼 단독으로 돈다 — `실행 순서` 가 기본값보다 작으면 레인들보다
먼저(배포가 살아 있는지를 먼저 말해 주는 자리라 장벽이 된다), 아니면 레인들이 다 끝난 뒤에.

레인은 **공유 상태의 경계**다. 같은 픽스처·같은 서버 쪽 캐시·같은 관측 기록(http-trace)을
건드리는 파일은 같은 레인에 둔다. 동시 실행이 파일의 전제를 흔들면 대개 실패가 아니라
**헛통과**로 나타나므로(관측 순서가 단언인 파일이 남의 호출 덕에 초록이 되는 식), 확신이
없으면 선언하지 않는 쪽이 기본이다. `--serial` 은 동시 실행이 의심될 때의 대조군이다.

## 사용

```
python tests/integration/run_all.py --group primary \
    --base-url http://127.0.0.1:8080 --trace-url http://127.0.0.1:8090
python tests/integration/run_all.py --group auth-variant --base-url http://127.0.0.1:8088
python tests/integration/run_all.py --group oauth-variant --base-url http://127.0.0.1:8089
python tests/integration/run_all.py --group gatekeeper-variant --base-url http://127.0.0.1:8092
python tests/integration/run_all.py --group primary --list      # 드라이런(배차 목록만)
python tests/integration/run_all.py --group primary --serial ...  # 레인 무시, 전부 직렬
```
"""

from __future__ import annotations

import argparse
import ast
import pathlib
import re
import subprocess
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass

HERE = pathlib.Path(__file__).resolve().parent

#: 매칭 단위가 아닌 파일 — 공유 헬퍼와 이 하네스 자신.
NOT_MATCHING_UNIT = {"_helpers.py", "run_all.py", "check_ac_mapping.py"}

GROUPS = ("primary", "auth-variant", "oauth-variant", "gatekeeper-variant")

DEFAULT_ORDER = 50

#: `검증 시나리오:` 가 시나리오 대신 취할 수 있는 값 (규칙 3, 비-시나리오 파일).
NON_SCENARIO_MARKER = "없음"

LANE_RE = re.compile(r"^[a-z0-9][a-z0-9-]*$")


class DeclarationError(Exception):
    """모듈 docstring 선언이 없거나 규약을 벗어났을 때."""


@dataclass(frozen=True)
class Declaration:
    path: pathlib.Path
    scenarios: tuple[str, ...]
    group: str
    needs_trace: bool
    order: int
    lane: str | None = None

    @property
    def name(self) -> str:
        return self.path.name

    @property
    def non_scenario(self) -> bool:
        return not self.scenarios


def matching_unit_paths(root: pathlib.Path = HERE) -> list[pathlib.Path]:
    """매칭 단위 파일(= 시나리오와 1:1로 대응해야 하는 파일) 목록."""
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
    raw_scenarios = _field(doc, "검증 시나리오")
    if raw_scenarios is None:
        raise DeclarationError(
            f"{path.name}: 모듈 docstring에 `검증 시나리오:` 선언이 없다 "
            f"(매칭 단위 파일은 자신이 주검증하는 시나리오를 신고해야 한다)"
        )
    if raw_scenarios.startswith(NON_SCENARIO_MARKER):
        scenarios: tuple[str, ...] = ()
    else:
        scenarios = tuple(
            part.strip() for part in raw_scenarios.split(",") if part.strip()
        )
        if not scenarios:
            raise DeclarationError(f"{path.name}: `검증 시나리오:` 값이 비어 있다")

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

    lane = _field(doc, "병렬 레인")
    if lane is not None:
        if not LANE_RE.match(lane):
            raise DeclarationError(
                f"{path.name}: `병렬 레인:` 이 {lane!r} — 소문자·숫자·하이픈만 쓴다"
            )
        if order < DEFAULT_ORDER:
            raise DeclarationError(
                f"{path.name}: `실행 순서: {order}` 는 레인보다 먼저 도는 장벽 자리인데 "
                f"`병렬 레인: {lane}` 도 선언했다 — 둘 중 하나만 둔다"
            )

    return Declaration(
        path=path,
        scenarios=scenarios,
        group=group,
        needs_trace=needs_trace,
        order=order,
        lane=lane,
    )


def declarations(root: pathlib.Path = HERE) -> list[Declaration]:
    """매칭 단위 파일 전부의 선언 (실행 순서 → 파일명 순)."""
    parsed = [parse_declaration(p) for p in matching_unit_paths(root)]
    return sorted(parsed, key=lambda d: (d.order, d.name))


def dispatch_plan(group: str, root: pathlib.Path = HERE) -> list[Declaration]:
    """해당 그룹에서 실행될 파일 목록 (실행 순서대로)."""
    return [d for d in declarations(root) if d.group == group]


def _argv(decl: Declaration, base_url: str, trace_url: str | None) -> list[str] | None:
    argv = [sys.executable, str(decl.path), base_url]
    if decl.needs_trace:
        if not trace_url:
            print(
                f"error: {decl.name} 는 `추가 인자: trace` 를 신고했는데 "
                f"--trace-url 이 주어지지 않았다",
                file=sys.stderr,
            )
            return None
        argv.append(trace_url)
    return argv


def _header(decl: Declaration) -> str:
    label = (
        ", ".join(decl.scenarios) if decl.scenarios else "비-시나리오(스모크/인프라)"
    )
    lane = f" [레인 {decl.lane}]" if decl.lane else ""
    return f"\n===== {decl.name}{lane} (시나리오: {label}) ====="


def _run_one(decl: Declaration, base_url: str, trace_url: str | None) -> int:
    """단독 실행. 출력은 그대로 흘려보낸다."""
    argv = _argv(decl, base_url, trace_url)
    if argv is None:
        return 2
    print(_header(decl), flush=True)
    started = time.monotonic()
    code = subprocess.run(argv).returncode
    print(f"----- {decl.name} {time.monotonic() - started:.1f}s", flush=True)
    return code


def _run_lanes(
    lanes: dict[str, list[Declaration]], base_url: str, trace_url: str | None
) -> list[tuple[str, int]]:
    """레인들을 동시에 돌리고 실패한 (파일, 종료 코드) 목록을 돌려준다.

    출력은 파일 단위로 모아 잠금 아래에서 한 번에 찍어 레인끼리 줄이 섞이지 않게 한다.
    """
    failed: list[tuple[str, int]] = []
    stop = threading.Event()
    lock = threading.Lock()

    def lane_worker(files: list[Declaration]) -> None:
        for decl in files:
            if stop.is_set():
                return
            argv = _argv(decl, base_url, trace_url)
            started = time.monotonic()
            if argv is None:
                code, output = 2, ""
            else:
                proc = subprocess.run(
                    argv, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True
                )
                code, output = proc.returncode, proc.stdout
            elapsed = time.monotonic() - started
            with lock:
                print(_header(decl))
                print(output, end="" if output.endswith("\n") or not output else "\n")
                print(f"----- {decl.name} {elapsed:.1f}s", flush=True)
                if code != 0:
                    failed.append((decl.name, code))
                    stop.set()
            if code != 0:
                return

    with ThreadPoolExecutor(max_workers=len(lanes)) as pool:
        for _ in pool.map(lane_worker, lanes.values()):
            pass
    return failed


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
    parser.add_argument(
        "--serial",
        action="store_true",
        help="`병렬 레인` 선언을 무시하고 전부 직렬로 돈다",
    )
    args = parser.parse_args(argv)

    plan = dispatch_plan(args.group)

    if args.list:
        for decl in plan:
            extra = " +trace" if decl.needs_trace else ""
            lane = f" [레인 {decl.lane}]" if decl.lane and not args.serial else ""
            print(f"{decl.name}{extra}{lane}")
        return 0

    if not args.base_url:
        parser.error("--base-url 은 (--list 가 아니면) 필수다")
    if not plan:
        print(f"error: {args.group} 그룹에 배차된 파일이 없다", file=sys.stderr)
        return 2

    print(f"러너: {args.group} 그룹 {len(plan)}개 파일 — {[d.name for d in plan]}")
    base = args.base_url.rstrip("/")
    lane_of = (lambda d: None) if args.serial else (lambda d: d.lane)
    before = [d for d in plan if lane_of(d) is None and d.order < DEFAULT_ORDER]
    after = [d for d in plan if lane_of(d) is None and d.order >= DEFAULT_ORDER]
    lanes: dict[str, list[Declaration]] = {}
    for decl in plan:
        if lane_of(decl) is not None:
            lanes.setdefault(decl.lane, []).append(decl)

    for decl in before:
        code = _run_one(decl, base, args.trace_url)
        if code != 0:
            print(f"\nFAIL: {decl.name} (exit {code})", file=sys.stderr)
            return code

    if lanes:
        print(
            f"\n러너: 레인 {len(lanes)}개 동시 실행 — "
            f"{ {name: len(files) for name, files in lanes.items()} }",
            flush=True,
        )
        started = time.monotonic()
        failed = _run_lanes(lanes, base, args.trace_url)
        print(f"\n러너: 레인 단계 {time.monotonic() - started:.1f}s", flush=True)
        if failed:
            for name, code in failed:
                print(f"\nFAIL: {name} (exit {code})", file=sys.stderr)
            return failed[0][1]

    for decl in after:
        code = _run_one(decl, base, args.trace_url)
        if code != 0:
            print(f"\nFAIL: {decl.name} (exit {code})", file=sys.stderr)
            return code
    print(f"\nOK: {args.group} 그룹 {len(plan)}개 파일 전부 통과")
    return 0


if __name__ == "__main__":
    sys.exit(main())
