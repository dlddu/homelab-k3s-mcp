"""`/metrics` 노출을 스크레이프해 값으로 읽는 공유 표면 (매칭 단위가 아니다).

`test-metrics.md` 의 시나리오 파일들과 `test-event-log.md#시나리오 1` 이 같은 세 동작을
되풀이한다 — 배포의 메트릭 리스너에 닿고, 노출 텍스트를 계열별 값으로 풀고, 호출 전후의
차를 잰다. ``_eventlog.py`` 가 레코드 줄을 한 벌로 두는 것과 같은 이유로 여기 한 벌만 둔다.

리스너에는 **파드 포트로** 닿는다(``kubectl port-forward deploy/<name>``). 주 배포의 Service 는
`metrics` 포트(9090)를 들지만 변형 배포(auth-variant · gatekeeper-variant)의 Service 는 `http`
뿐이고, 서버는 ``METRICS_LISTEN_ADDR`` 이 비어도 ``0.0.0.0:9090`` 을 연다(``main.go``
``buildMetricsServer``) — 그래서 어느 배포든 파드의 9090 이 곧 노출 지점이고, Service 를 고쳐
가며 변형마다 포트를 뚫을 이유가 없다. ``_helpers.port_forward`` 는 Service 를 상대로 하는
러너 그룹의 계약이라 그대로 두고 여기서 파드 쪽 포워드를 한 벌 든다.

값은 텍스트 노출 형식(``name{k="v",...} value``)에서 읽는다. 히스토그램은 ``_bucket`` ·
``_sum`` · ``_count`` 세 이름으로 갈라져 오므로 ``Snapshot`` 은 노출된 **샘플 이름** 그대로를
키로 둔다 — ``mcp_tool_duration_seconds`` 의 관측 수는 ``mcp_tool_duration_seconds_count`` 로
묻는다. 라벨 값의 ``\\`` · ``\"`` · ``\n`` 이스케이프는 푼다.
"""

from __future__ import annotations

import contextlib
import re
import socket
import subprocess
import time
from collections.abc import Iterator

import httpx

from _helpers import EXPECTED_TOOLS

METRICS_PORT = 9090

FAMILIES = {"mcp_tool_calls_total", "mcp_tool_refusals_total", "mcp_tool_duration_seconds", "mcp_gate_wait_seconds"}

LABEL_NAMES = {"tool", "result", "reason", "decision", "le"}

#: internal/eventlog 의 Results·Reasons 와 internal/gatekeeper 의 Verdicts 와 같아야 한다 — 값이 늘면 두 자리가 함께 움직인다.
CLOSED_VALUES = {
    "tool": EXPECTED_TOOLS | {"unregistered"},
    "result": {"success", "refused", "error"},
    "reason": {
        "auth_failed",
        "invalid_input",
        "unconfigured",
        "gate_rejected",
        "gate_expired",
        "gate_timeout",
        "gate_unreachable",
        "gate_unconfigured",
    },
    "decision": {"approved", "rejected", "expired", "timeout", "unreachable", "unconfigured"},
}

_SAMPLE_RE = re.compile(r"^(?P<name>[A-Za-z_:][A-Za-z0-9_:]*)(?:\{(?P<labels>[^}]*)\})?\s+(?P<value>\S+)")
_LABEL_RE = re.compile(r'(?P<key>[A-Za-z_][A-Za-z0-9_]*)="(?P<value>(?:[^"\\]|\\.)*)"')

Labels = tuple[tuple[str, str], ...]


_ESCAPES = {"n": "\n", '"': '"', "\\": "\\"}


def _unescape(value: str) -> str:
    return re.sub(r"\\(.)", lambda m: _ESCAPES.get(m.group(1), m.group(0)), value)


def parse(text: str) -> dict[str, dict[Labels, float]]:
    """노출 텍스트 → ``{샘플 이름: {정렬된 라벨 쌍: 값}}``. ``#`` 줄은 버린다."""
    out: dict[str, dict[Labels, float]] = {}
    for line in text.splitlines():
        if not line or line.startswith("#"):
            continue
        match = _SAMPLE_RE.match(line)
        assert match, f"노출 형식이 아닌 줄: {line!r}"
        labels = tuple(
            sorted((m.group("key"), _unescape(m.group("value"))) for m in _LABEL_RE.finditer(match.group("labels") or ""))
        )
        out.setdefault(match.group("name"), {})[labels] = float(match.group("value"))
    return out


