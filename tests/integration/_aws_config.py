"""aws-config-get e2e 픽스처의 공유 상수 (매칭 단위가 아니다).

`_` 접두 파일이라 `run_all.py`·`check_ac_mapping.py` 모두 매칭 단위에서 제외한다
(AC를 주검증하지 않으므로 `검증 AC:` 선언도 갖지 않는다).

aws-config-get/AC1(고정 객체 조회)·AC2(정적 키 미사용) 전용 파일이 같은 버킷/키를
가리켜야 하므로 한곳에 둔다.

서버는 AWS_CONFIG_ROLE_ARN 을 MinIO 의 STS 엔드포인트에 대해 assume 하고, 그 자격
증명으로 s3://ci-config-bucket/aws/config(tests/k8s/kind/minio.yaml 의 minio-seed
Job 이 업로드)를 읽는다.
"""

from __future__ import annotations

import re

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
