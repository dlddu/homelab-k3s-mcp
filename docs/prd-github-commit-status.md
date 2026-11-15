# PRD: github_commit_status_request

GitHub 커밋 하나에 붙일 **commit status 하나를 사람에게 승인 요청**하는 도구. 도구는 요청을
PocketAide(`dlddu/pocket-aide`)에 남기고 **바로 돌아온다** — 기다리지 않고, GitHub 에 아무것도
쓰지 않고, 자격증명을 돌려주지 않는다. 이 서버를 PocketAide 호출자로 등록한 사람이 앱에서 승인하면
PocketAide 가 **자기 GitHub App** 으로 그 status 를 기록한다. 에이전트는 PR(커밋)의 status 를 지켜보다가
status 가 붙으면 작업을 이어 간다.

> 2026-11-12 이전 이 PRD 는 `github_commit_status_create` — 서버가 안에서 설치 토큰을 발급해 status 를
> **바로 쓰는** 도구 — 였다. 경위는 아래 「배경」, 이력은 `doc-tracker/` 2026-11 과 `values.md` 이력.

## 배경

commit status 는 브랜치 보호의 필수 검사(required status check)가 읽는 신호다. 그래서 쓰기
(`statuses: write`)는 `github_app_installation_token` 으로 **토큰째 내주지 않는다**
([github_app_installation_token AC5](prd-github-app-installation-token.md#AC5)). 처음에는 이 서버가 허용된
context 네임스페이스 안에서 status 하나를 대신 써 주는 좁은 동작으로 열었다(`github_commit_status_create`,
2026-09-19). 그런데 이 서버가 쓰는 대표 context 인 `review/manual-approval` 은 「사람이 PR 을 봤다」는
판정 **그 자체**라, 접두사가 남의 context 위조는 막아도 사람 없는 기록(무인 에이전트의 자기 승인)은
막지 못했다. 동기 승인 게이트를 붙여 봤으나(2026-10-23) MCP 커넥터의 30초 응답 제한 안에 사람이
결정하지 못하면 호출이 끊겨 되돌렸다(2026-10-24).

그래서 2026-11-12 도구를 **비동기 요청**으로 바꿨다. 요청 내역은 이 서버가 아니라 PocketAide DB 에
쌓인다 — 이 서버는 상태를 갖지 않는다(stateless). 기록은 승인이 일어나는 PocketAide 가 자기 App 으로
하므로 「사람이 승인했다 ⇔ status 가 생겼다」가 PocketAide 한 곳에서 성립하고, 이 서버에는 status 를
쓰는 경로 자체가 없다.

## 달성 가치
- **V2: 단명·최소권한 자격증명** — commit status 쓰기는 토큰으로도, 이 서버의 동작으로도 나가지 않는다.
  쓰는 쪽(PocketAide)도 승인마다 그 저장소·`statuses: write` 로 좁힌 토큰을 받아 쓰고 버린다.
- **V3: 안전한 운영(Safe-by-default)** — 요청할 수 있는 context 를 서버가 정한 네임스페이스로 제한하고,
  네임스페이스가 설정되지 않으면 요청하지 않는다. 사람이 승인하지 않은 status 는 생기지 않는다.

## 도구 개요
- 입력
  - `repository` (필수) — `owner/name`. PocketAide 의 GitHub App 이 설치된 저장소여야 승인 시 기록된다.
  - `sha` (필수) — 40자 16진 전체 커밋 SHA. 축약 SHA·브랜치·태그 이름은 받지 않는다.
  - `state` (필수) — `error` · `failure` · `pending` · `success` 중 하나.
  - `context` (필수) — status 라벨, 255자 이하. 허용 접두사(AC4) 안이어야 한다.
  - `requester` (필수) — 누가·무엇을 하다 요청하는지 호출자가 쓴 문장, 공백 제거 후 1~200자. 승인
    화면에 그대로 보인다(검증되지 않은 값).
  - `description` (선택) — 140자 이하. `target_url` (선택) — `https://`·`http://` 절대 URL.
  - `explanation` (선택) — 왜 이 status 를 붙여야 하는지 에이전트가 쓴 설명, 2000자 이하. PocketAide 에는
    `reason` 으로 간다. 승인 게이트의 `reason` 과 이름을 달리한 것은 이 도구가 게이트를 기다리지 않기
    때문이다(디스패처는 `reason` 을 게이트 도구에만 허용한다 — `prd-approval-gate`).
- 출력: PocketAide 가 돌려준 요청 `id`, `status`(`PENDING`), `created_at`, 그리고 「승인 전에는 GitHub 에
  아무것도 쓰이지 않는다 — 커밋의 status 를 지켜보라」는 안내 문장.
- 요청 본문: `POST {APPROVAL_GATE_URL}/api/external/commit-status-requests` 에 `owner`·`repo`·`sha`(소문자)·
  `state`·`context`·`description`·`targetUrl`·`requester`·`reason`·`sessionId`(MCP 요청의
  `X-Session-Platform-Session-Id` 헤더). 인증은 승인 게이트·`pocket_aide_notification_send` 와 같은
  ServiceAccount 토큰(audience `pocket-aide`).
- 서버 요구 설정
  - `APPROVAL_GATE_URL`(+ 투영 토큰) — 승인 게이트와 공유
  - `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES` — 요청할 수 있는 context 접두사 목록(쉼표 구분). 운영 `review/`
- PocketAide 쪽 전제: 커밋 상태 요청 엔드포인트가 있는 판(dlddu/pocket-aide PRD-12 AC18~AC21)이 먼저
  배포돼 있을 것. 없으면 404 를 그 사실을 말하는 도구 에러로 돌려준다. 기록하려면 PocketAide 에 GitHub App
  (Commit statuses: Read and write) 자격 증명이 있어야 한다 — 없으면 요청은 쌓이지만 앱에서 승인할 수 없다.
- 어노테이션: `readOnlyHint=false`, `destructiveHint=false`, `idempotentHint=false`, `openWorldHint=true`

## 범위 밖 (근거와 함께)
- **승인 게이트(동기 승인)를 타지 않는다.** 이 도구 자체가 승인을 「요청」하는 것이고, 결정을 기다리지
  않는다 — 기다리면 30초 커넥터 제한에 다시 걸린다. 그래서 `reason` 을 받지 않고(대신 `explanation`),
  `doc-tracker/` 의 「게이트 밖 예외」 계수에도 들어가지 않는다(클러스터 쓰기가 아니다).
- **결과 조회·폴링 도구를 두지 않는다.** 결과는 GitHub 에 status 로 나타나고, 에이전트는 이미 그것을 PR
  화면·`statuses: read` 토큰으로 본다. 거절은 아무것도 쓰지 않는다.
- **status 를 직접 쓰지 않는다.** 이 서버의 GitHub App 으로 쓰는 경로는 2026-11-12 걷었다.
- **check run(`checks` 권한)은 다루지 않는다.** 토큰 도구가 `checks: write` 를 배제하는 것은 그 PRD AC5 다.

## Acceptance Criteria

### AC1: 요청은 PocketAide 에 쌓이고 도구는 바로 돌아온다
- **설명**: 유효한 입력으로 호출하면 서버는 위 요청 본문으로 PocketAide 에 요청을 하나 만들고, PocketAide
  가 돌려준 `id`·`status`(`PENDING`)·`created_at` 을 결정을 기다리지 않고 바로 반환한다. 요청은 이 서버를
  등록한 사용자의 것이 되고 실행에 필요한 값(저장소·SHA·state·context·description·target_url)을 모두
  담는다. PocketAide 의 거부는 사유를 구분해 도구 에러로 돌려준다 — 401(호출자 미등록), 404(엔드포인트
  없는 옛 PocketAide), 400(PocketAide 문면), 그 밖의 상태 코드.
- **달성 가치**: V3
- **검증 방법**: 호출 결과가 `PENDING` 과 id 를 담아 결정 없이 돌아오고, 같은 id 의 요청이 등록 사용자의
  PocketAide 커밋 상태 요청에 입력과 같은 값으로 있다. 그 사이 GitHub 에 status 쓰기 요청은 없다.

### AC2: 서버는 GitHub 에 닿지 않고 자격증명을 다루지 않는다
- **설명**: 이 도구는 GitHub API 를 부르지 않는다 — 설치 토큰 발급·status 기록·토큰 폐기 어느 것도 하지
  않는다. 도구 응답·에러 문면에 설치 토큰·PEM 아머·JWT 가 없다.
- **달성 가치**: V2
- **검증 방법**: 호출 동안 GitHub(스텁)가 받은 요청이 0 이다. 같은 배포에서 토큰 도구를 부르면 스텁이
  요청을 받는다(양성 대조). 직렬화한 도구 결과에 `ghs_`·PEM·`eyJ` 가 없다.

### AC3: 입력 검증은 PocketAide 호출보다 앞선다
- **설명**: `repository` 누락·`owner/name` 아님, 40자 16진이 아닌 `sha`, 네 값 밖의 `state`, 누락·255자 초과
  `context`, 140자 초과 `description`, http(s) 절대 URL 이 아닌 `target_url`, 누락·200자 초과 `requester`,
  2000자 초과 `explanation`, 문자열 아닌 값은 거부한다. 문면은 `invalid commit status request: …` 로
  어느 필드인지 지목한다(문자열 아닌 값은 JSON-RPC 인자 오류). 거부된 호출은 PocketAide 에 요청을 만들지
  않는다.
- **달성 가치**: V3
- **검증 방법**: 위 경우 각각이 도구 에러이고 등록 사용자의 커밋 상태 요청이 늘지 않는다. 유효한 한 벌은
  요청을 남긴다(양성 대조).

### AC4: context 네임스페이스 제한 (fail-closed)
- **설명**: `context` 가 `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES` 의 접두사 중 하나로 시작할 때만 요청한다.
  접두사 목록이 비어 있거나 설정되지 않았으면 **모든 호출을 거부**한다 — 「설정 안 함 = 전부 허용」이면
  남의 필수 검사 이름으로도 승인 요청이 쌓여 사람이 실수로 승인할 여지가 기본값이 된다. 거부 문면은 허용
  접두사 목록(또는 env 이름)을 보여 준다. 판정은 AC3 과 같이 PocketAide 호출 전에 한다.
- **달성 가치**: V3
- **검증 방법**: 허용 접두사 밖의 context(예: `ci/build`)는 거부되고 요청이 쌓이지 않는다. 접두사 env 가
  없는 배포에서는 허용 접두사처럼 보이는 context 도 거부된다. 허용 접두사 안의 context 는 AC1 대로 쌓인다.

### AC5: PocketAide 미설정 시 graceful 거부
- **설명**: `APPROVAL_GATE_URL` 이 없으면 `pocket-aide unavailable: …` 도구 에러를 반환하며, 서버 기동·다른
  도구에는 영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env 가 비어 있는 배포에서 유효한 입력의 호출이 그 문면으로 거부되고 직후 `ping` 이
  정상이다.

### AC6: 비파괴·외부 쓰기 광고
- **설명**: `tools/list` 가 이 도구를 `readOnlyHint=false`, `destructiveHint=false`, `idempotentHint=false`,
  `openWorldHint=true` 로 광고한다. 외부 시스템(PocketAide)에 요청을 남기고 같은 호출을 되풀이하면 요청이
  하나 더 쌓이지만, 아무것도 지우거나 고치지 않는다.
- **달성 가치**: V3
- **검증 방법**: `tools/list` 응답의 이 도구 어노테이션이 위 네 값이다.
