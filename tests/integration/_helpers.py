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
async def open_session(url: str) -> AsyncIterator[ClientSession]:
    """Open an MCP ClientSession against ``<url>/mcp`` (skips initialize)."""
    mcp_url = f"{url}/mcp"
    async with streamablehttp_client(mcp_url) as (read, write, _):
        async with ClientSession(read, write) as session:
            yield session
