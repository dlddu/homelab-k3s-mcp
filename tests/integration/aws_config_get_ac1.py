"""Deployed-server e2e for aws-config-get/AC1 (고정 객체 조회).

검증 AC: aws-config-get/AC1
실행 대상: primary

서버에 고정된 버킷/키의 내용과 메타데이터를 읽는다(픽스처는
``tests/k8s/kind/minio.yaml`` 의 minio-seed Job 이 올린 객체).

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import asyncio
import datetime

from _helpers import base_url, open_session, wait_for_healthz
from _aws_config import ETAG_PATTERN, EXPECTED_BUCKET, EXPECTED_CONTENT, EXPECTED_KEY


async def test_aws_config_get_ac1_fixed_object(session) -> None:
    """AC: aws-config-get/AC1 — the server-pinned object comes back with its metadata.

    Asserts every field the AC names: the content of the pinned bucket/key plus
    size, content type, ETag and last-modified. ``size`` is checked against the
    byte length of the content actually returned (not a constant), the ETag has
    to be the quote-stripped digest shape the tool promises, and last-modified
    has to parse as an instant that is not in the future — a passed-through
    placeholder string would fail all three.
    """
    result = await session.call_tool("aws_config_get", {})
    assert result.isError is False, result

    structured = result.structuredContent
    assert structured is not None, result
    assert structured["bucket"] == EXPECTED_BUCKET, structured
    assert structured["key"] == EXPECTED_KEY, structured
    assert structured["content"] == EXPECTED_CONTENT, structured
    assert structured["size"] == len(EXPECTED_CONTENT.encode()), structured

    etag = structured.get("etag")
    assert etag and ETAG_PATTERN.fullmatch(etag), structured

    content_type = structured.get("contentType")
    assert content_type, structured

    last_modified = structured.get("lastModified")
    assert last_modified, structured
    modified_at = datetime.datetime.fromisoformat(last_modified.replace("Z", "+00:00"))
    assert modified_at.tzinfo is not None, last_modified
    now = datetime.datetime.now(datetime.timezone.utc)
    assert modified_at <= now + datetime.timedelta(minutes=5), (last_modified, now)

    # The text block is the object itself, so a client that ignores structured
    # content still gets the config verbatim.
    assert result.content, result
    block = result.content[0]
    assert block.type == "text", block
    assert block.text == EXPECTED_CONTENT, block.text

    print(
        "aws_config_get ok ->",
        f"s3://{structured['bucket']}/{structured['key']}",
        f"({structured['size']} bytes, {content_type}, etag={etag},"
        f" modified={last_modified})",
    )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- aws-config-get/AC1 ---")
        await test_aws_config_get_ac1_fixed_object(session)
        print("ok: aws-config-get/AC1")


if __name__ == "__main__":
    asyncio.run(run())
