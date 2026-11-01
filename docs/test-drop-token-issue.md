# 테스트 문서: drop_token_issue

## 검증 대상 AC
- AC1: 서비스 계정 교환으로 단기 토큰을 발급한다 (PRD: drop_token_issue)
- AC2: 결과는 셸에서 그대로 쓰는 curl 견본을 담는다 (PRD: drop_token_issue)
- AC3: 파일 내용은 도구를 지나지 않는다 (PRD: drop_token_issue)
- AC4: 입력 검증이 drop 보다 앞선다 (PRD: drop_token_issue)
- AC5: drop 의 거부 사유를 구분해 돌려준다 (PRD: drop_token_issue)
- AC6: 미설정 시 graceful 거부 (PRD: drop_token_issue)

> **자동화 상태**: 도구 계층은 구현·검증됐다 — 아래 "자동화" 필드의 Go 단위는 실재하는 테스트 이름이다.
> 통합 e2e 는 kind 에 띄운 **실물 drop**(`tests/k8s/kind/drop.yaml`, 모킹 없음)을 상대로 돈다. kind 에는
> Traefik·Sablier 가 없으므로 kind 오버레이가 `DROP_URL` 을 drop Service 로 바로 돌리고, 러너는 포트포워드로
> drop 에 닿아 도구가 돌려준 curl 견본을 `base_url` 만 바꿔 그대로 실행한다. 시나리오 4 의 401·403·502 는
> 그 상태를 만드는 변형 배포가 하네스에 없어 지금은 Go 단위만 잰다(`(미작성)`).

## 테스트 시나리오

### 시나리오 1: 발급한 토큰과 견본으로 올리고 내려받는다
- **사전 조건**: 실물 drop 의 `SA_ALLOW` 에 서버의 ServiceAccount 가 있고 drop 이 TokenReview 권한을 가진 상태
- **실행 단계**: `{valid_minutes: 5, label: "e2e 왕복"}` 로 호출 → `usage.upload` 로 작은 임의 바이트 파일을 올림 →
  응답의 아이디·비밀번호로 `usage.download_list`·`usage.download_file` 실행 → `usage.list` → `usage.delete` →
  `usage.list`
- **기대 결과**: `token` 이 `drk_` 로 시작하고 `expires_at` 이 지금 + 5분(±30초), `created_by` 가
  `sa:homelab-k3s-mcp/homelab-k3s-mcp`, `usage.setup` 이 `export DROP_TOKEN='<token>'` 이다. 내려받기는 표준
  출력이 비어 있고 `-o` 로 저장된 파일의 바이트가 올린 것과 같다. 목록에 보관함이 있다가 삭제 뒤에는 없다.
  (토큰 재읽기·307 재전송·`usage` 의 토큰 1회 등장·`days` 낮춤은 단위 계층에서 잰다)
- **검증 AC**: AC1, AC2
- **자동화**: Go 단위 `internal/drop/drop_test.go`
  (`TestIssueTokenExchangesTheServiceAccountToken`, `TestIssueTokenOmitsUnsetOptionalFields`,
  `TestIssueTokenReadsTheTokenOnEveryCall`, `TestIssueTokenFollowsSablierRedirectWithBody`) +
  `internal/server/drop_test.go` (`TestDropTokenIssueReturnsTokenAndCurlTemplates`,
  `TestDropTokenIssueDefaultsAndUnlimitedDaysKeepTheExampleAtSeven`) + 통합
  `tests/integration/drop_token_issue_sc1.py`

### 시나리오 2: 파일 내용은 도구를 지나지 않는다
- **사전 조건**: 시나리오 1 과 같음
- **실행 단계**: `tools/list` 로 입력 스키마·설명 확인 → `{file: "aGVsbG8="}`·`{content: "aGVsbG8="}` 로 호출 →
  토큰을 받아 고유 표지 문자열을 담은 파일을 견본으로 올림 → 다시 호출 → 올린 보관함 삭제
- **기대 결과**: 속성이 정확히 `valid_minutes`·`max_days`·`label` 이고 `additionalProperties: false`, 설명이
  「never sends or receives file contents」를 담는다. `file`·`content` 는 인자 오류(unknown argument)다. 두 번째
  결과의 키는 AC1 의 다섯 + `usage` 이고, `structuredContent`·`content` 어디에도 표지 문자열이 없으며 `usage` 의
  값은 모두 명령 문자열이다.
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/server/drop_test.go` (`TestToolsListAdvertisesDropTokenIssue`,
  `TestDropTokenIssueSendsOnlyTokenParametersToDrop`, `TestDropTokenIssueRefusesBadArgumentsBeforeCallingDrop`) +
  통합 `tests/integration/drop_token_issue_sc2.py`

### 시나리오 3: 범위 밖 인자는 drop 에 닿기 전에 거부된다
- **사전 조건**: 시나리오 1 과 같음
- **실행 단계**: `valid_minutes` 0·1441·`"60"`, `max_days` 0·31, 41자 `label` 로 각각 호출 → drop 의 발급 기록 수
  확인 → `{valid_minutes: 1440, max_days: 30, label: <40자>}` 로 호출 → 직후 `ping`
- **기대 결과**: 앞의 여섯은 각 사유를 담은 인자 오류이고 drop 의 발급 기록(`service account token issued`)이
  늘지 않는다. 경계값 호출은 성공하고 발급 기록이 정확히 하나 는다. `ping` 은 `pong`.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/server/drop_test.go` (`TestDropTokenIssueRefusesBadArgumentsBeforeCallingDrop`,
  `TestDropTokenIssueUnconfiguredRefusesGracefully`) + `internal/drop/drop_test.go`
  (`TestIssueTokenRefusesBadArgumentsWithoutARequest`) + 통합 `tests/integration/drop_token_issue_sc3.py`

### 시나리오 4: drop 의 거부 사유가 구분돼 돌아온다
- **사전 조건**: drop 이 400·401·403·502·503·500 으로 답하는 상대
- **실행 단계**: 각 응답마다 호출
- **기대 결과**: 도구 에러 문구가 응답마다 다르고 401 은 `audience "drop"`, 403 은 `SA_ALLOW`, 502 는
  `system:auth-delegator` 를 이름으로 댄다. 500 은 상태 코드와 본문 앞 300자를 담는다. 어느 것도 `drop unavailable`
  이 아니다. 201 인데 토큰이 없으면 에러다.
- **검증 AC**: AC5
- **자동화**: Go 단위 `internal/drop/drop_test.go` (`TestIssueTokenExplainsEachRefusal`,
  `TestIssueTokenRejectsA201WithoutAToken`) + `internal/server/drop_test.go`
  (`TestDropTokenIssueSurfacesDropRefusal`) + 통합 (미작성)

### 시나리오 5: 미설정 시 도구 에러
- **사전 조건**: `DROP_URL` 미설정
- **실행 단계**: `{}` 로 호출 → 직후 `ping`
- **기대 결과**: 서버 정상, 호출만 `drop unavailable: drop is not configured (DROP_URL)` 도구 에러. 직후 `ping` 은
  여전히 `pong`.
- **검증 AC**: AC6
- **자동화**: Go 단위 `internal/server/drop_test.go` (`TestDropTokenIssueUnconfiguredRefusesGracefully`) +
  `internal/drop/drop_test.go` (`TestUnavailableIsTheUnconfiguredKind`, `TestFromEnv`) + 통합
  `tests/integration/drop_token_issue_sc5.py`(`실행 대상: auth-variant`)
