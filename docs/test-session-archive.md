# 테스트 문서: session_archive

## 검증 대상 AC
- AC1: 세션의 즉시 동결 (PRD: session_archive)
- AC2: 긴 동결의 pending 응답 (PRD: session_archive)
- AC3: 거부 응답의 구분 전달 (PRD: session_archive)
- AC4: 도구 표기 (PRD: session_archive)
- AC5: 미설정 시 graceful 거부 (PRD: session_archive)

> **자동화 상태**: 도구 계층은 구현·검증됐다 — 아래 "자동화" 필드의 Go 단위는 실재하는 테스트
> 이름이다. 시나리오 1·2의 통합 e2e는 kind 하네스의 제어면이 아카이브를 켠 채 뜬 뒤에 쓴다 — 상태
> 추적의 「⏳ 구현 대기」 표가 해제 조건을 적는다.

## 테스트 시나리오

### 시나리오 1: 살아 있는 세션이 지금 동결된다
- **사전 조건**: 아카이브가 켜진 제어면의 실 shell 세션 1개, 누적 출력에 마커 줄이 있음
- **실행 단계**: `session_archive` 호출 → `session_list` → 같은 id로 `session_archive` 재호출 →
  `session_restore` → offset 0 `session_read`
- **기대 결과**: 결과가 같은 id의 `snapshot` 세션이고 `pod`가 없으며 그 파드가 사라진다. 목록에서
  `snapshot`으로 보이고, 재호출도 오류 없이 같은 세션을 돌려준다. 복원 뒤 읽기가 마커 줄을
  돌려준다.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestArchiveSessionIssuesOnePostAndReturnsTheFrozenSession` — 요청은 동결 경로로 가는 POST 한
  건뿐이다, `TestArchiveSessionOnAnAlreadyFrozenSessionIsIdempotent` — 이미 동결된 세션에 다시
  불러도 같은 세션이 오류 없이 돌아오고 요청은 같은 경로의 POST 한 건 더다) +
  `internal/server/session_archive_test.go` (`TestSessionArchiveReturnsTheFrozenSession` —
  생성·삭제·목록·읽기·쓰기를 하나도 부르지 않는다,
  `TestSessionArchiveRepeatedOnAnArchivedSessionReturnsItUnchanged` — 재호출이 pending 없이 같은
  `snapshot` 세션을 돌려주고 복원·읽기·쓰기를 부르지 않는다) + 통합
  `tests/integration/session_archive_sc1.py` (미작성)

### 시나리오 2: 오래 걸리는 동결은 pending으로 답한다
- **사전 조건**: 대기 상한보다 오래 걸리는 동결(진행 중인 턴이 있는 claude-code 세션)
- **실행 단계**: `session_archive` 호출 → `session_list`를 `snapshot`이 될 때까지 폴링
- **기대 결과**: 도구가 대기 상한 안에 `isError=false`·`pending: true`·`state: "archiving"`과
  `session_list`를 가리키는 `message`로 답하고, 그 뒤 세션이 `snapshot`이 된다.
- **검증 AC**: AC2
- **자동화**: Go 단위 `internal/server/session_archive_test.go`
  (`TestSessionArchiveAnswersPendingWhenTheFreezeIsSlow` — 응답 뒤에도 제어면 호출이 끝까지 간다) +
  통합 `tests/integration/session_archive_sc2.py` (미작성)

### 시나리오 3: 거부 사유가 구분된다
- **사전 조건**: 없는 id, 아카이브가 꺼진 제어면의 살아 있는 shell 세션
- **실행 단계**: 각 id로 `session_archive` 호출 → 직후 `ping`
- **기대 결과**: 없는 id는 not found로 재시도를 권하지 않고, 아카이브가 꺼진 세션은 unavailable로
  읽히며 세션은 `active` 그대로다. 제어면 409는 conflict로 재시도를 권한다(Go 단위만 검증). 서버는
  계속 동작한다.
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestArchiveSessionRefusalsAreDistinct` — 404·409·503·500이 서로 다른 메시지이고 409만 재시도를
  권한다, `TestArchiveSessionRejectsEmptyIDBeforeTheWire`) + `internal/server/session_archive_test.go`
  (`TestSessionArchiveSurfacesRefusalsDistinctly`, `TestSessionArchiveRejectsBadArguments`) + 통합
  `tests/integration/session_archive_sc3.py`

### 시나리오 4: 도구 표기
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 호출
- **기대 결과**: `session_archive` 어노테이션이 readOnlyHint=false, destructiveHint=false,
  idempotentHint=true, openWorldHint=true이고 `required: [id]`다.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/server/session_archive_test.go` (`TestToolsListAdvertisesSessionArchive`) +
  통합 `tests/integration/session_archive_sc4.py`

### 시나리오 5: 미설정 시 도구 에러
- **사전 조건**: `SESSION_PLATFORM_ENDPOINT` 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러. 직후 `ping`은 여전히 `pong`.
- **검증 AC**: AC5
- **자동화**: Go 단위 `internal/server/session_archive_test.go`
  (`TestSessionArchiveUnavailableReturnsToolError`) +
  `internal/sessionplatform/sessionplatform_test.go` (`TestUnavailableRefusesCreateAndDelete`) + 통합
  `tests/integration/session_archive_sc5.py`(`실행 대상: auth-variant`)