class Snapshot:
    """한 번의 스크레이프. 값 조회와 계열·라벨 전집을 낸다."""

    def __init__(self, text: str) -> None:
        self.text = text
        self.samples = parse(text)

    def value(self, name: str, **labels: str) -> float:
        series = self.samples.get(name)
        assert series is not None, f"{name} 이 노출되지 않았다"
        key = tuple(sorted(labels.items()))
        assert key in series, f"{name}{dict(labels)} 계열이 없다 (있는 것: {sorted(series)[:8]}...)"
        return series[key]

    def has(self, name: str, **labels: str) -> bool:
        return tuple(sorted(labels.items())) in self.samples.get(name, {})

    def series_count(self) -> int:
        return sum(len(series) for series in self.samples.values())

    def family_names(self) -> set[str]:
        """``_bucket``·``_sum``·``_count`` 접미를 접은 패밀리 이름 집합."""
        return {re.sub(r"_(bucket|sum|count)$", "", name) for name in self.samples}

    def label_names(self) -> set[str]:
        return {key for series in self.samples.values() for labels in series for key, _ in labels}

    def label_values(self, key: str) -> set[str]:
        return {
            value
            for series in self.samples.values()
            for labels in series
            for name, value in labels
            if name == key
        }


def assert_label_universe(snapshot: Snapshot) -> None:
    """패밀리·라벨 이름·라벨 값이 전부 닫힌 전집 안에 있다(AC4 — 바늘 찾기가 아니라 전집 대조)."""
    assert snapshot.family_names() == FAMILIES, f"패밀리가 {sorted(snapshot.family_names())} — 기대 {sorted(FAMILIES)}"
    assert snapshot.label_names() == LABEL_NAMES, f"라벨 이름 집합이 {sorted(snapshot.label_names())} — 기대 {sorted(LABEL_NAMES)}"
    for key, allowed in CLOSED_VALUES.items():
        strays = snapshot.label_values(key) - allowed
        assert not strays, f"라벨 {key} 에 서버 상수 밖의 값이 있다: {sorted(strays)}"
    for bound in snapshot.label_values("le"):
        assert bound == "+Inf" or float(bound) >= 0, f"le 가 버킷 경계가 아니다: {bound!r}"


def scrape(url: str) -> Snapshot:
    response = httpx.get(f"{url}/metrics", timeout=10.0)
    response.raise_for_status()
    return Snapshot(response.text)


def delta(before: Snapshot, after: Snapshot, name: str, **labels: str) -> float:
    return after.value(name, **labels) - before.value(name, **labels)


def _free_to_bind(port: int) -> bool:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
        probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        try:
            probe.bind(("127.0.0.1", port))
        except OSError:
            return False
    return True


def _wait_for_answer(proc: subprocess.Popen, url: str, ready_path: str, timeout: float, what: str) -> None:
    deadline = time.monotonic() + timeout
    last_exc: Exception | None = None
    while time.monotonic() < deadline:
        if proc.poll() is not None:
            stderr = (proc.stderr.read() if proc.stderr else "") or ""
            raise RuntimeError(f"port-forward to {what} exited ({proc.returncode}): {stderr.strip()}")
        try:
            httpx.get(f"{url}{ready_path}", timeout=2.0)
            return
        except httpx.HTTPError as exc:
            last_exc = exc
        time.sleep(0.5)
    raise RuntimeError(
        f"port-forward to {what} never answered {ready_path} within {timeout:.0f}s"
        + (f" (last error: {last_exc})" if last_exc else "")
    )


@contextlib.contextmanager
def pod_port_forward(
    namespace: str,
    deployment: str,
    remote_port: int,
    local_port: int,
    ready_path: str | None,
    timeout: float = 60.0,
) -> Iterator[str]:
    """``deploy/<deployment>`` 의 파드 포트로 포워드를 열고 base URL 을 넘긴다.

    ``ready_path`` 가 있으면 그 경로가 (상태 코드와 무관하게) **응답**할 때까지 기다린다 —
    ``kubectl port-forward`` 는 로컬 포트를 먼저 열고 파드 연결은 첫 요청에서 맺으므로 TCP
    연결 성공은 준비 신호가 아니다(``_helpers.port_forward`` 와 같은 독법). ``None`` 이면
    기다리지 않는다 — 리스너가 **없어야 하는** 배포(``METRICS_DISABLED``)를 재는 호출자가 쓴다.
    """
    assert _free_to_bind(local_port), f"local port {local_port} is already in use; another port-forward is live"
    proc = subprocess.Popen(
        ["kubectl", "-n", namespace, "port-forward", f"deploy/{deployment}", f"{local_port}:{remote_port}"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        text=True,
    )
    url = f"http://127.0.0.1:{local_port}"
    try:
        if ready_path is not None:
            _wait_for_answer(proc, url, ready_path, timeout, f"deploy/{deployment} in {namespace}")
        yield url
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=10)


@contextlib.contextmanager
def metrics_url(namespace: str, deployment: str, local_port: int) -> Iterator[str]:
    """배포의 메트릭 리스너 로컬 URL(``/metrics`` 가 응답할 때까지 기다린 뒤)."""
    with pod_port_forward(namespace, deployment, METRICS_PORT, local_port, "/metrics") as url:
        yield url
