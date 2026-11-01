"""drop_token_issue: 도구의 입력 스키마와 결과 어디에도 파일 내용 자리가 없다 (e2e).

검증 시나리오: test-drop-token-issue.md#시나리오 2
실행 대상: primary

파일을 실제로 올린 뒤에도 그 바이트가 도구 결과로 돌아오지 않는다는 것을 재야 헛통과가 아니다 —
아무것도 올리지 않은 drop 에서 「결과에 내용이 없다」는 당연하다.
"""

from __future__ import annotations

import asyncio
import json
import os
import tempfile
import uuid

from mcp import ClientSession
from mcp.shared.exceptions import McpError

from _drop import RESULT_KEYS, TOOL, drop_url, issue, run_template
from _helpers import base_url, open_session, wait_for_healthz


async def test_drop_token_issue_sc2_schema_has_no_content_parameter(session: ClientSession) -> None:
    tools = {tool.name: tool for tool in (await session.list_tools()).tools}
    schema = tools[TOOL].inputSchema
    assert set(schema["properties"]) == {"valid_minutes", "max_days", "label"}, schema["properties"]
    assert schema.get("additionalProperties") is False, schema
    assert "never sends or receives file contents" in tools[TOOL].description, tools[TOOL].description

    for name in ("file", "content"):
        try:
            result = await session.call_tool(TOOL, {name: "aGVsbG8="})
        except McpError as exc:
            assert "unknown argument" in str(exc), exc
        else:
            raise AssertionError(f"{TOOL} accepted a {name!r} argument: {result}")


async def test_drop_token_issue_sc2_result_carries_no_file_bytes(session: ClientSession) -> None:
    sentinel = f"drop-e2e-sentinel-{uuid.uuid4().hex}"
    first = await issue(session, {"label": "e2e 내용 비노출"})
    with drop_url() as local, tempfile.TemporaryDirectory() as work:
        path = os.path.join(work, "note.txt")
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(sentinel)
        uploaded = json.loads(
            run_template(first["usage"]["upload"], local, first["token"], work, **{"./path/to/file": path})
        )
        assert sentinel not in json.dumps(uploaded), "drop's own upload answer echoed the content"

        result = await session.call_tool(TOOL, {"label": uploaded["id"]})
        assert result.isError is False, result
        assert set(result.structuredContent) == RESULT_KEYS, result.structuredContent
        rendered = json.dumps(result.structuredContent) + "".join(
            getattr(block, "text", "") for block in result.content
        )
        assert sentinel not in rendered, "the tool result carried the uploaded file's contents"
        for key, value in result.structuredContent["usage"].items():
            assert isinstance(value, str), f"usage.{key} is not a command string: {value!r}"

        run_template(first["usage"]["delete"], local, first["token"], work, **{"<id>": uploaded["id"]})


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url, timeout=60.0) as session:
        print("--- no file content in schema or result (AC: drop-token-issue/AC3) ---")
        await test_drop_token_issue_sc2_schema_has_no_content_parameter(session)
        await test_drop_token_issue_sc2_result_carries_no_file_bytes(session)
        print("ok: drop-token-issue 시나리오 2")


if __name__ == "__main__":
    asyncio.run(run())
