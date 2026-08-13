"""End-to-end check for aws_config_get against the MinIO fixture.

The server assumes AWS_CONFIG_ROLE_ARN against MinIO's STS endpoint and reads
s3://ci-config-bucket/aws/config (seeded by tests/k8s/kind/minio.yaml) with the
resulting credentials.

Per-AC case names + docstrings declare the AC they verify (registry rule 3);
``docs/doc-tracker.md`` is the AC<->case mapping SSOT.

What this file does NOT assert is the assume-role access path itself
(aws-config-get/AC2, "정적 키 미사용"). The call succeeding shows *some*
credential worked against MinIO, but MinIO accepts the base credentials the CI
secret already carries just as readily as the assumed-role ones, so no
observation available here distinguishes the two. That AC is tracked as ⬜ in
``docs/doc-tracker.md`` until the fixture grows an observation point; the
assume-role wiring itself is asserted by the unit tests in
``internal/awsconfig/awsconfig_test.go``, which are outside this loop's e2e
scope.
"""

from __future__ import annotations

import asyncio
import datetime
import re

from _helpers import base_url, open_session, wait_for_healthz

# Must match tests/k8s/kind/minio.yaml: the seed ConfigMap and the bucket/key
# the minio-seed Job uploads to.
EXPECTED_BUCKET = "ci-config-bucket"
EXPECTED_KEY = "aws/config"
EXPECTED_CONTENT = "[default]\nregion = ap-northeast-2\noutput = json\n"

# GetConfig strips the quotes S3 wraps around an ETag before returning it
# (internal/awsconfig/awsconfig.go), leaving the bare hex digest — optionally
# with a "-<parts>" suffix for a multipart upload.
ETAG_PATTERN = re.compile(r"[0-9a-f]{32}(-\d+)?")


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
        print("--- aws_config_get fixed object (AC: aws-config-get/AC1) ---")
        await test_aws_config_get_ac1_fixed_object(session)


if __name__ == "__main__":
    asyncio.run(run())
