# 테스트 문서: github_commit_status_request

## 검증 대상 AC
- AC1: 요청은 PocketAide 에 쌓이고 도구는 바로 돌아온다 (PRD: github_commit_status_request)
- AC2: 서버는 GitHub 에 닿지 않고 자격증명을 다루지 않는다 (PRD: github_commit_status_request)
- AC3: 입력 검증은 PocketAide 호출보다 앞선다 (PRD: github_commit_status_request)
- AC4: context 네임스페이스 제한 (fail-closed) (PRD: github_commit_status_request)
- AC5: PocketAide 미설정 시 graceful 거부 (PRD: github_commit_status_request)
- AC6: 비파괴·외부 쓰기 광고 (PRD: github_commit_status_request)

## 픽스처

통합 테스트는 실물 PocketAide(`tests/k8s/kind/pocket-aide-fixture.yaml`, `ci.yml` 의 `POCKET_AIDE_SHA` 판 —
커밋 상태 요청 엔드포인트가 있는 dlddu/pocket-aide PRD-12 AC18 이후)와 그 사용자 API 를 쓴다. primary 배포의
ServiceAccount 는 사용자 `e2e-primary` 의 호출자로 등록돼 있고, e2e 는 그 사용자의 OIDC 토큰으로
`GET /api/commit-status-requests[/{id}]` 를 읽어 요청이 쌓였는지 잰다(`_approval_gate.py`). 쌓은 요청은 끝에서
거절해 다음 실행의 대기 목록에 남기지 않는다 — 거절은 GitHub 에 아무것도 쓰지 않는다. 다른 레인의 요청과는
`requester`·`description` 에 넣은 실행별 마커로 가른다.

GitHub 쪽은 기존 `github-mock`(`tests/k8s/kind/github-mock.yaml`)의 요청 기록(`/_admin/requests`)으로
「이 도구는 GitHub 에 닿지 않는다」를 잰다(AC2).

## 테스트 시나리오

### 시나리오 1: 요청이 PocketAide 에 쌓이고 도구는 바로 돌아온다
- **사전 조건**: 실물 PocketAide, 이 배포가 `e2e-primary` 의 호출자로 등록, `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES=homelab-k3s-mcp/`
- **실행 단계**: `repository=dlddu/test`, 40자 `sha`, `state=success`, `context=homelab-k3s-mcp/e2e`,
  `description`·`target_url`·`requester`·`explanation` 으로 호출 → 결과의 id 로 `e2e-primary` 의 요청 조회
- **기대 결과**: 결과가 `PENDING`·id·`created_at` 을 담아 결정 없이(15초 안에) 돌아온다. 조회한 요청이 `PENDING`
  이고 owner `dlddu`·repo `test`·sha·state·context·description·target_url·requester 가 입력과 같으며
  `explanation` 이 `reason` 으로 들어 있다. github-mock 에 status 쓰기 요청이 없다
- **검증 AC**: AC1
- **자동화**: Go 단위 `TestRequestCommitStatusFilesEverythingPocketAideNeedsAndReturnsAtOnce`·
  `TestRequestCommitStatusOmitsEmptyOptionalFields`·`TestRequestCommitStatusExplainsEachRefusal`
  (`internal/pocketaide/commitstatus_test.go`), `TestCommitStatusRequestReturnsTheTicketAtOnce`
  (`internal/server/commit_status_request_test.go`). 통합 e2e 는 `tests/integration/github_commit_status_sc1.py`

### 시나리오 2: 서버는 GitHub 에 닿지 않는다
- **사전 조건**: 시나리오 1과 동일, github-mock 요청 기록 초기화
- **실행 단계**: 유효한 입력으로 호출 → github-mock 기록을 읽는다 → 같은 세션에서 `github_app_installation_token`
  호출 → 다시 읽는다
- **기대 결과**: 첫 읽기는 0건이고 도구 결과 직렬화에 `ghs_`·PEM 아머·`eyJ` 가 없다. 토큰 도구 뒤에는 기록이
  생긴다(양성 대조)
