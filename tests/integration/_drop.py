"""drop_token_issue e2e 의 공유 표면 (매칭 단위가 아니다).

러너는 클러스터 밖에 있어 drop 의 클러스터 주소(``base_url``)에 닿지 못한다. 그래서 파일마다 drop
Service 로 짧게 포트포워드를 열고, 도구가 돌려준 curl 견본의 ``base_url`` 만 그 로컬 주소로 바꿔
**견본 그대로** 셸에서 돌린다 — 견본이 실제로 동작하는지가 이 도구 계약의 절반이기 때문이다.
"""

from __future__ import annotations

import contextlib
import os
import socket
import subprocess
from collections.abc import Iterator
from typing import Any

from _helpers import port_forward

TOOL = "drop_token_issue"
NAMESPACE = "drop"
SERVICE = "drop"

# tests/k8s/kind/drop.yaml 의 PUBLIC_BASE_URL 과 같아야 한다.
CLUSTER_BASE_URL = "http://drop.drop.svc.cluster.local"

RESULT_KEYS = {"token", "expires_at", "max_days", "base_url", "created_by", "usage"}


def _ephemeral_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
        probe.bind(("127.0.0.1", 0))
        return probe.getsockname()[1]


@contextlib.contextmanager
def drop_url() -> Iterator[str]:
    with port_forward(NAMESPACE, SERVICE, 80, _ephemeral_port(), ready_path="/healthz") as url:
        yield url


async def issue(session, arguments: dict[str, Any] | None = None) -> dict[str, Any]:
    result = await session.call_tool(TOOL, arguments or {})
    assert result.isError is False, f"{TOOL} failed: {result}"
    payload = result.structuredContent
    assert payload is not None, result
    assert set(payload) == RESULT_KEYS, f"result keys = {sorted(payload)}"
    assert payload["base_url"] == CLUSTER_BASE_URL, payload["base_url"]
    return payload


def run_template(template: str, local_base: str, token: str, cwd: str, **fills: str) -> str:
    """견본 한 줄을 셸로 돌리고 표준 출력을 돌려준다. ``<id>`` 같은 자리는 ``fills`` 로 채운다."""
    command = template.replace(CLUSTER_BASE_URL, local_base)
    for key, value in fills.items():
        command = command.replace(key, value)
    proc = subprocess.run(
        ["bash", "-c", command],
        cwd=cwd,
        env={**os.environ, "DROP_TOKEN": token},
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert proc.returncode == 0, (
        f"template failed ({proc.returncode}): {command}\nstderr: {proc.stderr.strip()}"
    )
    return proc.stdout
