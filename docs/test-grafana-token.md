# 테스트 문서: grafana_token

## 검증 대상 AC
- AC1: read-only 토큰 발급 (PRD: grafana_token)
- AC2: 즉시 사용 가능한 형태 (PRD: grafana_token)
- AC3: 미설정 시 graceful 거부 (PRD: grafana_token)
- AC4: 발급자 토큰 비노출 (PRD: grafana_token)

## 테스트 시나리오

### 시나리오 1: read 토큰 발급(.env + 만료 주석)
- **사전 조건**: grafana-mock 구성
- **실행 단계**: 인자 없이 호출
- **기대 결과**: text/plain 리소스에 `# token expires` 주석과 `GRAFANA_TOKEN=glc_mock_...` 포함
- **검증 AC**: AC1
- **자동화**: Go 단위 `mcp_test.go::TestGrafanaTokenDispatchesEnvResource`. 통합 `grafana.py`.
  참고: 1시간 TTL과 스코프는 서버에 고정.

### 시나리오 2: 엔드포인트·인스턴스 ID 동봉
- **사전 조건**: 동일
- **실행 단계**: 발급 결과의 키 검사
- **기대 결과**: `GRAFANA_METRICS_URL`, `GRAFANA_METRICS_USER`, `GRAFANA_LOGS_URL`,
  `GRAFANA_LOGS_USER`가 토큰과 함께 반환되어, 추가 정보 없이 Basic 인증(user=인스턴스 ID,
  password=토큰)으로 쿼리 가능
- **검증 AC**: AC2
- **자동화**: 통합 `grafana.py`(키 존재 단언). Go 단위 `TestGrafanaTokenDispatchesEnvResource`.

### 시나리오 3: 미설정 시 도구 에러
- **사전 조건**: Grafana env 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러
- **검증 AC**: AC3
- **자동화**: Go 단위 `mcp_test.go::TestGrafanaTokenUnavailableReturnsToolError`. 구성 검증은
  `internal/grafana/grafana_test.go::TestFromEnv*`.

### 시나리오 4: 발급자 토큰 비노출
- **사전 조건**: 동일(구성됨)
- **실행 단계**: 발급 결과 검사
- **기대 결과**: 출력은 단명 read 토큰·엔드포인트·USER뿐이며 `GRAFANA_ISSUER_TOKEN` 미포함
- **검증 AC**: AC4
- **자동화**: 부분 — 출력 내용 검증으로 간접 확인. 발급자 토큰 부재를 명시적으로 단언하는 케이스
  추가 권장.
