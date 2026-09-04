# 테스트 문서: platform — 인증·안전 기반 (공통)

## 검증 대상 AC
- AC1: 인증 게이트 (PRD: platform)
- AC2: 인증 디스커버리 (PRD: platform)
- AC3: 최소권한 권한 경계 RBAC (PRD: platform)
- AC4: 하드닝된 런타임 (PRD: platform)
- AC5: 서버 수준 graceful degradation (PRD: platform)
- AC6: 헬스·레디니스 (PRD: platform)
- AC7: API 키 인증 (비대화형) (PRD: platform)
- AC8: 인증 방식 구성 유연성 (PRD: platform)

## 테스트 시나리오

### 시나리오 1: Bearer 인증 게이트
- **사전 조건**: 인증 활성(`MCP_AUTH_DISABLED` 미설정), IdP/JWKS 구성
- **실행 단계**: (a) 토큰 없이 `/mcp` 요청, (b) 무효 토큰 요청, (c) 유효 토큰 요청
- **기대 결과**: (a)(b) 401 + `WWW-Authenticate`가 보호 리소스 메타데이터를 가리킴, (c) 정상 처리
- **검증 AC**: AC1
- **자동화**: 배포 서버 통합 e2e `tests/integration/platform_auth_safety_ac1.py::test_platform_auth_safety_ac1_gate`
  (인증 켠 배포 변형 `tests/k8s/kind/auth-fixture.yaml`에서 무Authorization `/mcp` 호출 →
  401 `missing_token` + `WWW-Authenticate: Bearer`). Go 단위 `internal/auth/auth_test.go`의
  RequireBearer 게이트 단언과 병행.

### 시나리오 2: 인증 디스커버리
- **사전 조건**: 동일
- **실행 단계**: `/.well-known/oauth-protected-resource`, `/.well-known/openid-configuration`
  조회
- **기대 결과**: 보호 리소스 메타데이터가 발급자/리소스 반환, OIDC discovery로 JWKS 로드 가능
- **검증 AC**: AC2
- **자동화**: 배포 서버 통합 e2e `tests/integration/platform_auth_safety_ac2.py`
  (`실행 대상: oauth-variant` — `tests/k8s/kind/oidc-fixture.yaml`이 실 OIDC 발급자 dex와
  `MCP_OAUTH_*`를 세팅한 서버 변형을 띄운다. 디스커버리 라우트는 OAuth가 구성된 배포에만
  걸리므로 주 배포·auth-variant에서는 관측되지 않는다). 세 케이스가 클라이언트의 자동 구성
  경로를 **연결된 사슬로** 걷는다 — ① 인증 없는 `/mcp` 401의 `WWW-Authenticate`가
  `resource_metadata`로 문서 주소를 광고, ② 그 문서가 `resource`·`authorization_servers`·
  `bearer_methods_supported`를 배포된 구성대로 반환(`MCP_OAUTH_RESOURCE`를 audience와 다른
  값으로 두어 폴백과 구분), ③ 광고된 발급자의 `/.well-known/openid-configuration`이 준
  `jwks_uri`에서 쓸 수 있는 RSA 키를 읽는다. 서버 쪽 JWKS 동적 로드는 그 배포가 Available
  하다는 사실이 증거이며(실패 시 `auth.FromEnv` → `os.Exit(1)`), 그 경로가 실제로 치명적임은
  시나리오 8의 (d)가 관측한다. Go 단위 `internal/auth/auth_test.go::TestMetadataHandlerServesProtectedResource`와
  `internal/server/auth_routing_test.go`의 라우팅 단언과 병행.

### 시나리오 3: 최소권한 RBAC 경계
- **사전 조건**: 배포된 RBAC(`k8s/rbac.yaml`)
- **실행 단계**: RBAC 규칙 정적 검토
- **기대 결과**: 워크로드 get/list/watch/patch, 파드 get/list, pods/log get, pods/exec
  get/create, namespaces·events get/list만 존재. delete·시크릿 읽기·워크로드 create 없음.
- **검증 AC**: AC3
- **자동화**: 배포 identity e2e `tests/integration/platform_auth_safety_ac3.py`
  ::test_platform_auth_safety_ac3_rbac_boundary — 실제로 바인딩된 ClusterRole을 읽어
  기대 권한과 **동등**함을 단정하고(추가 권한이 어디에 있어도 실패), apiserver
  SubjectAccessReview로 허용 동사 전부가 yes·AC가 못박은 금지 동사(워크로드
  delete/create·시크릿 읽기·네임스페이스 생성/삭제)가 no임을 관측한다. `k8s/rbac.yaml`
  정적 리뷰는 보조 수단이다.

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
- **자동화**: 통합 `tests/integration/platform_auth_safety_ac5.py::test_platform_auth_safety_ac5_graceful_degradation`
  — 자격증명 시크릿을 하나도 붙이지 않은 배포 변형(`auth-fixture.yaml`)에서 `/healthz` 정상 +
  `tools/list`가 전체 도구 표면을 그대로 반환함을 단언. 자격증명이 모두 배선된 주 배포에서는
  이 AC의 전제가 성립하지 않아 주 배포에서 도는 파일로는 관측 불가. Go 단위
  `mcp_test.go::TestToolsListIncludesAllTools`, `TestToolsListAdvertisesAnnotations`,
  각 `*UnavailableReturnsToolError`.

