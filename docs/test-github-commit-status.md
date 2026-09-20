# 테스트 문서: github_commit_status_create

## 검증 대상 AC
- AC1: 커밋 status 기록 (PRD: github_commit_status_create)
- AC2: 내부 토큰의 최소 스코프·비노출 (PRD: github_commit_status_create)
- AC3: 입력 검증은 GitHub 호출보다 앞선다 (PRD: github_commit_status_create)
- AC4: context 네임스페이스 제한 (fail-closed) (PRD: github_commit_status_create)
- AC5: 미설정 시 graceful 거부 (PRD: github_commit_status_create)
- AC6: 비파괴·외부 쓰기 광고 (PRD: github_commit_status_create)

## 픽스처

통합 테스트는 기존 `github-mock`(`tests/k8s/kind/github-mock.yaml`)을 쓴다. 기록 엔드포인트
(`/_admin/requests`)와 응답 모드 노브(`/_admin/config`)는 토큰 도구 AC5 작업이 이미 세웠으므로,
남은 것은 **`POST /repos/{owner}/{repo}/statuses/{sha}`(422 모드 포함)** 와
**`GET /app/installations/{id}` 응답의 `account.login`** 둘이다 — 구현은 owner 를 호출자에게서 받지
않고 그 필드에서 읽는데(2026-09-20), 현 스텁은 `{id, permissions}` 만 돌려준다. 이 둘을 더하면서
`docs/e2e-mocking-policy.md`의 `github-mock` 등재 「대상」을 같은 PR에서 넓혀야 한다. 새 상류를
띄우는 것이 아니라 기존 등재의 대상을 넓히는 것이다.

## 테스트 시나리오

### 시나리오 1: status 기록과 GitHub 오류 전달
- **사전 조건**: github-mock 구성, `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES=homelab-k3s-mcp/`
- **실행 단계**: (a) `repository=test`, 40자 `sha`, `state=success`,
  `context=homelab-k3s-mcp/e2e`, `description`, `target_url`로 호출. (b) 스텁이 422를 돌려주도록
  정한 SHA로 같은 호출
- **기대 결과**: (a) 스텁이 `statuses/{sha}`에 네 필드를 받았고, 도구 응답의 `id`·`state`·`context`·
  `sha`·`created_at`이 스텁 응답과 같다. (b) 스텁의 422 문면을 담은 도구 에러
- **검증 AC**: AC1
- **자동화**: Go 단위 `TestCommitStatusCreatesStatus`·`TestCommitStatusSurfacesGitHubError`
  (`internal/github/commitstatus_test.go`, 2026-09-20 착지). 통합은 **(미작성)** —
  `github_commit_status_ac1.py` 계획

### 시나리오 2: 내부 토큰 스코프와 비노출
- **사전 조건**: 시나리오 1과 동일
- **실행 단계**: 시나리오 1(a)의 호출과 1(b)의 실패 호출을 각각 한 뒤, 스텁의 기록 엔드포인트에서
  토큰 발급 요청 본문을 읽는다
- **기대 결과**: 발급 요청 본문이 `{"repositories":["test"],"permissions":{"statuses":"write"}}`와
  같다. 성공·실패 두 도구 결과를 직렬화한 전체에 스텁이 발급한 토큰 문자열(`ghs_mock_…`), PEM
  아머, `eyJ`로 시작하는 JWT가 없다
- **검증 AC**: AC2
- **자동화**: Go 단위 `TestCommitStatusMintsNarrowToken`·`TestCommitStatusNeverReturnsToken`
  (발급 본문을 문자열로 대조하고, 성공·실패 두 결과를 직렬화해 토큰·PEM·JWT 부재를 단언한다).
  `TestCommitStatusDiscardsItsToken` 이 「쓰고 버린다」의 폐기 호출까지 잰다. 통합은 **(미작성)** —
  `github_commit_status_ac2.py` 계획

### 시나리오 3: 잘못된 입력은 상류에 닿지 않는다
- **사전 조건**: 시나리오 1과 동일, 스텁 요청 카운터 초기화
- **실행 단계**: 다음 여섯 호출 — `repository` 누락 · `sha=abc1234`(축약) · `state=ok` ·
  `context` 누락 · 141자 `description` · `target_url=ftp://x`
- **기대 결과**: 여섯 모두 도구 에러이고 스텁 요청 카운터가 0이다(토큰 발급 요청도 0)
- **검증 AC**: AC3
- **자동화**: Go 단위 `TestCommitStatusValidatesBeforeGitHub`(표 기반 6행 — 각 행이 상류 요청 수 0을
  단언한다, 2026-09-20 착지). 통합 e2e 는 `tests/integration/github_commit_status_ac3.py` 가 여섯
  호출을 한 창에 몰아 넣고 끝에 github-mock 의 요청 로그가 비어 있음을 단언한다 — 거부가 입력
  검증에서 왔다는 것은 필드를 지목하는 문면으로 가르고, 「로그가 비어 있다」가 공허하지 않다는
  것은 유효한 한 벌이 로그를 남기는 포지티브 컨트롤로 가른다

### 시나리오 4: context 네임스페이스 밖은 거부
- **사전 조건**: (a) 시나리오 1과 동일한 배포, (b) `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES` 미설정
  배포
- **실행 단계**: (a)에서 `context=ci/build`로 호출, 이어서 `context=homelab-k3s-mcp/e2e`로 호출.
  (b)에서 `context=homelab-k3s-mcp/e2e`로 호출
- **기대 결과**: (a) 첫 호출은 허용 접두사 목록을 보여 주는 도구 에러이고 상류 요청 0, 둘째 호출은
  기록된다. (b) 도구 에러이고 상류 요청 0
- **검증 AC**: AC4
- **자동화**: Go 단위 `TestCommitStatusRejectsForeignContext`(거부 문면이 허용 접두사 목록을 보여 주는
  것까지)·`TestCommitStatusRefusesWhenNoPrefixConfigured`(2026-09-20 착지). 통합은 **(미작성)** —
  `github_commit_status_ac4.py` 계획 (b는 env 한 줄이 다른 배포 변형이 필요하다)

### 시나리오 5: 미설정 시 도구 에러
- **사전 조건**: GitHub App env 미설정 배포(`auth-fixture.yaml` 변형)
- **실행 단계**: 유효한 입력으로 호출한 뒤 `ping`
- **기대 결과**: `github app unavailable: …` 도구 에러, 직후 `ping` 정상
- **검증 AC**: AC5
- **자동화**: Go 단위 `TestCommitStatusUnavailableReturnsToolError`(2026-09-20 착지). 통합 e2e 는
  `tests/integration/github_commit_status_ac5.py` 가 auth-variant 배포(`auth-fixture.yaml`)에서
  유효한 입력의 거부 문면과 직후 `ping` 을 함께 단언한다

### 시나리오 6: 어노테이션 광고
- **사전 조건**: 임의 배포
- **실행 단계**: `tools/list`
- **기대 결과**: `github_commit_status_create`의 `readOnlyHint=false`, `destructiveHint=false`,
  `openWorldHint=true`
- **검증 AC**: AC6
- **자동화**: Go 단위 `TestToolsListAdvertisesCommitStatus`(`internal/server/mcp_test.go`,
  세 힌트를 이름으로 단언, 2026-09-20 착지). 통합 e2e 는
  `tests/integration/github_commit_status_ac6.py` 가 배포된 서버의 `tools/list` 에서 세 힌트를
  한 벌로 되읽는다
