# 테스트 문서: opensearch_search

## 검증 대상 AC
- AC1: 질의 검색 (PRD: opensearch_search)
- AC2: 결과 상한 (PRD: opensearch_search)
- AC3: AssumeRole·SigV4 접근 (PRD: opensearch_search)
- AC4: 미설정 시 graceful 거부 (PRD: opensearch_search)

## 테스트 시나리오

### 시나리오 1: 질의어 매칭 문서 반환
- **사전 조건**: OpenSearch 호환 픽스처(예: 단일 노드 opensearch 컨테이너)의 `runbooks`
  인덱스에 "etcd backup" 문서와 무관한 문서를 각 1건 시드
- **실행 단계**: `query="etcd backup"`으로 호출, 이어서 `index="runbooks"`를 지정해 재호출
- **기대 결과**: 매칭 문서만 index·id·score·본문(`_source`)과 함께 반환되고 무관한 문서는
  미포함. index 지정 시 검색 범위가 해당 인덱스로 한정.
- **검증 AC**: AC1
- **자동화**: 없음 — 도구 미구현. 구현 시 Go 단위(`internal/opensearch`) + 통합
  `tests/integration/opensearch.py`로 자동화 예정.

### 시나리오 2: size 기본값과 상한 초과 거부
- **사전 조건**: 동일 인덱스에 매칭 문서 12건 시드
- **실행 단계**: size 미지정 호출 → `size=50` 호출 → `size=51` 호출
- **기대 결과**: 미지정 시 최대 10건 반환, 50은 허용, 51은 클램프 없이 도구 에러로 거부.
- **검증 AC**: AC2
- **자동화**: 없음 — 도구 미구현. 구현 시 Go 단위로 자동화 예정.

### 시나리오 3: AssumeRole → SigV4 경로(정적 키 없음)
- **사전 조건**: 베이스 자격증명은 기본 체인, `OPENSEARCH_ROLE_ARN` 설정
- **실행 단계**: 호출 후 접근 경로 확인
- **기대 결과**: 기본 자격증명 체인 → STS AssumeRole → 단명 자격증명으로 SigV4(service
  `aoss`) 서명 요청. 정적 키 환경변수 미사용. 서명/요청 실패는 도구 에러로 래핑.
- **검증 AC**: AC3
- **자동화**: 없음 — 도구 미구현. 구현 시 Go 단위(서명·AssumeRole 스텁) + 통합으로 자동화
  예정.

### 시나리오 4: 미설정 시 도구 에러
- **사전 조건**: `OPENSEARCH_ENDPOINT` 등 관련 env 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러
- **검증 AC**: AC4
- **자동화**: 없음 — 도구 미구현. 구현 시 Go 단위(`FromEnv` 미설정 케이스)로 자동화 예정.