### 시나리오 6: 헬스·레디니스
- **사전 조건**: 서버 기동
- **실행 단계**: `/healthz`, `/readyz`, 루트, 미존재 경로 요청
- **기대 결과**: `/healthz` status=ok, `/readyz` status=ready, 루트는 서비스명, 미존재 경로는 404
- **검증 AC**: AC6
- **자동화**: 통합 `tests/integration/platform_auth_safety_ac6.py::test_platform_auth_safety_ac6_health_readiness`
  — 배포 서버의 `/healthz`(status=ok)·`/readyz`(status=ready)를 단언(비정상 측면은 e2e로
  강제할 수단이 없어 미커버). Go 단위 `internal/server/health_test.go::TestHealthzReturnsOK`,
  `TestReadyzReturnsReady`, `TestRootReturnsServiceName`, `TestUnknownRouteReturns404`.

### 시나리오 7: API 키 인증 게이트 (비대화형)
- **사전 조건**: 인증 활성(`MCP_AUTH_DISABLED` 미설정), `MCP_API_KEYS`에 하나 이상의 키 설정
- **실행 단계**: (a) 유효 키를 `Authorization: Bearer <key>`로 요청, (b) 목록에 없는 키로 요청,
  (c) Authorization 헤더 없이 요청, (d) OAuth도 함께 구성한 상태에서 유효 JWT로 요청
- **기대 결과**: (a) 정상 처리, (b)(c) 401, (d) 정상 처리(API 키 경로 실패 후 JWT 폴백으로 통과).
  어느 응답에도 키 값이 노출되지 않음.
- **검증 AC**: AC7 (일부 AC1)
- **자동화**: Go 단위 `internal/auth/auth_test.go::TestParseAPIKeys`, `TestMatchAPIKey`,
  `TestRequireBearerAcceptsAPIKey`, `TestRequireBearerRejectsUnknownKeyInKeyOnlyMode`,
  `TestRequireBearerRejectsJWTInKeyOnlyMode`, `TestRequireBearerAcceptsAPIKeyAndJWT`
  — 유효/무효/부재 키 게이트, JWT 병행 시 양쪽 통과, 상수시간 대조 경로, 키 비노출 단언.
  배포 서버 통합 e2e `tests/integration/platform_auth_safety_ac7.py::test_platform_auth_safety_ac7_api_key`
  (인증 켠 변형에서 무효 키 → 401 `invalid_token`, 유효 키 → tools/list 인가, 응답에 키 미노출).

### 시나리오 8: 인증 방식 구성 유연성 (env 게이팅)
- **사전 조건**: 각 조합별 env 세팅
- **실행 단계**: `FromEnv`를 (a) `MCP_API_KEYS`만, (b) `MCP_OAUTH_*`만, (c) 둘 다,
  (d) 둘 다 미설정 + `MCP_AUTH_DISABLED`도 미설정으로 각각 호출; 각 경우 `App` 라우팅에서
  `/.well-known/oauth-protected-resource` 제공 여부 확인
- **기대 결과**: (a) 인증 활성 + 디스커버리 엔드포인트 미제공, (b) 기존 동작(디스커버리 제공),
  (c) 둘 다 동작, (d) 기동 실패(무방비 노출 차단)
- **검증 AC**: AC8
- **자동화**: 배포 서버 통합 e2e `tests/integration/platform_auth_safety_ac8.py`
  (`실행 대상: auth-variant` — 러너가 준 base URL이 구성 (a)인 `auth-fixture.yaml` 배포이고,
  (b)(c)(d)는 `tests/k8s/kind/oidc-fixture.yaml`의 세 변형에 `_oidc.port_forward`·kubectl로
  닿는다). 네 케이스가 AC의 네 구성을 그대로 대조한다 — (a) 401 + 챌린지에 `resource_metadata`
  없음 + 디스커버리 404, (b) `MCP_API_KEYS`가 전혀 없는 배포가 Available + 디스커버리 제공,
  (c) API 키로 `tools/list` 인가 + 디스커버리 제공 + 챌린지가 그것을 광고,
  (d) 어느 경로도 구성되지 않은 배포가 `availableReplicas` 0 · 종료 코드 1 · 로그
  `no authentication configured`로 **기동 실패**. env 게이팅 자체는 아래 Go 단위가 덮고,
  e2e가 더하는 것은 그것이 **배포된 서버의 라우팅과 기동 여부로 나타나는가**다(단위 테스트는
  파드가 뜨지 않는 것을 보지 못한다). 함께:
  Go 단위 `internal/auth/auth_test.go::TestFromEnvAPIKeysOnly`, `TestFromEnvOAuthGating`,
  `TestFromEnvNoAuthConfiguredErrors`, `TestFromEnvRequiresIssuerWhenOAuthRequested`,
  `TestFromEnvRequiresAudienceWhenOAuthRequested`(`FromEnv` env 게이팅 4조합 — 기존
  issuer/audience 필수 테스트를 "API 키 미설정 시에만 OAuth 필수"로 갱신). 디스커버리 조건부
  제공은 `internal/server/auth_routing_test.go::TestDiscoveryServedWhenOAuthConfigured`·
  `TestDiscoveryAbsentWhenOAuthNotConfigured`.
