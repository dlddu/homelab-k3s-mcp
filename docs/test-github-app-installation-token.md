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
- **자동화**: 통합 `github_app.py::test_github_app_installation_token_ac1_short_lived_token`
  — 서명 JWT ↔ 설치 토큰 교환이 실제로 일어났음을(`ghs_mock_67890`) + 만료·스코프 주석이 담긴
  .env 형태를 단언. Go 단위 `mcp_test.go::TestGitHubTokenDispatchesWithDefaults`.
  참고: 실제 ~1시간 TTL은 GitHub 측 동작이며 mock은 고정 만료(2099-01-01)를 사용하므로
  TTL 자체는 e2e에서 관측 불가.

### 시나리오 2: repo/권한 스코프 제한
- **사전 조건**: 동일
- **실행 단계**: repositories=[homelab-k3s-mcp], permissions={contents:read}로 호출;
  repositories에 비배열 전달도 호출
- **기대 결과**: `# Repository selection: selected`, `# Permissions: contents=read` 반영.
  비배열 repositories는 거부.
- **검증 AC**: AC2
- **자동화**: 통합 `github_app.py::test_github_app_installation_token_ac2_scope_restriction`
  — 미지정 시 `Repository selection: all`(기본 권한 동봉), 지정 시 `selected` +
  `Permissions: contents=read`로 좁혀지고 기본 `metadata=read`가 사라짐을 단언. Go 단위
  `mcp_test.go::TestGitHubTokenPassesThroughScope`,
  `TestGitHubTokenRejectsNonArrayRepositories`. 참고: 설치 범위 밖 repo 거부는 GitHub 측
  동작이라 mock으로는 관측 불가.

### 시나리오 3: 미설정 시 도구 에러
- **사전 조건**: GitHub App env 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러
- **검증 AC**: AC3
- **자동화**: Go 단위 `mcp_test.go::TestGitHubTokenUnavailableReturnsToolError`. 통합
  `tests/integration/no_config.py::test_github_app_installation_token_ac3_unconfigured_refusal`
  (자격증명 미부착 배포 변형에서 unavailable 도구 에러 반환 + 직후 ping 정상).

### 시나리오 4: 개인키 비노출
- **사전 조건**: 동일(구성됨)
- **실행 단계**: 발급 결과 검사
- **기대 결과**: 출력은 설치 토큰·만료·스코프 주석뿐이며 App 개인키 미포함
- **검증 AC**: AC4
- **자동화**: 통합 `github_app.py::test_github_app_installation_token_ac4_private_key_not_exposed`
  — 직렬화한 전체 도구 결과(content + structured)에 PEM 아머(`-----BEGIN`/`-----END`),
  `PRIVATE KEY`/`RSA PRIVATE`, env 이름 `GITHUB_APP_PRIVATE_KEY`, 서명된 App JWT(`eyJ`)가
  하나도 없고 노출되는 것은 설치 토큰뿐임을 단언. 키 바이트는 CI 실행마다 생성되므로
  아머 마커로 판정한다.
