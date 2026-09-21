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
(`/_admin/requests`)와 응답 모드 노브(`/_admin/config`)는 토큰 도구 AC5 작업이 세웠고,
**`POST /repos/{owner}/{repo}/statuses/{sha}`(422 갈래 포함)** 와
**`GET /app/installations/{id}` 응답의 `account.login`** 은 2026-09-20 에 섰다
(`rct_20260920-0001` — `docs/e2e-mocking-policy.md` 의 `github-mock` 등재 「대상」 확대와 같은 PR).
**픽스처 쪽 선행은 이제 없다** — 남은 것은 아래 각 시나리오의 전용 e2e 저작뿐이다.

그 스텁이 세우는 계약 셋은 e2e 가 그대로 기대해도 되는 값이다:

| 계약 | 값 |
| --- | --- |
| 설치 계정 login(= 경로의 `{owner}`) | `dlddu` |
| 성공 응답 | `201` · `{"id": 1, "state", "context", "created_at": "2026-01-01T00:00:00Z"}` (+ 보낸 `description`·`target_url`). **`sha` 는 싣지 않는다** |
| 422 갈래를 여는 `sha` | `f` 40자 (`"f" * 40`). 그 밖의 40자 hex 는 전부 성공 갈래다 |

`sha` 를 싣지 않는 것도 의도된 선택이다 — 실 GitHub 의 status 생성 응답에 `sha` 키가 없다. 스텁이 그것을
실어 주면 도구가 응답을 그대로 넘기기만 해도 「응답의 `sha` 가 요청한 값이다」가 통과하는데, 운영에서는
빈 문자열이 나간다(2026-09-21 운영 호출에서 실제로 그렇게 드러났다).

`id` 가 1(≠0)인 것은 의도된 선택이다 — 스텁이 0 을 돌려주면 도구가 `id` 를 **파싱하지 못한**
경우의 Go 제로값과 구분되지 않아, 「도구 응답의 `id` 가 스텁 응답과 같다」가 공허하게 통과한다.

## 테스트 시나리오

### 시나리오 1: status 기록과 GitHub 오류 전달
- **사전 조건**: github-mock 구성, `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES=homelab-k3s-mcp/`
- **실행 단계**: (a) `repository=test`, 40자 `sha`, `state=success`,
  `context=homelab-k3s-mcp/e2e`, `description`, `target_url`로 호출. (b) 스텁이 422를 돌려주도록
  정한 SHA로 같은 호출
- **기대 결과**: (a) 스텁이 `statuses/{sha}`에 네 필드를 받았고, 도구 응답의 `id`·`state`·`context`·
  `created_at`이 스텁 응답과 같으며 `sha` 는 요청한 값이다(스텁 응답에는 없다 — 픽스처 절). (b) 스텁의 422 문면을 담은 도구 에러
- **검증 AC**: AC1
- **자동화**: Go 단위 `TestCommitStatusCreatesStatus`·`TestCommitStatusSurfacesGitHubError`
  (`internal/github/commitstatus_test.go`, 2026-09-20 착지). 통합 e2e 는
  `tests/integration/github_commit_status_ac1.py` 가 (a) 를 양 끝에서 잰다 — github-mock 의
  요청 기록에서 `statuses/{sha}` 본문의 네 필드를, 도구 응답에서 `id`·`state`·`context`·
  `created_at` 이 스텁 계약과 같고 `sha` 가 요청값임을 — 그리고 (b) 의 422 문면이 스텁의
  `message` 를 축자로 담는지와 그 거부가 실제 기록된 상류 응답에서 왔는지를 함께 단언한다

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
  `TestCommitStatusDiscardsItsToken` 이 「쓰고 버린다」의 폐기 호출까지 잰다. 통합 e2e 는
  `tests/integration/github_commit_status_ac2.py` 가 성공·실패 호출 각각의 발급 요청 본문을
  github-mock 기록에서 읽어 JSON 동치를 단언하고, 두 결과의 직렬화 전체에 토큰·PEM·JWT 가
  없음을 재되, 같은 기록의 폐기 요청(`DELETE /installation/token`) 헤더에 그 토큰이 실려
  있음을 포지티브 컨트롤로 삼아 「없다」가 공허하지 않게 한다

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
  것까지)·`TestCommitStatusRefusesWhenNoPrefixConfigured`(2026-09-20 착지). 통합 e2e 는
  `tests/integration/github_commit_status_ac4.py` 한 파일이 두 배포를 대조한다 — (a) 는 primary
  에서 거부 문면의 허용 접두사 목록과 상류 요청 0, 이어지는 자기 접두사 호출의 기록을 재고,
  (b) 는 접두사 env 만 없는 전용 변형(`tests/k8s/kind/commit-status-variant.yaml`)에 자기
  포트포워드로 닿아 거부 문면이 `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES` 를 지목하는지(= App
  미설정의 `unavailable` 문면이 아님)와 상류 요청 0 을 잰다

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
