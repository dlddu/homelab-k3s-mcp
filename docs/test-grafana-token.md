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
- **자동화**: 통합 `grafana.py::test_grafana_token_ac1_read_only_short_lived` —
  `# token expires <RFC3339>`를 파싱해 만료가 지금부터 50~70분 사이임을 단언(mock이 서버가
  보낸 `expiresAt`을 그대로 되돌려주므로 TTL이 실제로 관측된다) + `GRAFANA_TOKEN=glc_mock_`.
  Go 단위 `mcp_test.go::TestGrafanaTokenDispatchesEnvResource`. 참고: 스코프는 서버 고정
  access policy이며 정책이 실제로 무엇을 허용하는지는 Grafana Cloud 측이라 미관측.

### 시나리오 2: 엔드포인트·인스턴스 ID 동봉
- **사전 조건**: 동일
- **실행 단계**: 발급 결과의 키 검사
- **기대 결과**: `GRAFANA_METRICS_URL`, `GRAFANA_METRICS_USER`, `GRAFANA_LOGS_URL`,
  `GRAFANA_LOGS_USER`가 토큰과 함께 반환되어, 추가 정보 없이 Basic 인증(user=인스턴스 ID,
  password=토큰)으로 쿼리 가능
- **검증 AC**: AC2
- **자동화**: 통합 `grafana.py::test_grafana_token_ac2_ready_to_use_env` — 메트릭·로그
  URL/USER 4종이 구성값 그대로 실려 오고 공유 Basic 비밀번호인 `GRAFANA_TOKEN`이 함께
  반환됨을 값 단위로 단언. Go 단위 `TestGrafanaTokenDispatchesEnvResource`.

### 시나리오 3: 미설정 시 도구 에러
- **사전 조건**: Grafana env 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러
- **검증 AC**: AC3
- **자동화**: Go 단위 `mcp_test.go::TestGrafanaTokenUnavailableReturnsToolError`. 구성 검증은
  `internal/grafana/grafana_test.go::TestFromEnv*`. 통합
  `tests/integration/no_config.py::test_grafana_token_ac3_unconfigured_refusal`
  (자격증명 미부착 배포 변형에서 unavailable 도구 에러 반환 + 직후 ping 정상).

### 시나리오 4: 발급자 토큰 비노출
- **사전 조건**: 동일(구성됨)
- **실행 단계**: 발급 결과 검사
- **기대 결과**: 출력은 단명 read 토큰·엔드포인트·USER뿐이며 `GRAFANA_ISSUER_TOKEN` 미포함
- **검증 AC**: AC4
- **자동화**: 통합 `grafana.py::test_grafana_token_ac4_issuer_token_not_exposed` — 발급 응답 .env에
  `GRAFANA_ISSUER_TOKEN` 키·그 구성값(`glsa_mock_issuer`)·발급자 접두(`glsa_`)가 부재하고 단명 read
  토큰(`GRAFANA_TOKEN=glc_mock_…`)만 포함됨을 단언. 구성 검증은 `internal/grafana/grafana_test.go`.
