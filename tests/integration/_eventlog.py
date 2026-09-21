"""이벤트 레코드(``msg="tool call"``)를 파드 stdout 에서 집는 공유 표면 (매칭 단위가 아니다).

`test-event-log.md` 의 시나리오 파일들이 같은 세 동작을 되풀이한다 — 배포의 로그 전량에서
레코드 줄만 고르고, 한 줄을 필드 사전으로 풀고, 호출 뒤 그 줄이 kubelet 의 로그 읽기에
나타날 때까지 기다린다. ``event_log_ac3_*.py`` 두 파일은 각자 들고 있었고 셋째 파일부터는
한 벌만 두는 편이 값이 어긋날 자리를 없앤다(``_gatekeeper.py`` 가 승인 조작을 한 벌만 두는
것과 같은 이유).

레코드 한 줄은 ``internal/eventlog/eventlog.go::Attrs`` 가 정한 순서의 ``key=value`` 열이고
그룹 필드는 ``target.kind``·``gate.decision`` 처럼 점으로 이어진다. 빈 값은 slog 의 text
핸들러가 ``key=""`` 로 낸다 — 그래서 성공 레코드의 ``reason`` 은 **없는 키가 아니라 빈 값**이고,
``parse`` 는 그것을 빈 문자열로 돌려준다(AC1 의 「필드 자체는 존재한다」를 그대로 보존).
"""

from __future__ import annotations

import re
import subprocess
import time

RECORD_MARK = 'msg="tool call"'

#: 응답이 돌아온 뒤 kubelet 의 로그 읽기가 그 줄을 보여 주기까지의 창.
RECORD_BUDGET = 15.0

_FIELD_RE = re.compile(r'(?P<key>[A-Za-z_][A-Za-z0-9_.]*)=(?P<value>"(?:[^"\\]|\\.)*"|\S*)')


def records(namespace: str, deployment: str) -> list[str]:
    """배포 파드 stdout 의 레코드 줄 전부(오래된 것부터)."""
    log = subprocess.check_output(
        ["kubectl", "-n", namespace, "logs", f"deploy/{deployment}", "--tail=-1"],
        text=True,
    )
    return [line for line in log.splitlines() if RECORD_MARK in line]


def parse(line: str) -> dict[str, str]:
    fields: dict[str, str] = {}
    for match in _FIELD_RE.finditer(line):
        value = match.group("value")
        if value.startswith('"'):
            value = value[1:-1].encode().decode("unicode_escape")
        fields[match.group("key")] = value
    return fields


def select(lines: list[str], **wanted: str) -> list[str]:
    """``tool="resource_patch", **{"target.name": "x"}`` 처럼 필드 값이 전부 일치하는 줄."""
    return [line for line in lines if all(parse(line).get(k) == v for k, v in wanted.items())]


def wait_for_new(namespace: str, deployment: str, before: int, **wanted: str) -> list[str]:
    """``wanted`` 에 맞는 레코드가 ``before`` 개보다 많아질 때까지 기다려 그 줄들을 돌려준다.

    예산 안에 늘지 않으면 그때의 줄들을 그대로 돌려주고 단언은 호출자가 한다 — 「호출당 정확히
    하나」는 이 헬퍼가 아니라 각 파일의 단언이다.
    """
    deadline = time.monotonic() + RECORD_BUDGET
    while True:
        found = select(records(namespace, deployment), **wanted)
        if len(found) > before or time.monotonic() >= deadline:
            return found
        time.sleep(0.5)


def exactly_one_new(found: list[str], before: int, what: str) -> dict[str, str]:
    assert len(found) - before == 1, (
        f"{what} 의 레코드가 {before} → {len(found)} — 호출당 정확히 하나여야 한다"
    )
    return parse(found[-1])
