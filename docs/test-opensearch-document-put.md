# 테스트 문서: opensearch_document_put

## 검증 대상 AC
- AC1: 문서 색인·업서트 (PRD: opensearch_document_put)
- AC2: 인덱스 자동 생성 (PRD: opensearch_document_put)
- AC3: 파괴적 작업 표기 (PRD: opensearch_document_put)
- AC4: AssumeRole·SigV4 접근 (PRD: opensearch_document_put)
- AC5: 미설정 시 graceful 거부 (PRD: opensearch_document_put)

## 테스트 시나리오

### 시나리오 1: 색인·업서트·자동 id
- **사전 조건**: OpenSearch 호환 픽스처의 `notes` 인덱스 존재
- **실행 단계**: `id="n1"`로 색인 → 본문을 바꿔 같은 id로 재색인 → id 미지정으로 2회 색인
- **기대 결과**: 최초 색인은 created, 재색인은 updated이며 refresh 이후 검색에서 새 본문이
  조회. id 미지정 호출 2회는 서로 다른 자동 id를 반환.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/opensearch/opensearch_test.go`
  (`TestPutDocumentWithIDUpserts`, `TestPutDocumentReportsUpdated`,
  `TestPutDocumentWithoutIDAutoGenerates`) + 통합
  `tests/integration/opensearch_document_put_ac1.py`
  (created→updated·업서트 후 검색이 돌려주는 본문이 두 번째 본문·자동 id 2회 상이 +
  최종 문서 3건).

### 시나리오 2: 미존재 인덱스 자동 생성
- **사전 조건**: `troubleshooting-2026` 인덱스 부재
- **실행 단계**: 해당 인덱스명으로 문서 색인
- **기대 결과**: 인덱스가 자동 생성되고, refresh 이후 그 인덱스에서 문서가 검색된다.
- **검증 AC**: AC2
- **자동화**: 통합
  `tests/integration/opensearch_document_put_ac2.py`
  — 실행·케이스마다 새 인덱스명(`ci-put-ac2-<RUN_ID>`)을 쓰고, 색인 **전** 그 인덱스
  검색이 404 `index_not_found_exception`으로 거부되는 것을 선행 관측한 뒤 색인 후 검색을
  확인한다(사전 부재를 단정해야 자동 생성의 증거가 된다).

### 시나리오 3: destructiveHint 광고
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 호출
- **기대 결과**: opensearch_document_put 어노테이션이 destructiveHint=true.
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/server/mcp_test.go`
  (`TestToolsListAdvertisesOpenSearchDocumentPut`).

### 시나리오 4: AssumeRole → SigV4 경로(정적 키 없음)
- **사전 조건**: 베이스 자격증명은 기본 체인, `OPENSEARCH_ROLE_ARN` 설정
- **실행 단계**: 색인 호출 후 접근 경로 확인
- **기대 결과**: 기본 체인 → STS AssumeRole → 단명 자격증명으로 SigV4(service `aoss`) 서명
  요청. 정적 키 미사용.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/opensearch/opensearch_test.go` (서명 경로는 3도구 공통
  `do()` — `TestSearchSignsRequestWithAssumedRoleCreds`). 통합
  `opensearch_document_put_ac4.py` — 픽스처는 서명 유무를
  구분하지 못하므로 `tests/k8s/kind/http-trace.yaml` 프록시 기록을 읽어,
  `PUT /<index>/_doc/<id>`가 `aoss` 스코프로 서명됐고 그 키가 STS가 내준 키(≠ 베이스 키)이며
  세션 토큰을 달고 있음을 단정한다.

### 시나리오 5: 미설정 시 도구 에러
- **사전 조건**: OpenSearch 관련 env 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러
- **검증 AC**: AC5
- **자동화**: Go 단위 `internal/opensearch/opensearch_test.go`
  (`TestUnavailableFailsEveryCall`, `TestFromEnvUnsetEndpointReturnsNil`) +
  `internal/server/mcp_test.go` (`TestOpenSearchUnavailableReturnsToolError`) + 통합
  `tests/integration/no_config.py::test_opensearch_document_put_ac5_unconfigured_refusal`
  (자격증명 미부착 배포 변형에서 unavailable 도구 에러 반환 + 직후 ping 정상).
