# PRD: github_app_installation_token

서버에 구성된 GitHub App 설치에 대해 단명 설치 토큰을 발급하는 도구.

## 달성 가치
- **V2: 단명·최소권한 자격증명** — 장수 PAT 대신 약 1시간 수명의 스코프된 토큰을 발급한다.
- **V3: 안전한 운영(Safe-by-default)** — App 개인키는 서버에만 두고, 미설정 시 graceful하게
  거부한다.

## 도구 개요
- 입력: `repositories`(선택, repo 이름 배열), `permissions`(선택, 권한→레벨 맵)
- 출력: `text/plain` .env 형식(`GITHUB_TOKEN=...`)과 만료·스코프 주석
- 서버 요구 설정: `GITHUB_APP_CLIENT_ID`, `GITHUB_APP_INSTALLATION_ID`, `GITHUB_APP_PRIVATE_KEY`
- 어노테이션: `readOnlyHint=false`, `destructiveHint=false`, `idempotentHint=false`, `openWorldHint=true`
- **발급하지 않는 권한 레벨**: `statuses: write`(commit status 쓰기). 쓰기는 토큰으로 내주지 않고
  [`github_commit_status_create`](prd-github-commit-status.md)가 좁은 동작으로만 행사한다(AC5).
  `statuses: read`(조회)는 이 도구에서 그대로 발급한다.

## Acceptance Criteria

### AC1: 단명 설치 토큰 발급
- **설명**: App 개인키로 서명한 단명 JWT로 설치 토큰을 교환해, 만료 시각(약 1시간 후)을 포함한
  .env 형식으로 반환한다.
- **달성 가치**: V2
- **검증 방법**: 반환 토큰의 만료가 발급 시점 기준 약 1시간 이내이고, .env에 만료·스코프 주석이
  포함된다.

### AC2: 스코프 제한
- **설명**: `repositories`/`permissions`로 토큰을 설치 repo·권한의 부분집합으로 좁힌다. 미지정
  시 설치된 전체 repo 범위로 발급된다.
- **달성 가치**: V2
- **검증 방법**: 요청한 repo/권한 스코프가 발급 토큰에 반영되고, App 설치 범위를 벗어난 요청은
  거부된다.

### AC3: 미설정 시 graceful 거부
- **설명**: 필수 서버 설정이 없으면 unavailable 류 에러를 반환하며, 서버 기동·다른 도구에는
  영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.

### AC4: 베이스 키 비노출
- **설명**: App 개인키는 응답에 포함되지 않으며, 노출되는 것은 만료가 있는 설치 토큰뿐이다.
- **달성 가치**: V2, V3
- **검증 방법**: 응답 페이로드에 개인키가 존재하지 않는다.

### AC5: commit status 쓰기 권한 배제 (읽기는 발급)
- **설명**: 이 도구가 발급하는 토큰에는 어떤 경로로도 `statuses: write`가 실리지 않는다.
  `statuses: read`는 다른 권한과 똑같이 발급한다 — 조회는 필수 검사를 위조할 수 없기 때문이다.
  1. `permissions`에 `statuses: write`가 있으면 GitHub에 요청하기 전에 거부하고, 문면에
     `github_commit_status_create`를 쓰라고 안내한다. `statuses: read`는 그대로 요청한다.
  2. `permissions`를 생략하면 GitHub의 「생략 = 설치 권한 전부」에 기대지 않는다. 서버가 설치의
     권한 목록(`GET /app/installations/{id}`)을 읽어, 설치에 `statuses: write`가 있으면 그 항목만
     `statuses: read`로 **낮춘 명시적 맵**으로 요청한다. 그 목록을 읽지 못하면 발급을 거부한다 —
     생략 요청으로 폴백하지 않는다.
  3. GitHub가 돌려준 토큰의 `permissions`에 그래도 `statuses: write`가 있으면 그 토큰을
     폐기(`DELETE /installation/token`)하고 에러를 반환한다. 토큰 문자열은 응답에 싣지 않는다.
- **달성 가치**: V2, V3
- **검증 방법**: (1) `permissions={statuses: write}`·`{contents: read, statuses: write}` 호출이
  거부되고 상류 토큰 발급 요청이 0이다. `permissions={statuses: read}`는 발급되고 응답
  `# Permissions:` 주석이 `statuses=read`다. (2) 설치 권한에 `statuses: write`가 있는 상류에서 인자
  없이 호출하면, 상류가 받은 발급 요청 본문에 `permissions`가 명시돼 있고 `statuses`가 `read`이며,
  응답 주석도 `statuses=read`다. 설치 권한 조회가 실패하면 발급 요청 없이 에러다. (3) 상류가
  `statuses: write`가 실린 토큰을 돌려주면 폐기 요청이 한 번 가고, 도구 결과는 에러이며 그 토큰
  문자열을 포함하지 않는다.
- **호환성**: 인자 없는 기존 호출의 결과는 `statuses`가 `read`로 낮아지는 것 외에는 같다.
  2026-09-19 현재 설치에는 `statuses` 권한이 아예 없으므로 오늘 호출자가 보는 차이는 0이다 — 이 AC는
  App에 그 권한을 추가하기 **전에** 배포돼야 한다([배포 순서](prd-github-commit-status.md)).
