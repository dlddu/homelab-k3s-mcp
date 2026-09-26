"""Deployed-server e2e for aws-config-get/AC2 (정적 키 미사용).

검증 시나리오: test-aws-config-get.md#시나리오 2
실행 대상: primary
추가 인자: trace
병렬 레인: aws
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

    A server that read the object with the static keys, or with no signature at
    all, fails all of it.
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
