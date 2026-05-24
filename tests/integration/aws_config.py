"""End-to-end check for aws_config_get against the MinIO fixture.

The server assumes AWS_CONFIG_ROLE_ARN against MinIO's STS endpoint and reads
s3://ci-config-bucket/aws/config (seeded by tests/k8s/kind/minio.yaml) with the
resulting credentials. This exercises the full assume-role -> GetObject path.
"""

from __future__ import annotations

import asyncio

from _helpers import base_url, open_session, wait_for_healthz

# Must match tests/k8s/kind/minio.yaml: the seed ConfigMap and the bucket/key
# the minio-seed Job uploads to.
EXPECTED_BUCKET = "ci-config-bucket"
EXPECTED_KEY = "aws/config"
EXPECTED_CONTENT = "[default]\nregion = ap-northeast-2\noutput = json\n"


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        print("--- aws_config_get ---")
        result = await session.call_tool("aws_config_get", {})
        assert result.isError is False, result

        structured = result.structuredContent
        assert structured is not None, result
        assert structured["bucket"] == EXPECTED_BUCKET, structured
        assert structured["key"] == EXPECTED_KEY, structured
        assert structured["content"] == EXPECTED_CONTENT, structured
        assert structured["size"] == len(EXPECTED_CONTENT.encode()), structured

        assert result.content, result
        block = result.content[0]
        assert block.type == "text", block
        assert block.text == EXPECTED_CONTENT, block.text

        print(
            "aws_config_get ok ->",
            f"s3://{structured['bucket']}/{structured['key']}",
            f"({structured['size']} bytes, etag={structured.get('etag')!r})",
        )


if __name__ == "__main__":
    asyncio.run(run())
