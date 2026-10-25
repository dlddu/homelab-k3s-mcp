from __future__ import annotations

from typing import Any

REASON = "e2e 시나리오가 게이트 경로를 검증하려고 부르는 호출이다."

TOOLS = frozenset(
    {
        "resource_get",
        "resource_watch",
        "resource_create",
        "resource_update",
        "resource_patch",
        "resource_delete",
        "resource_delete_collection",
        "resource_exec",
        "resource_attach",
        "resource_port_forward",
        "resource_proxy",
    }
)


def with_reason(name: str, arguments: dict[str, Any] | None) -> dict[str, Any] | None:
    if name not in TOOLS or not isinstance(arguments, dict) or "reason" in arguments:
        return arguments
    return {**arguments, "reason": REASON}


class ReasonedSession:
    def __init__(self, session: Any) -> None:
        self._session = session

    def __getattr__(self, attr: str) -> Any:
        return getattr(self._session, attr)

    async def call_tool(self, name: str, arguments: dict[str, Any] | None = None, *args: Any, **kwargs: Any) -> Any:
        return await self._session.call_tool(name, with_reason(name, arguments), *args, **kwargs)
