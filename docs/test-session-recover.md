# 테스트 문서: session_recover

## 검증 대상 AC
- AC1: 삭제된 세션의 복원 (PRD: session_recover)
- AC2: 거부 응답의 구분 전달 (PRD: session_recover)
- AC3: 도구 표기 (PRD: session_recover)
- AC4: 미설정 시 graceful 거부 (PRD: session_recover)

> **자동화 상태**: 도구 계층은 구현·검증됐다 — 아래 "자동화" 필드의 Go 단위는 실재하는 테스트
> 이름이다. 시나리오 1·2의 통합 e2e는 kind 하네스의 제어면 이미지에 복원 엔드포인트
> (dlddu/session-platform#229)가 들어온 뒤에 쓴다 — 상태 추적의 「⏳ 구현 대기」 표가 해제 조건을
> 적는다.

## 테스트 시나리오

### 시나리오 1: 지운 세션이 같은 id로 돌아온다
- **사전 조건**: 아카이브가 켜진 제어면의 실 shell 세션 1개, 누적 출력에 마커 줄이 있고 한 번 동결됨
- **실행 단계**: `session_delete`로 삭제 → `session_recover`로 같은 id 복원 → `session_list` →
  offset 0 `session_read`
- **기대 결과**: 결과가 같은 id·이름의 `active` 세션이고 파드가 동결 전과 다르다. 목록에 다시
  나타나고, 읽기가 동결 전 마커 줄을 돌려준다.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestRecoverSessionIssuesOnePostAndReturnsTheSession` — 요청은 복원 경로로 가는 POST 한 건뿐이다) +
  `internal/server/mcp_test.go` (`TestSessionRecoverReturnsTheRecoveredSession` — 생성·목록·읽기·쓰기를
  하나도 부르지 않는다) + 통합 `tests/integration/session_recover_sc1.py` (미작성)

### 시나리오 2: 거부 사유가 구분된다
- **사전 조건**: 동결된 적 없는 id, 살아 있는 세션 id
- **실행 단계**: 각 id로 `session_recover` 호출 → 직후 `ping`
- **기대 결과**: 없는 id는 not found, 살아 있는 id는 still exists로 서로 다르게 읽히고 둘 다 재시도를
  권하지 않는다. 제어면 503은 unavailable로 읽힌다(Go 단위만 검증). 서버는 계속 동작한다.
- **검증 AC**: AC2
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestRecoverSessionRefusalsAreDistinct` — 404·409·503·403이 서로 다른 메시지이고 어느 것도 재시도를
  권하지 않는다, `TestRecoverSessionRejectsEmptyIDBeforeTheWire`) + `internal/server/mcp_test.go`
  (`TestSessionRecoverSurfacesRefusalsDistinctly`, `TestSessionRecoverRejectsBadArguments`) + 통합
  `tests/integration/session_recover_sc2.py` (미작성)

### 시나리오 3: 도구 표기
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 호출
- **기대 결과**: `session_recover` 어노테이션이 readOnlyHint=false, destructiveHint=false,
  idempotentHint=false, openWorldHint=true이고 `required: [id]`다.
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/server/mcp_test.go` (`TestToolsListAdvertisesSessionRecover`) + 통합
  `tests/integration/session_recover_sc3.py`

### 시나리오 4: 미설정 시 도구 에러
- **사전 조건**: `SESSION_PLATFORM_ENDPOINT` 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러. 직후 `ping`은 여전히 `pong`.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/server/mcp_test.go`
  (`TestSessionRecoverUnavailableReturnsToolError`) +
  `internal/sessionplatform/sessionplatform_test.go` (`TestUnavailableRefusesCreateAndDelete`) + 통합
  `tests/integration/session_recover_sc4.py`(`실행 대상: auth-variant`)
