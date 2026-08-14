"""Shared helpers for the homelab-k3s-mcp Python integration tests.

The server exposes an MCP-flavoured JSON-RPC endpoint at ``/mcp``, but it
does not yet implement the full Streamable HTTP transport contract: in
particular it answers JSON-RPC notifications (e.g. ``notifications/initialized``
sent by ``ClientSession.initialize``) with a JSON-RPC error body and HTTP 200
instead of HTTP 202 No Content. That body fails Pydantic validation inside the
SDK's read loop and aborts the session.

To stay on the official ``mcp`` Python package while still exercising the live
server, these tests open a ``ClientSession`` over ``streamablehttp_client`` but
skip ``session.initialize()``. The server's dispatch table accepts
``tools/list`` and ``tools/call`` without prior initialization, so the resulting
calls flow through the SDK exactly the way a real client would issue them.
"""

from __future__ import annotations

import contextlib
import os
import sys
import time
from collections.abc import AsyncIterator
from typing import Any

import httpx
from mcp import ClientSession
from mcp.client.streamable_http import streamablehttp_client


def base_url() -> str:
    """Return the MCP base URL from argv[1] or ``MCP_BASE_URL``."""
    if len(sys.argv) > 1 and sys.argv[1]:
        return sys.argv[1].rstrip("/")
    return os.environ.get("MCP_BASE_URL", "http://127.0.0.1:8080").rstrip("/")


def trace_url() -> str:
    """Return the http-trace admin URL from argv[2] or ``MCP_TRACE_URL``.

    Only the tests that assert an access path need this; the fixture is
    ``tests/k8s/kind/http-trace.yaml``.
    """
    if len(sys.argv) > 2 and sys.argv[2]:
        return sys.argv[2].rstrip("/")
    return os.environ.get("MCP_TRACE_URL", "http://127.0.0.1:8090").rstrip("/")


def wait_for_healthz(url: str, timeout: float = 30.0) -> None:
    """Block until ``GET <url>/healthz`` responds 200, then return."""
    deadline = time.monotonic() + timeout
    last_exc: Exception | None = None
    while time.monotonic() < deadline:
        try:
            response = httpx.get(f"{url}/healthz", timeout=2.0)
            if response.status_code == 200:
                return
        except httpx.HTTPError as exc:
            last_exc = exc
        time.sleep(1)
    raise RuntimeError(
        f"healthz never became available at {url} within {timeout:.0f}s"
        + (f" (last error: {last_exc})" if last_exc else "")
    )


def get_json(url: str, path: str) -> dict[str, Any]:
    response = httpx.get(f"{url}{path}", timeout=5.0)
    response.raise_for_status()
    return response.json()


@contextlib.asynccontextmanager
async def open_session(
    url: str, headers: dict[str, str] | None = None
) -> AsyncIterator[ClientSession]:
    """Open an MCP ClientSession against ``<url>/mcp`` (skips initialize).

    ``headers`` are attached to every HTTP request the transport makes, letting
    auth-gated deployments be exercised with an ``Authorization`` header.
    """
    mcp_url = f"{url}/mcp"
    async with streamablehttp_client(mcp_url, headers=headers) as (read, write, _):
        async with ClientSession(read, write) as session:
            yield session


async def assert_destructive_annotation(
    session: ClientSession, tool_name: str
) -> None:
    """Assert the deployed server advertises ``tool_name`` as a destructive tool.

    Fetches ``tools/list`` from the live server and checks the MCP annotations
    that encode "파괴적 작업 표기" (destructive-operation marking): the tool must
    advertise ``destructiveHint == True`` and ``readOnlyHint == False``. This
    promotes the in-process assertion in ``internal/server/mcp_test.go``
    (``TestToolsListAdvertisesAnnotations``) to the deployed-server e2e layer
    without ever executing the destructive operation itself.
    """
    tools = await session.list_tools()
    by_name = {tool.name: tool for tool in tools.tools}
    assert tool_name in by_name, (
        f"{tool_name} not advertised by tools/list: {sorted(by_name)}"
    )
    annotations = by_name[tool_name].annotations
    assert annotations is not None, f"{tool_name} advertises no annotations"
    assert annotations.destructiveHint is True, (
        f"{tool_name} destructiveHint = {annotations.destructiveHint!r}, expected True"
    )
    assert annotations.readOnlyHint is False, (
        f"{tool_name} readOnlyHint = {annotations.readOnlyHint!r}, expected False"
    )


# --- http-trace: asserting the access path, not just its outcome -------------
#
# The MinIO and OpenSearch fixtures cannot tell an assumed-role request apart
# from one signed with the base credentials (MinIO accepts both) or from one
# that is not signed at all (the OpenSearch fixture runs with the security
# plugin disabled). So an e2e assertion built only on "the call succeeded"
# passes just as happily against a server that never assumes a role — it is
# vacuous with respect to the AssumeRole/SigV4 ACs.
#
# tests/k8s/kind/http-trace.yaml puts a recording proxy in front of both
# upstreams. The helpers below read that recording so a test can assert which
# credential actually signed a request.

