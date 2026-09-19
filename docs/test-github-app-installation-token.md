# 테스트 문서: github_app_installation_token

## 검증 대상 AC
- AC1: 단명 설치 토큰 발급 (PRD: github_app_installation_token)
- AC2: 스코프 제한 (PRD: github_app_installation_token)
- AC3: 미설정 시 graceful 거부 (PRD: github_app_installation_token)
- AC4: 베이스 키 비노출 (PRD: github_app_installation_token)
- AC5: commit status 쓰기 권한 배제 (읽기는 발급) (PRD: github_app_installation_token)

## 테스트 시나리오

### 시나리오 1: 기본 토큰 발급(.env + 만료·스코프 주석)
- **사전 조건**: github-mock 구성(설치 ID 67890)
- **실행 단계**: 인자 없이 호출
- **기대 결과**: text/plain 리소스에 `GITHUB_TOKEN=...`, `# Expires at:` 주석,
  `# Repository selection: all`, `contents=` 포함
- **검증 AC**: AC1
- **자동화**: 통합 `github_app_installation_token_ac1.py::test_github_app_installation_token_ac1_short_lived_token`
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
- **자동화**: 통합 `github_app_installation_token_ac2.py::test_github_app_installation_token_ac2_scope_restriction`
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
  `tests/integration/github_app_installation_token_ac3.py::test_github_app_installation_token_ac3_unconfigured_refusal`
  (자격증명 미부착 배포 변형에서 unavailable 도구 에러 반환 + 직후 ping 정상).

### 시나리오 4: 개인키 비노출
- **사전 조건**: 동일(구성됨)
- **실행 단계**: 발급 결과 검사
- **기대 결과**: 출력은 설치 토큰·만료·스코프 주석뿐이며 App 개인키 미포함
- **검증 AC**: AC4
- **자동화**: 통합 `github_app_installation_token_ac4.py::test_github_app_installation_token_ac4_private_key_not_exposed`
  — 직렬화한 전체 도구 결과(content + structured)에 PEM 아머(`-----BEGIN`/`-----END`),
  `PRIVATE KEY`/`RSA PRIVATE`, env 이름 `GITHUB_APP_PRIVATE_KEY`, 서명된 App JWT(`eyJ`)가
  하나도 없고 노출되는 것은 설치 토큰뿐임을 단언. 키 바이트는 CI 실행마다 생성되므로
  아머 마커로 판정한다.

### 시나리오 5: statuses 쓰기는 발급되지 않고 읽기는 발급된다
- **사전 조건**: github-mock이 (a) 설치 권한 조회 `GET /app/installations/67890`에 `statuses:
  write`를 포함한 권한 목록을 돌려주고, (b) 받은 요청을 돌려주는 기록 엔드포인트를 가진다.
  (c) 토큰 발급 응답에 `statuses: write`를 섞어 돌려주는 모드를 켤 수 있다
- **실행 단계**: ① `permissions={statuses: write}`, `{contents: read, statuses: write}`로 각각
  호출. ② `permissions={statuses: read}`로 호출. ③ 인자 없이 호출. ④ 설치 권한 조회가 500을
  돌려주게 한 뒤 인자 없이 호출. ⑤ (c) 모드에서 `permissions={contents: read}`로 호출
- **기대 결과**: ① 둘 다 `github_commit_status_create`를 안내하는 도구 에러, 발급 요청 0.
  ② 발급되고 응답 `# Permissions:` 주석이 `statuses=read`. ③ 발급 요청 본문에 `permissions`가
  명시돼 있고 `statuses`가 `read`이며, 응답 주석도 `statuses=read`. ④ 도구 에러, 발급 요청 0.
  ⑤ `DELETE /installation/token` 요청 1회, 도구 에러, 직렬화한 결과에 발급된 토큰 문자열 없음
- **검증 AC**: AC5
- **자동화**: Go 단위 `github_test.go::TestGitHubTokenRejectsStatusesWrite`(①),
  `TestGitHubTokenAllowsStatusesRead`(②), `TestGitHubTokenDefaultDowngradesStatusesToRead`(③),
  `TestGitHubTokenRefusesWhenInstallationUnreadable`(④),
  `TestGitHubTokenRevokesTokenCarryingStatusesWrite`(⑤) — 다섯 모두 `httptest` 상류를 세워
  **상류가 받은 요청(메서드·경로·본문)** 을 단언한다. AC5는 대부분 「일어나면 안 되는 요청」에
  대한 주장이라 반환 토큰이 아니라 요청 로그가 판정 근거다.
  통합 e2e 는 `tests/integration/github_app_installation_token_ac5.py` 가 다섯 단계를 그대로
  단언한다 — 선행이던 github-mock 확장(요청 기록 · `statuses: write` 를 섞어 돌려주는 모드 ·
  설치 권한 조회 500 모드 · `DELETE /installation/token`)이 그 파일과 같은 PR 로 착지했다.
  Go 단위가 `httptest` 상류에서 보는 것을 e2e 는 배포된 서버와 github-mock 의 요청 기록에서
  본다
