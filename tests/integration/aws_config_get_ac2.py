"""Deployed-server e2e for aws-config-get/AC2 (정적 키 미사용).

검증 AC: aws-config-get/AC2
실행 대상: primary
추가 인자: trace

MinIO 는 CI 시크릿의 베이스 자격증명도 그대로 받아주므로 "호출이 성공했다"로는
구분되지 않는다 — http-trace 프록시의 기록으로 서명 키를 관측한다.
"""

from __future__ import annotations

import asyncio

from _helpers import (
    assert_assumed_role_access, base_url, fetch_trace, open_session, trace_url,
    wait_for_healthz,
)
from _aws_config import EXPECTED_BUCKET, EXPECTED_KEY, REGION, ROLE_ARN


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
        print("--- aws-config-get/AC2 ---")
        await test_aws_config_get_ac2_assume_role_access(session, trace)
        print("ok: aws-config-get/AC2")


if __name__ == "__main__":
    asyncio.run(run())