# The static key pair the CI secret carries (see the aws-config secret in
# .github/workflows/ci.yml). It is the base credential: legitimate for the
# AssumeRole call itself, and exactly what must NOT appear on a data-plane
# request.
BASE_ACCESS_KEY_ID = "minioadmin"


def fetch_trace(url: str | None = None) -> list[dict[str, Any]]:
    """Return every request the http-trace proxy has recorded so far."""
    target = (url or trace_url()).rstrip("/")
    response = httpx.get(f"{target}/requests", timeout=10.0)
    response.raise_for_status()
    return response.json()["requests"]


def assume_role_records(
    records: list[dict[str, Any]], role_arn: str
) -> list[dict[str, Any]]:
    """Return the successful AssumeRole calls recorded for ``role_arn``."""
    return [
        record
        for record in records
        if record.get("sts")
        and record["sts"].get("roleArn") == role_arn
        and record["status"] == 200
    ]


def find_requests(
    records: list[dict[str, Any]],
    *,
    upstream: str,
    method: str,
    path: str,
) -> list[dict[str, Any]]:
    """Return the recorded requests matching an exact upstream/method/path."""
    return [
        record
        for record in records
        if record["upstream"] == upstream
        and record["method"] == method
        and record["path"] == path
    ]


def assert_assumed_role_access(
    records: list[dict[str, Any]],
    *,
    role_arn: str,
    upstream: str,
    method: str,
    path: str,
    service: str,
    region: str,
) -> dict[str, Any]:
    """Assert ``method path`` was signed by credentials assumed from ``role_arn``.

    Three separate observations have to line up, and each one rules out a
    different way of passing without ever assuming the role:

    1. **An AssumeRole for exactly this role ARN was issued** — rules out a
       server that skips STS entirely.
    2. **The data-plane request carries a SigV4 Authorization header** scoped
       to ``service``/``region`` and covering ``host`` — rules out an unsigned
       request, which the security-disabled OpenSearch fixture would otherwise
       accept without complaint.
    3. **The access key that signed it is one STS handed back, and is not the
       static base key** from the CI secret, and it travels with a session
       token — rules out signing with the long-lived base credentials, which
       MinIO would otherwise accept without complaint.

    Returns the matched data-plane record so callers can assert more.
    """
    assumed = assume_role_records(records, role_arn)
    assert assumed, (
        f"no successful AssumeRole for {role_arn} in the trace — the server "
        f"never assumed the role (recorded STS calls: "
        f"{[r['sts'] for r in records if r.get('sts')]})"
    )
    issued_keys = {
        record["sts"].get("issuedAccessKeyId")
        for record in assumed
        if record["sts"].get("issuedAccessKeyId")
    }
    assert issued_keys, f"AssumeRole for {role_arn} returned no key id: {assumed}"

    # The base credential's only job is to assume the role. Observing it on the
    # STS call (and, below, never on the data-plane call) is what makes
    # "정적 키 미사용" an observation rather than a claim.
    for sts_call in assumed:
        assert sts_call["sigv4"] is not None, (
            f"the AssumeRole call for {role_arn} was not signed at all: {sts_call}"
        )
        assert sts_call["sigv4"]["service"] == "sts", sts_call
        assert sts_call["sigv4"]["accessKeyId"] == BASE_ACCESS_KEY_ID, (
            f"AssumeRole for {role_arn} was signed with "
            f"{sts_call['sigv4']['accessKeyId']!r}, expected the base credential "
            f"{BASE_ACCESS_KEY_ID!r} from the default chain: {sts_call}"
        )

    matches = find_requests(records, upstream=upstream, method=method, path=path)
    assert matches, (
        f"no {method} {path} recorded against {upstream} — the request never "
        f"reached the traced endpoint"
    )
    record = matches[-1]

    sigv4 = record["sigv4"]
    assert sigv4 is not None, (
        f"{method} {path} carried no SigV4 Authorization header: {record}"
    )
    assert sigv4["algorithm"] == "AWS4-HMAC-SHA256", record
    assert sigv4["service"] == service, record
    assert sigv4["region"] == region, record
    assert "host" in sigv4["signedHeaders"], record

    assert sigv4["accessKeyId"] != BASE_ACCESS_KEY_ID, (
        f"{method} {path} was signed with the static base key "
        f"{BASE_ACCESS_KEY_ID!r}, not with assumed-role credentials: {record}"
    )
    assert sigv4["accessKeyId"] in issued_keys, (
        f"{method} {path} was signed with {sigv4['accessKeyId']!r}, which is "
        f"not a key STS issued for {role_arn} ({sorted(issued_keys)}): {record}"
    )
    assert record["securityToken"] is True, (
        f"{method} {path} carried no X-Amz-Security-Token, so it was not "
        f"signed with temporary credentials: {record}"
    )
    return record
