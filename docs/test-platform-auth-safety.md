# 테스트 문서: platform — 인증·안전 기반 (공통)

## 검증 대상 AC
- AC1: 인증 게이트 (PRD: platform)
- AC2: 인증 디스커버리 (PRD: platform)
- AC4: 하드닝된 런타임 (PRD: platform)
- AC5: 서버 수준 graceful degradation (PRD: platform)
- AC6: 헬스·레디니스 (PRD: platform)
- AC7: API 키 인증 (비대화형) (PRD: platform)
- AC8: 인증 방식 구성 유연성 (PRD: platform)
- AC9: GitHub Actions OIDC 인증 (워크플로) (PRD: platform)
- AC10: GitHub Actions 호출자의 도구 범위 제한 (PRD: platform)
- AC11: GitHub Actions 호출자가 받는 설치 토큰의 범위 (PRD: platform)

## 테스트 시나리오

### 시나리오 1: Bearer 인증 게이트
- **사전 조건**: 인증 활성(`MCP_AUTH_DISABLED` 미설정), IdP/JWKS 구성
- **실행 단계**: (a) 토큰 없이 `/mcp` 요청, (b) 무효 토큰 요청, (c) 유효 토큰 요청
- **기대 결과**: (a)(b) 401 + `WWW-Authenticate`가 보호 리소스 메타데이터를 가리킴, (c) 정상 처리
- **검증 AC**: AC1
- **자동화**: 배포 서버 통합 e2e `tests/integration/platform_auth_safety_sc1.py::test_platform_auth_safety_ac1_gate`
  (인증 켠 배포 변형 `tests/k8s/kind/auth-fixture.yaml`에서 무Authorization `/mcp` 호출 →
  401 `missing_token` + `WWW-Authenticate: Bearer`). Go 단위 `internal/auth/auth_test.go`의
  RequireBearer 게이트 단언과 병행.

### 시나리오 2: 인증 디스커버리
- **사전 조건**: 동일
- **실행 단계**: `/.well-known/oauth-protected-resource`, `/.well-known/openid-configuration`
  조회
- **기대 결과**: 보호 리소스 메타데이터가 발급자/리소스 반환, OIDC discovery로 JWKS 로드 가능
- **검증 AC**: AC2
- **자동화**: 배포 서버 통합 e2e `tests/integration/platform_auth_safety_sc2.py`
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
- **자동화**: 통합 `tests/integration/platform_auth_safety_sc5.py::test_platform_auth_safety_ac5_graceful_degradation`
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
- **자동화**: 통합 `tests/integration/platform_auth_safety_sc6.py::test_platform_auth_safety_ac6_health_readiness`
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
  배포 서버 통합 e2e `tests/integration/platform_auth_safety_sc7.py::test_platform_auth_safety_ac7_api_key`
  (인증 켠 변형에서 무효 키 → 401 `invalid_token`, 유효 키 → tools/list 인가, 응답에 키 미노출).

### 시나리오 8: 인증 방식 구성 유연성 (env 게이팅)
- **사전 조건**: 각 조합별 env 세팅
- **실행 단계**: `FromEnv`를 (a) `MCP_API_KEYS`만, (b) `MCP_OAUTH_*`만, (c) 둘 다,
  (d) 둘 다 미설정 + `MCP_AUTH_DISABLED`도 미설정으로 각각 호출; 각 경우 `App` 라우팅에서
  `/.well-known/oauth-protected-resource` 제공 여부 확인
- **기대 결과**: (a) 인증 활성 + 디스커버리 엔드포인트 미제공, (b) 기존 동작(디스커버리 제공),
  (c) 둘 다 동작, (d) 기동 실패(무방비 노출 차단)
- **검증 AC**: AC8
- **자동화**: 배포 서버 통합 e2e `tests/integration/platform_auth_safety_sc8.py`
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

### 시나리오 9: GitHub Actions OIDC 토큰 인증
- **사전 조건**: 인증 활성, `MCP_GITHUB_OIDC_ISSUER`가 GitHub 클레임(`repository`·`workflow_ref`)을
  담아 서명하는 발급자, `MCP_GITHUB_OIDC_AUDIENCE` 설정, `MCP_GITHUB_OIDC_RULES`에 레포 R 의 레포
  규칙과 워크플로 W 규칙, 레포 N 의 워크플로 J 규칙만 등재
