# 테스트 문서: session_restore

## 검증 대상 AC
- AC1: 동결된 세션의 복원 (PRD: session_restore)
- AC2: 긴 복원의 pending 응답 (PRD: session_restore)
- AC3: 거부 응답의 구분 전달 (PRD: session_restore)
- AC4: 도구 표기 (PRD: session_restore)
- AC5: 미설정 시 graceful 거부 (PRD: session_restore)

> **자동화 상태**: 도구 계층은 구현·검증됐다 — 아래 "자동화" 필드의 Go 단위는 실재하는 테스트
> 이름이다. 시나리오 1·2의 통합 e2e는 kind 하네스의 제어면이 아카이브를 켠 채 뜬 뒤에 쓴다 — 동결된
> 세션이 없으면 복원을 관측할 수 없다. 상태 추적의 「⏳ 구현 대기」 표가 해제 조건을 적는다.

## 테스트 시나리오

### 시나리오 1: 동결된 세션이 같은 id로 돌아온다
- **사전 조건**: 아카이브가 켜진 제어면의 실 shell 세션 1개, 누적 출력에 마커 줄이 있고 동결됨
- **실행 단계**: `session_restore` 호출 → `session_list` → 같은 id로 `session_restore` 재호출 →
  offset 0 `session_read`
- **기대 결과**: 결과가 같은 id의 `active` 세션이고 새 `pod`가 있다. 목록에서 `active`로 보이고,
  재호출도 오류 없이 같은 세션을 돌려준다. 읽기가 마커 줄을 돌려준다.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestRestoreSessionIssuesOneSwitchAndReturnsTheActiveSession` — 요청은 switch 경로로 가는 POST 한
  건뿐이다) + `internal/server/session_restore_test.go` (`TestSessionRestoreReturnsTheActiveSession` —
  읽기·쓰기·동결을 하나도 부르지 않는다) + 통합 `tests/integration/session_restore_sc1.py` (미작성)

### 시나리오 2: 오래 걸리는 복원은 pending으로 답한다
- **사전 조건**: 대기 상한보다 오래 걸리는 복원(이미지가 없는 노드에 서는 복원 파드, 또는 큰 아카이브)
- **실행 단계**: `session_restore` 호출 → `session_list`를 `active`가 될 때까지 폴링
- **기대 결과**: 도구가 대기 상한 안에 `isError=false`·`pending: true`·`state: "restoring"`과
  `session_list`를 가리키는 `message`로 답하고, 그 뒤 세션이 `active`가 된다.
- **검증 AC**: AC2
- **자동화**: Go 단위 `internal/server/session_restore_test.go`
  (`TestSessionRestoreAnswersPendingWhenTheRestoreIsSlow` — 응답 뒤에도 제어면 호출이 끝까지 간다) +
  통합 `tests/integration/session_restore_sc2.py` (미작성)

### 시나리오 3: 거부 사유가 구분된다
- **사전 조건**: 없는 id, 살아 있는 shell 세션
- **실행 단계**: 각 id로 `session_restore` 호출 → `session_list`
- **기대 결과**: 없는 id는 not found로 재시도를 권하지 않는다. 살아 있는 세션은 같은 파드의 `active`
  세션으로 그대로 돌아온다. 제어면 409는 conflict로 재시도를 권하고, 503은 unavailable로 읽힌다(Go
  단위만 검증).
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestRestoreSessionRefusalsAreDistinct`) + `internal/server/session_restore_test.go`
  (`TestSessionRestoreSurfacesRefusalsDistinctly`, `TestSessionRestoreRejectsBadArguments`) + 통합
  `tests/integration/session_restore_sc3.py`

### 시나리오 4: 도구 표기
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 호출
- **기대 결과**: `session_restore` 어노테이션이 readOnlyHint=false, destructiveHint=false,
  idempotentHint=true, openWorldHint=true이고 `required: [id]`다.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/server/session_restore_test.go` (`TestToolsListAdvertisesSessionRestore`) +
  통합 `tests/integration/session_restore_sc4.py`

### 시나리오 5: 미설정 시 도구 에러
- **사전 조건**: `SESSION_PLATFORM_ENDPOINT` 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러. 직후 `ping`은 여전히 `pong`.
- **검증 AC**: AC5
- **자동화**: Go 단위 `internal/server/session_restore_test.go`
  (`TestSessionRestoreUnavailableReturnsToolError`) +
  `internal/sessionplatform/sessionplatform_test.go` (`TestUnavailableRefusesCreateAndDelete`) + 통합
  `tests/integration/session_restore_sc5.py`(`실행 대상: auth-variant`)
