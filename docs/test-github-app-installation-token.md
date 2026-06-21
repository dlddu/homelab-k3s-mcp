# 테스트 문서: github_app_installation_token

## 검증 대상 AC
- AC1: 단명 설치 토큰 발급 (PRD: github_app_installation_token)
- AC2: 스코프 제한 (PRD: github_app_installation_token)
- AC3: 미설정 시 graceful 거부 (PRD: github_app_installation_token)
- AC4: 베이스 키 비노출 (PRD: github_app_installation_token)

## 테스트 시나리오

### 시나리오 1: 기본 토큰 발급(.env + 만료·스코프 주석)
- **사전 조건**: github-mock 구성(설치 ID 67890)
- **실행 단계**: 인자 없이 호출
- **기대 결과**: text/plain 리소스에 `GITHUB_TOKEN=...`, `# Expires at:` 주석,
  `# Repository selection: all`, `contents=` 포함
- **검증 AC**: AC1
- **자동화**: Go 단위 `mcp_test.go::TestGitHubTokenDispatchesWithDefaults`. 통합
  `github_app.py`(defaults). 참고: 실제 ~1시간 TTL은 GitHub 측 동작이며 mock은 고정 만료를 사용.

### 시나리오 2: repo/권한 스코프 제한
- **사전 조건**: 동일
- **실행 단계**: repositories=[homelab-k3s-mcp], permissions={contents:read}로 호출;
  repositories에 비배열 전달도 호출
- **기대 결과**: `# Repository selection: selected`, `# Permissions: contents=read` 반영.
  비배열 repositories는 거부.
- **검증 AC**: AC2
- **자동화**: Go 단위 `mcp_test.go::TestGitHubTokenPassesThroughScope`,
  `TestGitHubTokenRejectsNonArrayRepositories`. 통합 `github_app.py`(with scope).

### 시나리오 3: 미설정 시 도구 에러
- **사전 조건**: GitHub App env 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러
- **검증 AC**: AC3
- **자동화**: Go 단위 `mcp_test.go::TestGitHubTokenUnavailableReturnsToolError`.

### 시나리오 4: 개인키 비노출
- **사전 조건**: 동일(구성됨)
- **실행 단계**: 발급 결과 검사
- **기대 결과**: 출력은 설치 토큰·만료·스코프 주석뿐이며 App 개인키 미포함
- **검증 AC**: AC4
- **자동화**: 부분 — 출력 내용 검증으로 간접 확인(통합 `github_app.py`). 개인키 부재를 명시적으로
  단언하는 케이스 추가 권장.