- **실행 단계**: (a) R/W, R/다른 워크플로, N/J 토큰으로 요청, (b) 규칙 밖 레포·N 의 다른 워크플로·
  N 의 `workflow_ref` 없음·다른 레포를 가리키는 `workflow_ref`·`repository` 없음·다른 audience·
  만료·`exp` 없음·다른 키 서명 토큰으로 요청, (c) OAuth도 구성한 상태에서 OAuth JWT로 요청,
  (d) audience 또는 규칙이 빈 구성, 잘못된 형태의 규칙, OAuth와 같은 발급자를 쓰는 구성, 같은 규칙을
  `MCP_GITHUB_OIDC_RULES`와 `MCP_GITHUB_OIDC_RULES_X`에 둔 구성으로 기동, (e) N/J 규칙을
  `MCP_GITHUB_OIDC_RULES_X`로 옮긴 구성에서 (a)를 반복
- **기대 결과**: (a) 정상 처리 + 기록 주체 `github_oidc:<레포>/<워크플로 경로>`, (b) 401
  `invalid_token` + 응답에 토큰 미노출, (c) OAuth 경로로 통과하고 도구 범위가 걸리지 않음,
  (d) 기동 실패(중복은 먼저 나온 변수를 밝힘), (e) (a)와 같음
- **검증 AC**: AC9 (일부 AC1·AC8)
- **자동화**: Go 단위 `internal/auth/github_oidc_test.go::TestRequireBearerAcceptsGitHubOIDCAndScopesTools`,
  `TestRequireBearerRefusesGitHubOIDC`, `TestRequireBearerKeepsOAuthUnscopedNextToGitHubOIDC`,
  `TestFromEnvGitHubOIDCOnly`, `TestFromEnvGitHubOIDCRequiresAudienceAndRules`,
  `TestFromEnvRefusesOneIssuerForOAuthAndGitHub`, `TestParseGitHubRules`, `TestWorkflowPath`,
  `internal/auth/github_oidc_token_cap_test.go::TestGitHubRuleSourcesReadsSuffixedVariables`·
  `TestParseGitHubRuleSourcesCombinesVariables`(변수 분할),
  그리고 auth·디스패처를 잇는 `internal/server/github_oidc_test.go::TestGitHubOIDCTokenFromAnotherRepositoryIsUnauthorized`.
  배포 서버 e2e는 🚧 공백 — kind 에 GitHub 클레임을 내는 발급자가 없다(dex 는 `repository`·
  `workflow_ref` 클레임을 낼 수 없다).

### 시나리오 10: GitHub Actions 호출자는 자기 레포의 도구만 본다
- **사전 조건**: 시나리오 9의 구성. R/W 규칙의 범위는 `["github_commit_status_create", "ping"]`,
  R 레포 규칙의 범위는 `["opensearch_search", "ping"]`
- **실행 단계**: (a) R/W 토큰과 R/다른 워크플로 토큰으로 허용 집합 확인, (b) W 범위만 둔 배포에서
  `tools/list`, (c) `ping` 호출, (d) 범위 밖이지만 존재하는 `resource_delete` 호출과 존재하지 않는
  도구 호출, (e) 서버에 없는 도구 이름을 규칙에 넣은 구성으로 기동
- **기대 결과**: (a) R/W 는 합집합(`ping`·`github_commit_status_create`·`opensearch_search`), R/다른
  워크플로는 레포 규칙만, (b) 정확히 `ping`·`github_commit_status_create`가 광고 순서대로, 각
  항목의 스키마는 원래 그대로, (c) `pong`, (d) 두 호출 모두 `-32602` · `unknown tool: <name>` —
  형태가 같고, `resource_delete` 거부 기록이 `principal=github_oidc:R/<W 경로>` · `result=refused` ·
  `reason=invalid_input`, (e) 기동 실패(`invalid auth config`)
