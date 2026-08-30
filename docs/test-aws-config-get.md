# 테스트 문서: aws_config_get

## 검증 대상 AC
- AC1: 고정 객체 조회 (PRD: aws_config_get)
- AC2: 정적 키 미사용 (PRD: aws_config_get)
- AC3: 미설정 시 graceful 거부 (PRD: aws_config_get)

## 테스트 시나리오

### 시나리오 1: 고정 객체 내용·메타데이터 반환
- **사전 조건**: MinIO 픽스처에 `s3://ci-config-bucket/aws/config` 시드
- **실행 단계**: 인자 없이 호출
- **기대 결과**: structuredContent에 bucket=ci-config-bucket, key=aws/config, 시드 내용,
  size, etag 반환. 텍스트 블록은 객체 내용과 일치.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/awsconfig/awsconfig_test.go::TestGetConfigMapsObjectAndMetadata`,
  `mcp_test.go::TestAWSConfigGetDispatchesToService`. 통합
  `aws_config_get_ac1.py`(내용·size + contentType·ETag 모양·
  lastModified RFC3339 파싱까지 단정, 텍스트 블록 = 객체 내용).

### 시나리오 2: AssumeRole → GetObject 경로(정적 키 없음)
- **사전 조건**: 동일(서버가 MinIO STS로 AWS_CONFIG_ROLE_ARN AssumeRole)
- **실행 단계**: 호출 후 접근 경로 확인
- **기대 결과**: 기본 자격증명 체인 → STS AssumeRole → 단명 자격증명으로 GetObject. 정적 키
  미사용. GetObject 실패는 에러로 래핑.
- **검증 AC**: AC2
- **자동화**: Go 단위 `awsconfig_test.go`(assume-role 배선·
  `TestGetConfigWrapsGetObjectError`). 통합
  `aws_config_get_ac2.py` — MinIO는 베이스
  자격증명(minioadmin)도 똑같이 받아주므로 도구 응답으로는 구분되지 않는다. 대신
  `tests/k8s/kind/http-trace.yaml` 프록시 기록에서 (1) 설정된 role ARN의 AssumeRole이
  베이스 자격증명으로 서명돼 발급됐고 (2) 뒤이은 GetObject가 STS가 내준 키로 서명됐으며
  베이스 키가 아니고 (3) 세션 토큰을 달고 있음을 단정한다.

### 시나리오 3: 미설정 시 도구 에러
- **사전 조건**: AWS config env 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러
- **검증 AC**: AC3
- **자동화**: Go 단위 `awsconfig_test.go::TestUnavailableGetConfig`,
  `TestFromEnvUnsetBucketReturnsNil`, `TestFromEnvRequiresKeyAndRole`. 통합
  `tests/integration/no_config.py::test_aws_config_get_ac3_unconfigured_refusal`
  (자격증명 미부착 배포 변형에서 unavailable 도구 에러 반환 + 직후 ping 정상).
