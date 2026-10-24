# 테스트 문서: session_create

## 검증 대상 AC
- AC1: 세션 생성과 활성 상태 반환 (PRD: session_create)
- AC2: 잘못된 요청은 아무것도 만들지 않는다 (PRD: session_create)
- AC3: 비파괴 추가 작업 표기 (PRD: session_create)
- AC4: 미설정 시 graceful 거부 (PRD: session_create)

> **자동화 상태**: 도구 계층은 구현·검증됐다 — 아래 "자동화" 필드의 Go 단위는 실재하는 테스트
> 이름이다. 통합 e2e의 시나리오별 현황은 **아래 각 "자동화" 필드가 그 자리에서 말한다**
> (`(미작성)` 표기는 `tests/integration/check_ac_mapping.py` 규칙 8이 실측 파일 집합과 대조해
> CI에서 강제한다). kind 픽스처는 `shell` 이미지만 배선하므로, 에이전트 타입 세션의 실제
> 생성은 e2e가 아니라 Go 단위(요청 본문)까지만 검증된다 — 그 대신 그 타입들이 **제공되지 않는
> 배포에서 거부된다**는 것은 시나리오 2가 실 제어면의 타입 목록으로 관측한다.

## 테스트 시나리오

### 시나리오 1: shell 세션 생성과 활성 반환
- **사전 조건**: session-platform 제어면 배포, 서버에 `SESSION_PLATFORM_ENDPOINT` 배선
- **실행 단계**: `session_create`를 `name`만으로 호출 → 반환된 세션의 `pod`를 네임스페이스에서
  조회 → `session_list` 호출 → 정리로 세션 삭제
- **기대 결과**: 반환된 세션은 `workloadType=shell`, `state=active`이고 `pod`를 가진다. 그 파드는
  반환 시점에 이미 존재하고, `session_list`가 같은 id를 열거한다.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestCreateSessionReturnsTheActiveSession` — POST 한 번과 그 본문,
  `TestCreateSessionOmitsUnsetFields` — 비운 필드는 보내지 않아 제어면 기본값이 적용된다,
  `TestCreateSessionOutlastsTheOrdinaryRequestTimeout`·
  `TestFromEnvCreateBudgetExceedsTheControlPlanesReadyWait` — 생성 대기는 일반 요청 상한이
  아니라 제어면의 파드 대기보다 긴 자기 상한을 쓴다) + `internal/server/mcp_test.go`
  (`TestSessionCreateReturnsTheActiveSession`) + 통합
  `tests/integration/session_create_sc1.py` — 파드가 실제로 뜨는지는 여기서만 관측된다

### 시나리오 2: 잘못된 요청의 무부작용 거부
- **사전 조건**: 시나리오 1과 같다
- **실행 단계**: 공백뿐인 `name`, 제어면이 제공하지 않는 `workloadType`(지어낸 `bash`와, 이
  하네스가 이미지를 배선하지 않은 실재 타입 `claude-code`), `shell` 세션에 `model` 지정을 각각
  호출하고, 매 호출 전후로 세션 목록과 파드 집합을 비교
- **기대 결과**: 첫째는 제어면에 닿기 전 인자 오류(`-32602`)로 거부된다. 제공되지 않는 타입은
  제어면의 타입 목록을 읽은 뒤 생성 요청 없이 `invalid argument` 도구 에러로 거부되고, 메시지가
  제공되는 타입(`shell`)을 나열한다. 마지막은 제어면의 400이 `invalid argument` 도구 에러로
  거부된다. 모든 호출에서 세션 목록과 파드 집합이 그대로다.
- **검증 AC**: AC2
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestCreateSessionRejectsBadRequestsBeforeTheWire` — 요청 0건,
  `TestCreateSessionMapsControlPlaneRefusals` — 400·413·500이 각자의 오류 종류로) +
  `internal/sessionplatform/workload_types_test.go`
  (`TestCreateSessionRefusesATypeTheControlPlaneDoesNotOffer` — 목록 조회 뒤 생성 요청 0건,
  `TestCreateSessionAcceptsAnOfferedType` — 이 서버가 모르는 타입도 제어면이 제공하면 생성,
  `TestCreateSessionLooksNothingUpForShell`,
  `TestCreateSessionDefersToAControlPlaneWithoutAList`,
  `TestCreateSessionReportsAnUnreadableConfigAsAnAPIError`) +
  `internal/server/mcp_test.go` (`TestSessionCreateRejectsBadArguments`,
  `TestSessionCreateSurfacesControlPlaneRefusalAsInvalidArgument`) + 통합
  `tests/integration/session_create_sc2.py`

### 시나리오 3: 어노테이션 광고
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 호출
- **기대 결과**: `session_create`가 readOnlyHint=false, destructiveHint=false,
  idempotentHint=false로 광고된다.
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/server/mcp_test.go`
  (`TestToolsListAdvertisesSessionCreate` — 어노테이션 넷과 `required: [name]`,
  `workloadType`에 고정 열거가 없음) + 통합 `tests/integration/session_create_sc3.py`

### 시나리오 4: 미설정 시 도구 에러
- **사전 조건**: `SESSION_PLATFORM_ENDPOINT` 미설정
- **실행 단계**: 유효한 인자로 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러. 직후 `ping`은 여전히 `pong`.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/server/mcp_test.go`
  (`TestSessionCreateUnavailableReturnsToolError`) +
  `internal/sessionplatform/sessionplatform_test.go`
  (`TestUnavailableRefusesCreateAndDelete`) + 통합
  `tests/integration/session_create_sc4.py`(`실행 대상: auth-variant`)
