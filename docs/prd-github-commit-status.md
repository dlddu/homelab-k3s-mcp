# PRD: github_commit_status_create

서버에 구성된 GitHub App으로 커밋 하나에 **commit status 하나를 기록**하는 도구.
호출자는 자격증명을 받지 않는다 — 서버가 그 호출 하나에 필요한 만큼만 좁힌 설치 토큰을 안에서
발급해 쓰고 버린다.

## 배경

commit status는 브랜치 보호의 필수 검사(required status check)가 읽는 신호다. 이 권한을 가진
토큰은 `ci/build` 같은 **남의 context에 `success`를 적어 필수 검사를 통과시킬 수 있다.** 그래서
**쓰기**(`statuses: write`)는 `github_app_installation_token`으로 **토큰째 내주지 않고**
([github_app_installation_token AC5](prd-github-app-installation-token.md#AC5)), 이 도구가 정한
좁은 동작 — 허용된 context 네임스페이스 안에 status 하나를 적는 것 — 으로만 행사한다.
**읽기**(`statuses: read`)는 위조 위험이 없으므로 토큰 도구에서 그대로 발급한다 — 이 도구는
쓰기만 맡는다.

## 달성 가치
- **V2: 단명·최소권한 자격증명** — commit status 쓰기가 운영자 손에 토큰으로 들어오지 않는다.
  서버 안에서 발급하는 토큰도 `statuses: write` 하나와 대상 repo 하나로 좁혀지고 응답에 실리지
  않는다.
- **V3: 안전한 운영(Safe-by-default)** — 쓸 수 있는 context를 서버가 정한 네임스페이스로 제한해
  다른 시스템의 검사를 위조하지 못하게 하고, 네임스페이스가 설정되지 않으면 기록하지 않는다.

## 도구 개요
- 입력
  - `repository` (필수) — owner를 뺀 repo 이름. App이 설치된 repo여야 한다.
  - `sha` (필수) — 40자 16진 전체 커밋 SHA. 축약 SHA·브랜치·태그 이름은 받지 않는다.
  - `state` (필수) — `error` · `failure` · `pending` · `success` 중 하나.
  - `context` (필수) — status의 식별 라벨. GitHub 기본값(`default`)에 기대지 않는다.
  - `description` (선택) — 140자 이하.
  - `target_url` (선택) — `https://` 또는 `http://` 절대 URL.
- 출력: 생성된 status의 `id`·`state`·`context`·`sha`·`description`·`target_url`·`created_at`.
  토큰은 포함하지 않는다.
- 서버 요구 설정
  - `GITHUB_APP_CLIENT_ID`, `GITHUB_APP_INSTALLATION_ID`, `GITHUB_APP_PRIVATE_KEY` — 토큰 도구와 공유
  - `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES` — 쓸 수 있는 context 접두사 목록(쉼표 구분). 예:
    `homelab-k3s-mcp/,reconciler/`
- GitHub 쪽 전제: App 권한에 **Commit statuses: Read and write**가 있고 설치 소유자가 그 권한
  변경을 승인했을 것. 2026-09-19 실측 기준 **현재 설치에는 이 권한이 없다**(`statuses: write` 요청이
  422 — "not granted to this installation").
- 어노테이션: `readOnlyHint=false`, `destructiveHint=false`, `idempotentHint=false`,
  `openWorldHint=true`

### 배포 순서 (필수)

App에 `statuses` 권한을 **먼저** 부여하면, 그 순간부터 `permissions`를 생략한 기존 토큰 호출이
설치 권한 전부 — `statuses: write` 포함 — 를 받는다. AC5는 이를 `statuses: read`로 낮춘다. 따라서 순서는 다음으로 고정한다.

1. `github_app_installation_token` AC5(statuses 쓰기 배제)를 배포한다.
2. GitHub App 권한에 Commit statuses: Read and write를 추가하고 설치 소유자가 승인한다.
3. 이 도구를 배포한다(1과 같은 릴리스여도 된다 — 2 이전에는 AC1이 GitHub 422로 실패할 뿐이다).

## 범위 밖 (근거와 함께)
- **승인 게이트(gatekeeper)를 타지 않는다.** 게이트의 보증은 「승인 없이는 **클러스터 상태**가
  바뀌지 않는다」이고(values V3), commit status는 클러스터 밖 GitHub의 추가 전용(append-only)
  기록이라 그 범위가 아니다. 위조 위험은 게이트 대신 AC4의 context 네임스페이스가 막는다. 그래서
  `doc-tracker/`의 「게이트 밖 예외」(클러스터 쓰기 중 게이트를 타지 않는 것) 계수에도 들어가지
  않는다.
- **status 조회·목록·삭제는 다루지 않는다.** GitHub API에 status 삭제는 없고, 조회는
  `github_app_installation_token`에서 `statuses: read` 토큰을 받아 한다(그 PRD AC5).
- **check run(`checks` 권한)은 다루지 않는다.** 다른 API·다른 권한이며, 그 권한을 토큰 도구에서
  배제할지는 이 PRD가 정하지 않는다(`doc-tracker/` 미결 사항에 등재).

## Acceptance Criteria

### AC1: 커밋 status 기록
- **설명**: 유효한 입력으로 호출하면 `POST /repos/{owner}/{repository}/statuses/{sha}`로 status를
  하나 만들고, 생성된 status를 출력 형식대로 반환한다. `owner`는 설치 계정이며 호출자가 넘기지
  않는다. GitHub 오류(커밋 없음 422, repo 미설치, 권한 없음 등)는 GitHub 문면을 담은 도구 에러로
  돌려준다.
- **달성 가치**: V2
- **검증 방법**: 호출 뒤 같은 `sha`·`context`의 status가 요청한 `state`·`description`·`target_url`로
  상류에 기록되어 있고, 도구 응답의 `id`·`created_at`이 그 기록과 같다. 존재하지 않는 SHA는
  GitHub 문면을 담은 도구 에러가 된다.

### AC2: 내부 토큰의 최소 스코프·비노출
- **설명**: 서버는 호출마다 `repositories=[repository]`, `permissions={statuses: write}`로 좁힌 설치
  토큰을 발급해 그 호출 하나에만 쓴다. 이 토큰과 App 개인키·JWT는 도구 응답·에러 문면·로그 어디에도
  나타나지 않는다.
- **달성 가치**: V2, V3
- **검증 방법**: 상류가 받은 토큰 발급 요청의 본문이 정확히 그 두 값이다. 직렬화한 도구 결과
  전체(성공·실패 모두)에 설치 토큰·PEM 아머·App JWT가 없다.

### AC3: 입력 검증은 GitHub 호출보다 앞선다
- **설명**: `repository` 누락·빈 값, 40자 16진이 아닌 `sha`, 네 값 밖의 `state`, 누락된 `context`,
  140자를 넘는 `description`, http(s) 절대 URL이 아닌 `target_url`은 거부한다. 거부된 호출은
  토큰을 발급하지 않고 GitHub에 요청을 하나도 보내지 않는다.
- **달성 가치**: V3
- **검증 방법**: 위 여섯 경우 각각이 도구 에러로 돌아오고, 그동안 상류가 받은 요청 수가 0이다.

### AC4: context 네임스페이스 제한 (fail-closed)
- **설명**: `context`가 `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES`의 접두사 중 하나로 시작할 때만
  기록한다. 접두사 목록이 비어 있거나 설정되지 않았으면 **모든 호출을 거부**한다 — 「설정 안 함 =
  전부 허용」이면 다른 시스템의 필수 검사를 위조할 수 있는 상태가 기본값이 된다. 거부 문면은 허용
  접두사 목록을 보여 준다. 판정은 AC3과 같이 GitHub 호출 전에 한다.
- **달성 가치**: V3
- **검증 방법**: 허용 접두사 밖의 context(예: `ci/build`)는 거부되고 상류 요청 수가 0이다. 접두사
  env가 없는 배포에서는 허용 접두사처럼 보이는 context도 거부된다. 허용 접두사 안의 context는
  AC1대로 기록된다.

### AC5: 미설정 시 graceful 거부
- **설명**: GitHub App 필수 설정이 없으면 unavailable 류 에러를 반환하며, 서버 기동·다른 도구에는
  영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 직후 `ping`이 정상이다.

### AC6: 비파괴·외부 쓰기 광고
- **설명**: `tools/list`가 이 도구를 `readOnlyHint=false`, `destructiveHint=false`,
  `openWorldHint=true`로 광고한다. status는 이력에 추가될 뿐 기존 기록을 지우거나 고치지 않으므로
  파괴적이지 않지만, 외부 시스템에 쓰는 도구임은 클라이언트가 알 수 있어야 한다.
- **달성 가치**: V3
- **검증 방법**: `tools/list` 응답의 이 도구 어노테이션이 위 세 값이다.
