# 테스트 문서: opensearch_document_delete

## 검증 대상 AC
- AC1: 단일 문서 삭제 (PRD: opensearch_document_delete)
- AC2: 부재 문서의 명확한 처리 (PRD: opensearch_document_delete)
- AC3: 파괴적 작업 표기 (PRD: opensearch_document_delete)
- AC4: AssumeRole·SigV4 접근 (PRD: opensearch_document_delete)
- AC5: 미설정 시 graceful 거부 (PRD: opensearch_document_delete)

## 테스트 시나리오

### 시나리오 1: 지정 문서만 삭제
- **사전 조건**: OpenSearch 호환 픽스처의 `notes` 인덱스에 문서 2건(`n1`, `n2`) 시드
- **실행 단계**: `index="notes"`, `id="n1"`로 삭제 호출 후 검색
- **기대 결과**: deleted 반환. refresh 이후 검색에서 `n1`은 미노출, `n2`는 유지.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/opensearch/opensearch_test.go`
  (`TestDeleteDocumentDeleted`) + 통합
  `tests/integration/opensearch_document_delete_ac1.py`
  (같은 인덱스·같은 질의 토큰을 공유하는 문서 2건 중 삭제 대상만 사라지고 나머지는 유지).

### 시나리오 2: 없는 문서 삭제 → not_found
- **사전 조건**: `notes` 인덱스에 `ghost` id 부재
- **실행 단계**: `id="ghost"` 삭제 호출, 이어서 같은 호출 반복
- **기대 결과**: not_found가 명확한 결과로 반환되고 서버·다른 도구는 정상. 반복 호출도 동일
  결과(멱등).
- **검증 AC**: AC2
- **자동화**: Go 단위 `internal/opensearch/opensearch_test.go`
  (`TestDeleteDocumentMissingMapsToNotFound`) + `internal/server/mcp_test.go`
  (`TestOpenSearchDocumentDeleteReportsNotFound`) + 통합
  `tests/integration/opensearch_document_delete_ac2.py`
  (미생성 인덱스의 id·삭제된 id 반복 호출이 모두 not_found로 수렴 + 직후 ping 정상).

### 시나리오 3: destructiveHint 광고
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 호출
- **기대 결과**: opensearch_document_delete 어노테이션이 destructiveHint=true.
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/server/mcp_test.go`
  (`TestToolsListAdvertisesOpenSearchDocumentDelete`).

### 시나리오 4: AssumeRole → SigV4 경로(정적 키 없음)
- **사전 조건**: 베이스 자격증명은 기본 체인, `OPENSEARCH_ROLE_ARN` 설정
- **실행 단계**: 삭제 호출 후 접근 경로 확인
- **기대 결과**: 기본 체인 → STS AssumeRole → 단명 자격증명으로 SigV4(service `aoss`) 서명
  요청. 정적 키 미사용.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/opensearch/opensearch_test.go` (서명 경로는 3도구 공통
  `do()` — `TestSearchSignsRequestWithAssumedRoleCreds`). 통합
  `opensearch_document_delete_ac4.py` — 픽스처가 서명
  유무를 구분하지 못하므로 `tests/k8s/kind/http-trace.yaml` 프록시 기록을 읽어,
  `DELETE /<index>/_doc/<id>`가 `aoss` 스코프로 서명됐고 그 키가 STS가 내준 키(≠ 베이스 키)이며
  세션 토큰을 달고 있음을 단정한다(문서를 먼저 시드해 실제 삭제 요청을 관측).

### 시나리오 5: 미설정 시 도구 에러
- **사전 조건**: OpenSearch 관련 env 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러
- **검증 AC**: AC5
- **자동화**: Go 단위 `internal/opensearch/opensearch_test.go`
  (`TestUnavailableFailsEveryCall`, `TestFromEnvUnsetEndpointReturnsNil`) +
  `internal/server/mcp_test.go` (`TestOpenSearchUnavailableReturnsToolError`) + 통합
  `tests/integration/opensearch_document_delete_ac5.py::test_opensearch_document_delete_ac5_unconfigured_refusal`
  (자격증명 미부착 배포 변형에서 unavailable 도구 에러 반환 + 직후 ping 정상).
