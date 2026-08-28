"""End-to-end check for aws_config_get against the MinIO fixture.

검증 AC: aws-config-get/AC1, aws-config-get/AC2
실행 대상: primary
추가 인자: trace

**분할 대기(2 AC 겸용)** — 모델 `tbm_homelab-k3s-mcp-ac-e2e`의 파일 단위 규칙 2는
파일 하나가 AC 하나만 주검증할 것을 요구하므로, 이 파일은 AC별 전용 파일로 쪼개질 대상이다.
위 선언은 현재 겸용 상태를 있는 그대로 신고하는 것이고,
`tests/integration/check_ac_mapping.py`가 이를 규칙 2 위반으로 계수해 `docs/doc-tracker.md`와 대조한다.

The server assumes AWS_CONFIG_ROLE_ARN against MinIO's STS endpoint and reads
s3://ci-config-bucket/aws/config (seeded by tests/k8s/kind/minio.yaml) with the
resulting credentials.

Per-AC case names + docstrings declare the AC they verify (registry rule 3);
``docs/doc-tracker.md`` is the AC<->case mapping SSOT.

The access path itself (AC2, "정적 키 미사용") is not observable from the tool
response: MinIO accepts the base credentials the CI secret carries just as
readily as assumed-role ones, so a successful call proves only that *some*
credential worked. ``tests/k8s/kind/http-trace.yaml`` supplies the missing
observation point — a recording proxy in front of MinIO — and
``test_aws_config_get_ac2_assume_role_access`` reads it to assert which
credential actually signed the GetObject.
"""

from __future__ import annotations

import asyncio
import datetime
import re

from _helpers import (
    assert_assumed_role_access,
    base_url,
    fetch_trace,
    open_session,
    trace_url,
    wait_for_healthz,
)

# Must match tests/k8s/kind/minio.yaml: the seed ConfigMap and the bucket/key
# the minio-seed Job uploads to.
EXPECTED_BUCKET = "ci-config-bucket"
EXPECTED_KEY = "aws/config"
EXPECTED_CONTENT = "[default]\nregion = ap-northeast-2\noutput = json\n"

# Must match the aws-config secret in .github/workflows/ci.yml.
ROLE_ARN = "arn:aws:iam::000000000000:role/ci-config-reader"
REGION = "us-east-1"

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


async def test_aws_config_get_ac2_assume_role_access(session, trace) -> None:
    """AC: aws-config-get/AC2 — the object is read with assumed-role credentials, not static keys.

    Reads the http-trace recording rather than the tool response, because the
    tool response cannot tell the two apart: MinIO honours the static
    ``AWS_ACCESS_KEY_ID`` in the CI secret exactly as readily as the assumed
    role's temporary key, so "the call succeeded" is true either way.

    What is asserted is the access path — an AssumeRole for the configured
    role ARN was issued and signed with the base credential, and the GetObject
    that followed was signed with a key STS handed back (never the base key)
    and carried a session token. A server that read the object with the static
    keys, or with no signature at all, fails all of it.
    """
    await session.call_tool("aws_config_get", {})

    record = assert_assumed_role_access(
        fetch_trace(trace),
        role_arn=ROLE_ARN,
        upstream="minio",
        method="GET",
        path=f"/{EXPECTED_BUCKET}/{EXPECTED_KEY}",
        service="s3",
        region=REGION,
    )
    assert record["status"] == 200, record

    print(
        "aws_config_get assume-role path ok ->",
        f"GET {record['path']} signed by {record['sigv4']['accessKeyId']}"
        f" (base key never used on the data plane)",
    )


async def run() -> None:
    url = base_url()
    trace = trace_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- aws_config_get fixed object (AC: aws-config-get/AC1) ---")
        await test_aws_config_get_ac1_fixed_object(session)

        print("--- aws_config_get assume-role access (AC: aws-config-get/AC2) ---")
        await test_aws_config_get_ac2_assume_role_access(session, trace)


if __name__ == "__main__":
    asyncio.run(run())
