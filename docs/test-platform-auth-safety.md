# 테스트 문서: platform — 인증·안전 기반 (공통)

## 검증 대상 AC
- AC1: 인증 게이트 (PRD: platform)
- AC2: 인증 디스커버리 (PRD: platform)
- AC3: 최소권한 권한 경계 RBAC (PRD: platform)
- AC4: 하드닝된 런타임 (PRD: platform)
- AC5: 서버 수준 graceful degradation (PRD: platform)
- AC6: 헬스·레디니스 (PRD: platform)

## 테스트 시나리오

### 시나리오 1: Bearer 인증 게이트
- **사전 조건**: 인증 활성(`MCP_AUTH_DISABLED` 미설정), IdP/JWKS 구성
- **실행 단계**: (a) 토큰 없이 `/mcp` 요청, (b) 무효 토큰 요청, (c) 유효 토큰 요청
- **기대 결과**: (a)(b) 401 + `WWW-Authenticate`가 보호 리소스 메타데이터를 가리킴, (c) 정상 처리
- **검증 AC**: AC1
- **자동화**: ❌ 현재 자동화 없음(`internal/auth` 단위 테스트 부재) → 인증 게이트 자동화 추가 권장.

### 시나리오 2: 인증 디스커버리
- **사전 조건**: 동일
- **실행 단계**: `/.well-known/oauth-protected-resource`, `/.well-known/openid-configuration`
  조회
- **기대 결과**: 보호 리소스 메타데이터가 발급자/리소스 반환, OIDC discovery로 JWKS 로드 가능
- **검증 AC**: AC2
- **자동화**: ❌ 현재 자동화 없음 → 디스커버리 엔드포인트 자동화 추가 권장.

### 시나리오 3: 최소권한 RBAC 경계
- **사전 조건**: 배포된 RBAC(`k8s/rbac.yaml`)
- **실행 단계**: RBAC 규칙 정적 검토
- **기대 결과**: 워크로드 get/list/watch/patch, 파드 get/list, pods/log get, pods/exec
  get/create, namespaces·events get/list만 존재. delete·시크릿 읽기·워크로드 create 없음.
- **검증 AC**: AC3
- **자동화**: 🟡 정적 검증(`k8s/rbac.yaml` 리뷰). pods/log·pods/exec 바인딩은 통합
  `workload.py`/`dear_baby.py`로 간접 동작 확인. delete/secret 부재 단언 자동화 추가 권장.

### 시나리오 4: 하드닝된 런타임
- **사전 조건**: 배포 매니페스트(`k8s/deployment.yaml`)
- **실행 단계**: securityContext 정적 검토 및 컨테이너 기동 확인
- **기대 결과**: nonroot, readOnlyRootFilesystem, 모든 capability drop, seccomp RuntimeDefault로
  비특권 기동
- **검증 AC**: AC4
- **자동화**: 🟡 정적 검증(`k8s/deployment.yaml` 리뷰). 런타임 단언 자동화 추가 권장.

### 시나리오 5: 서버 수준 graceful degradation
- **사전 조건**: 일부 통합(GitHub/AWS/Grafana/k8s) 미설정
- **실행 단계**: 서버 기동, `tools/list` 조회, 미설정 도구 호출
- **기대 결과**: 서버 정상 기동, `tools/list` 정상 응답(전 도구 광고), 미설정 도구만 unavailable
  도구 에러
- **검증 AC**: AC5
- **자동화**: Go 단위 `mcp_test.go::TestToolsListIncludesAllTools`,
  `TestToolsListAdvertisesAnnotations`, 각 `*UnavailableReturnsToolError`. 통합 `smoke.py`
  (tools/list).

### 시나리오 6: 헬스·레디니스
- **사전 조건**: 서버 기동
- **실행 단계**: `/healthz`, `/readyz`, 루트, 미존재 경로 요청
- **기대 결과**: `/healthz` status=ok, `/readyz` status=ready, 루트는 서비스명, 미존재 경로는 404
- **검증 AC**: AC6
- **자동화**: Go 단위 `internal/server/health_test.go::TestHealthzReturnsOK`,
  `TestReadyzReturnsReady`, `TestRootReturnsServiceName`, `TestUnknownRouteReturns404`. 통합
  `smoke.py`(/healthz, /readyz).
