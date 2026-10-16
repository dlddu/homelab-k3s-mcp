# 테스트 문서: session_delete

## 검증 대상 AC
- AC1: 세션 삭제와 파드 회수 (PRD: session_delete)
- AC2: 거부 응답의 구분 전달 (PRD: session_delete)
- AC3: 파괴적 작업 표기 (PRD: session_delete)
- AC4: 미설정 시 graceful 거부 (PRD: session_delete)

> **자동화 상태**: 도구 계층은 구현·검증됐다 — 아래 "자동화" 필드의 Go 단위는 실재하는 테스트
> 이름이다. 통합 e2e의 시나리오별 현황은 **아래 각 "자동화" 필드가 그 자리에서 말한다**.
> 제어면의 409(진행 중인 생명주기 작업과의 충돌)는 kind에서 결정적으로 만들 수 없어 Go 단위가
> 유일한 검증이다.

## 테스트 시나리오

### 시나리오 1: 삭제한 세션만 사라지고 그 파드가 회수된다
- **사전 조건**: 실 shell 세션 2개(제어면으로 직접 생성)
- **실행 단계**: 한 세션을 `session_delete`로 삭제 → `session_list` 호출 → 두 파드의 존재를 회수
  예산 동안 관찰
- **기대 결과**: 결과가 `{id, deleted: true}`다. 삭제한 세션은 목록에서 빠지고 그 파드는 회수
  예산 안에 사라진다. 다른 세션은 목록에 남고 그 파드도 그대로다.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestDeleteSessionIssuesOneDelete` — 요청은 세션 경로로 가는 DELETE 한 건뿐이다) +
  `internal/server/mcp_test.go` (`TestSessionDeleteReportsTheDeletedSession` — 목록·읽기·쓰기를
  하나도 부르지 않는다) + 통합 `tests/integration/session_delete_sc1.py`

### 시나리오 2: 없는 세션 삭제는 not found로 구분된다
- **사전 조건**: 제어면에 없는 세션 id
- **실행 단계**: 그 id로 `session_delete` 호출 → 직후 `ping`
- **기대 결과**: not found 도구 에러가 반환되고, 메시지가 재시도를 권하지 않으며, 제어면
  미설정 거부와도 다르게 읽힌다. 서버는 계속 동작한다. (409 conflict는 "retry after"로 읽혀
  not found와 구별된다 — Go 단위만 검증)
- **검증 AC**: AC2
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestDeleteSessionRefusalsAreDistinct` — 404·409·500이 같은 본문으로도 서로 다른 메시지이고
  409만 재시도 가능, `TestDeleteSessionRejectsEmptyIDBeforeTheWire`) +
  `internal/server/mcp_test.go` (`TestSessionDeleteSurfacesRefusalsDistinctly`,
  `TestSessionDeleteRejectsBadArguments`) + 통합 `tests/integration/session_delete_sc2.py`

### 시나리오 3: destructiveHint 광고
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 호출
- **기대 결과**: `session_delete` 어노테이션이 destructiveHint=true, readOnlyHint=false.
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/server/mcp_test.go`
  (`TestToolsListAdvertisesSessionDelete` — 어노테이션 넷과 `required: [id]`) + 통합
  `tests/integration/session_delete_sc3.py` (`tools/list` 메타데이터만 읽고 삭제는 실행하지
  않는다)

### 시나리오 4: 미설정 시 도구 에러
- **사전 조건**: `SESSION_PLATFORM_ENDPOINT` 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러. 직후 `ping`은 여전히 `pong`.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/server/mcp_test.go`
  (`TestSessionDeleteUnavailableReturnsToolError`) +
  `internal/sessionplatform/sessionplatform_test.go`
  (`TestUnavailableRefusesCreateAndDelete`) + 통합
  `tests/integration/session_delete_sc4.py`(`실행 대상: auth-variant`)