- **검증 AC**: AC10
- **자동화**: Go 단위 `internal/auth/github_oidc_test.go::TestRequireBearerAcceptsGitHubOIDCAndScopesTools`(합집합·
  워크플로별 범위), `internal/server/github_oidc_test.go::TestGitHubOIDCCallerSeesOnlyItsTools`,
  `TestGitHubOIDCCallerIsRefusedOutOfScopeAsIfTheToolDidNotExist`(실 auth 미들웨어 + 디스패처를
  로컬 JWKS 발급자로 엮는다), `internal/auth/github_oidc_test.go::TestCheckToolScopes`,
  `internal/toolscope/toolscope_test.go`. 배포 서버 e2e는 시나리오 9와 같은 이유로 🚧 공백.

### 시나리오 11: GitHub Actions 호출자의 설치 토큰은 상한 안, 자기 레포 workflows는 없다
- **사전 조건**: 레포 R(`dlddu/reconciler`)의 워크플로 W 규칙이 `github_app_installation_token`을 주고
  `githubToken: {repositories: [A, B], permissions: {contents: write, pull_requests: write, metadata: read}}` 상한을 둔다.
  GitHub는 발급 호출을 기록하는 가짜다.
- **실행 단계**: (a) 인자 없이 호출, (b) `repositories: [A]`, `permissions: {contents: read}`, (c) `repositories: [R]`,
  `permissions: {workflows: write}`, `metadata: write`, 빈 목록·빈 맵, (d) 상한 없는 규칙으로 들어온 R 호출자가
  인자 없이 / `repositories: [R]` / `permissions: {workflows: write}`로 호출, (e) 같은 호출자가
  `repositories: [A]`, `permissions: {workflows: write}`로 호출, (f) 상한이 R에 workflows를 주는 구성으로 기동,
  (g) 상한을 배열 `[{repositories: [A, B], permissions: {contents: write, metadata: read}}, {repositories: [M],
  permissions: {contents: read, metadata: read}}]`로 바꾸고 `repositories: [M]` / `permissions: {contents: write}` /
  `repositories: [A, M]` / `repositories: [M]`, `permissions: {contents: write}` / 인자 없이 / `permissions:
  {contents: read}`로 호출, (h) 상한을 `[{excludeRepositories: [R, M, I]}, {repositories: [M], permissions: {contents: read,
  metadata: read}}]`로 바꾸고 `repositories: [A]`, `permissions: {workflows: write, administration: write}` / `repositories:
  [A]` / `repositories: [M]` / `repositories: [I]` / 인자 없이 호출, (i) 자기 레포 R을 빼지 않은 권한 생략 제외 상한으로 기동
- **기대 결과**: (a) A·B와 상한 권한으로 발급, (b) 요청 그대로 발급, (c) 전부 `-32602`(호출자의 범위를 밝힘)이고
  GitHub 호출 0건, (d) 전부 `-32602`(자기 레포 workflows)이고 GitHub 호출 0건, (e) 발급, (f) 기동 실패,
  (g) 앞의 둘은 들어맞는 상한에서 채워 발급(M·read / A·B·write), 다음 둘은 상한 사이에 걸치거나 넘어서
  `-32602`(상한 목록을 밝힘), 뒤의 둘은 두 상한에 다 들어맞아 모호하므로 `-32602` — 거부는 모두 GitHub 호출 0건,
  (h) 앞의 셋은 발급(A·요청 권한 그대로 / A·권한 생략 그대로 / M·read), `repositories: [I]`는 `-32602`(제외 레포,
  GitHub 호출 0건), 인자 없는 호출은 레포를 명시하지 않아 제외 상한에 들지 않고 M 상한 하나에만 들어맞으므로 M·read로 발급,
  (i) 기동 실패
- **검증 AC**: AC11
- **자동화**: Go 단위 `internal/mcp/token_cap_test.go`(상한 적용·거부, 자기 레포 가드, 상한 뒤 가드, 상한 배열의
  발급·거부·모호, 제외 상한 `TestInstallationTokenExcludeCap*`·`TestInstallationTokenSingleExcludeCap`),
  `internal/auth/github_oidc_token_cap_test.go`(규칙 파싱·검증, 상한 배열 파싱, 제외 상한 파싱·자기 레포 규칙, 여러 규칙의 상한을
  합치지 않고 모으는지, 미들웨어가 상한·자기 레포를 싣는지).
  배포 서버 e2e는 시나리오 9와 같은 이유로 🚧 공백.