- **검증 AC**: AC2
- **자동화**: 서버 구조상 이 도구의 경로에 `internal/github` 가 없다(`internal/mcp/commit_status.go`). 통합 e2e 는
  `tests/integration/github_commit_status_sc2.py`

### 시나리오 3: 잘못된 입력은 PocketAide 에 닿지 않는다
- **사전 조건**: 시나리오 1과 동일
- **실행 단계**: 다음 아홉 호출 — `repository` 누락 · `repository=homelab-k3s-mcp`(owner 없음) · `sha=abc1234` ·
  `state=ok` · `context` 누락 · 141자 `description` · `target_url=ftp://x` · `requester` 누락 · 201자 `requester`
  → 이어 유효한 한 벌
- **기대 결과**: 아홉 모두 필드를 지목하는 `invalid commit status request: …` 도구 에러이고, `e2e-primary` 의
  요청 목록에 이 실행의 마커를 담은 요청이 없다. 유효한 한 벌 뒤에는 정확히 하나 생긴다
- **검증 AC**: AC3
- **자동화**: Go 단위 `TestCommitStatusRequestValidationComesFirst`(표 기반 — 끝에 PocketAide 요청 0 을 단언),
  `TestCommitStatusRequestRefusesBeforePocketAide`(문자열 아닌 값의 JSON-RPC 오류 포함). 통합 e2e 는
  `tests/integration/github_commit_status_sc3.py`

### 시나리오 4: context 네임스페이스 밖은 요청하지 못한다
- **사전 조건**: (a) 시나리오 1과 동일한 배포, (b) `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES` 미설정 배포
- **실행 단계**: (a)에서 `context=ci/build`로 호출, 이어서 `context=homelab-k3s-mcp/e2e`로 호출.
  (b)에서 `context=homelab-k3s-mcp/e2e`로 호출
- **기대 결과**: (a) 첫 호출은 허용 접두사 목록을 보여 주는 도구 에러이고 요청이 쌓이지 않으며, 둘째 호출은
  쌓인다. (b) `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES` 를 지목하는 도구 에러
- **검증 AC**: AC4
- **자동화**: Go 단위 `TestCommitStatusRequestValidationComesFirst`(foreign context 행)·
  `TestCommitStatusRequestRefusesEveryContextWithoutPrefixes`. 통합 e2e 는
  `tests/integration/github_commit_status_sc4.py` 한 파일이 두 배포를 대조한다 — (b) 는 접두사 env 만 없는 전용
  변형(`tests/k8s/kind/commit-status-variant.yaml`)

### 시나리오 5: PocketAide 미설정 시 도구 에러
- **사전 조건**: `APPROVAL_GATE_URL` 미설정 배포(`auth-fixture.yaml` — 접두사 env 는 있어 검증을 지난다)
- **실행 단계**: 유효한 입력으로 호출한 뒤 `ping`
- **기대 결과**: `pocket-aide unavailable: PocketAide is not configured (APPROVAL_GATE_URL)` 도구 에러, 직후 `ping` 정상
- **검증 AC**: AC5
- **자동화**: Go 단위 `TestUnavailableRefusesCommitStatusRequests`·`TestCommitStatusRequestWithoutPocketAideIsUnavailable`.
  통합 e2e 는 `tests/integration/github_commit_status_sc5.py`(auth-variant)

### 시나리오 6: 어노테이션 광고
- **사전 조건**: 임의 배포
- **실행 단계**: `tools/list`
- **기대 결과**: `github_commit_status_request` 의 `readOnlyHint=false`, `destructiveHint=false`,
  `idempotentHint=false`, `openWorldHint=true`
- **검증 AC**: AC6
- **자동화**: Go 단위 `TestToolsListAdvertisesCommitStatusRequest`(`internal/server/commit_status_request_test.go` —
  `reason` 을 광고하지 않는 것까지). 통합 e2e 는 `tests/integration/github_commit_status_sc6.py`
