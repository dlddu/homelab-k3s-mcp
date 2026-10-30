# 테스트 문서: opensearch_document_put

## 검증 대상 AC
- AC1: 문서 색인·업서트 (PRD: opensearch_document_put)
- AC2: 인덱스 자동 생성 (PRD: opensearch_document_put)
- AC3: 파괴적 작업 표기 (PRD: opensearch_document_put)
- AC4: 백엔드 접근(OpenSearch REST 호환 · 인증 모드 설정) (PRD: opensearch_document_put)
- AC5: 미설정 시 graceful 거부 (PRD: opensearch_document_put)

## 테스트 시나리오

### 시나리오 1: 색인·업서트·자동 id
- **사전 조건**: docstore 픽스처의 `notes` 인덱스 존재
- **실행 단계**: `id="n1"`로 색인 → 본문을 바꿔 같은 id로 재색인 → id 미지정으로 2회 색인
- **기대 결과**: 최초 색인은 created, 재색인은 updated이며 이후 검색에서 새 본문이
  조회. id 미지정 호출 2회는 서로 다른 자동 id를 반환.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/opensearch/opensearch_test.go`
  (`TestPutDocumentWithIDUpserts`, `TestPutDocumentReportsUpdated`,
  `TestPutDocumentWithoutIDAutoGenerates`) + 통합
  `tests/integration/opensearch_document_put_sc1.py`
  (created→updated·업서트 후 검색이 돌려주는 본문이 두 번째 본문·자동 id 2회 상이 +
  최종 문서 3건).

### 시나리오 2: 미존재 인덱스 자동 생성
- **사전 조건**: `troubleshooting-2026` 인덱스 부재
- **실행 단계**: 해당 인덱스명으로 문서 색인
- **기대 결과**: 인덱스가 자동 생성되고, 그 인덱스에서 문서가 검색된다.
- **검증 AC**: AC2
- **자동화**: 통합
  `tests/integration/opensearch_document_put_sc2.py`
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

### 시나리오 4: 백엔드 접근 경로(인증 모드)
- **사전 조건**: `OPENSEARCH_ENDPOINT`가 OpenSearch REST 호환 백엔드를 가리킴(통합:
  `tests/k8s/kind/http-trace.yaml` 프록시 → `tests/k8s/kind/docstore.yaml` 실 docstore, 인증 없음 모드)
- **실행 단계**: 호출 후 접근 경로 확인
- **기대 결과**: 기본(없음) 모드는 `Authorization`·`X-Amz-Security-Token` 없이 백엔드에 도달한다.
  basic 모드는 Basic 헤더, SigV4 모드는 `es`|`aoss` 스코프의 AssumeRole 자격증명 서명(세션 토큰
  포함)을 싣는다. 모호한 인증 설정은 시작 시 거부된다. Sablier 대기(307 같은 URL) 응답은 본문째
  재시도된다. 전송 실패는 도구 에러로 래핑.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/opensearch/opensearch_test.go` (경로는 3도구 공통 `do()` —
  `TestSearchSendsUnsignedJSONRequest`(없음) · `TestBasicAuthSendsCredentials`(basic) ·
  `TestSigV4SignsWithAssumedRoleCreds`(SigV4, `es`·`aoss`) · `TestFromEnvSelectsAuthMode` ·
  `TestFromEnvRejectsAmbiguousAuth` · `TestSearchFollowsSablierRedirectWithBody`). 통합
  `opensearch_document_put_sc4.py` — 운영과 같은 기본 모드만 돈다. 응답만으로는 경로를 구분할 수 없으므로 프록시 기록을 읽어
  `PUT /<index>/_doc/<id>`가 docstore 업스트림에 도달했고 `Authorization`·세션 토큰 헤더가 없음을 단정한다.

### 시나리오 5: 미설정 시 도구 에러
- **사전 조건**: `OPENSEARCH_ENDPOINT` 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러
- **검증 AC**: AC5
- **자동화**: Go 단위 `internal/opensearch/opensearch_test.go`
  (`TestUnavailableFailsEveryCall`, `TestFromEnvUnsetEndpointReturnsNil`) +
  `internal/server/mcp_test.go` (`TestOpenSearchUnavailableReturnsToolError`) + 통합
  `tests/integration/opensearch_document_put_sc5.py::test_opensearch_document_put_ac5_unconfigured_refusal`
  (자격증명 미부착 배포 변형에서 unavailable 도구 에러 반환 + 직후 ping 정상).
