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
- **자동화**: 없음 — 도구 미구현. 구현 시 Go 단위(`internal/opensearch`) + 통합
  `tests/integration/opensearch.py`로 자동화 예정.

### 시나리오 2: 없는 문서 삭제 → not_found
- **사전 조건**: `notes` 인덱스에 `ghost` id 부재
- **실행 단계**: `id="ghost"` 삭제 호출, 이어서 같은 호출 반복
- **기대 결과**: not_found가 명확한 결과로 반환되고 서버·다른 도구는 정상. 반복 호출도 동일
  결과(멱등).
- **검증 AC**: AC2
- **자동화**: 없음 — 도구 미구현. 구현 시 Go 단위로 자동화 예정.

### 시나리오 3: destructiveHint 광고
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 호출
- **기대 결과**: opensearch_document_delete 어노테이션이 destructiveHint=true.
- **검증 AC**: AC3
- **자동화**: 없음 — 도구 미구현. 구현 시 Go 단위(`internal/server/mcp_test.go`의 도구
  어노테이션 검증)로 자동화 예정.

### 시나리오 4: AssumeRole → SigV4 경로(정적 키 없음)
- **사전 조건**: 베이스 자격증명은 기본 체인, `OPENSEARCH_ROLE_ARN` 설정
- **실행 단계**: 삭제 호출 후 접근 경로 확인
- **기대 결과**: 기본 체인 → STS AssumeRole → 단명 자격증명으로 SigV4(service `aoss`) 서명
  요청. 정적 키 미사용.
- **검증 AC**: AC4
- **자동화**: 없음 — 도구 미구현. 구현 시 Go 단위 + 통합으로 자동화 예정.

### 시나리오 5: 미설정 시 도구 에러
- **사전 조건**: OpenSearch 관련 env 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러
- **검증 AC**: AC5
- **자동화**: 없음 — 도구 미구현. 구현 시 Go 단위로 자동화 예정.
