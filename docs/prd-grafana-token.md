# PRD: grafana_token

Grafana Cloud의 read-only 단명 토큰을 발급하는 도구.

## 달성 가치
- **V2: 단명·최소권한 자격증명** — 1시간 수명의 read-only 토큰을 발급한다.
- **V3: 안전한 운영(Safe-by-default)** — 발급자 토큰은 서버에만 두고, 스코프/TTL을 서버에
  고정한다.

## 도구 개요
- 입력: 없음 (access policy와 1시간 TTL이 서버에 고정)
- 출력: `text/plain` .env (`GRAFANA_METRICS_URL`, `GRAFANA_METRICS_USER`, `GRAFANA_LOGS_URL`,
  `GRAFANA_LOGS_USER`, `GRAFANA_TOKEN`)
- 서버 요구 설정: `GRAFANA_ISSUER_TOKEN`, `GRAFANA_READ_POLICY_ID`, `GRAFANA_REGION`, 엔드포인트/USER 변수
- 어노테이션: `readOnlyHint=false`, `destructiveHint=false`, `idempotentHint=false`, `openWorldHint=true`

## Acceptance Criteria

### AC1: read-only 토큰 발급
- **설명**: 서버 고정 access policy로 메트릭·로그 읽기 범위의 토큰(TTL 1시간)을 발급한다.
- **달성 가치**: V2
- **검증 방법**: 반환 토큰의 만료가 약 1시간이고 read-only 범위이다.

### AC2: 즉시 사용 가능한 형태
- **설명**: 메트릭(Mimir/Prometheus)·로그(Loki) 엔드포인트와 Basic 인증 사용자(데이터 소스
  인스턴스 ID)를 토큰과 함께 반환한다. Basic 인증의 password가 토큰, username이 인스턴스 ID이다.
- **달성 가치**: V2
- **검증 방법**: 반환된 URL·USER·TOKEN 조합으로 추가 정보 없이 메트릭/로그 쿼리를 인증할 수 있다.

### AC3: 미설정 시 graceful 거부
- **설명**: 필수 서버 설정이 없으면 unavailable 류 에러를 반환하며, 서버 기동·다른 도구에는
  영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.

### AC4: 발급자 토큰 비노출
- **설명**: 서버의 `GRAFANA_ISSUER_TOKEN`은 응답에 포함되지 않으며, 노출되는 것은 단명 read
  토큰뿐이다.
- **달성 가치**: V2, V3
- **검증 방법**: 응답 페이로드에 발급자 토큰이 존재하지 않는다.
