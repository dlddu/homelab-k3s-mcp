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
