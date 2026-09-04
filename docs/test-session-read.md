# 테스트 문서: session_read

## 검증 대상 AC
- AC1: 오프셋 커서 읽기 (PRD: session_read)
- AC2: 상태 분기 노출 (PRD: session_read)
- AC3: 대상 부재·잘못된 커서의 명확한 처리 (PRD: session_read)
- AC4: 미설정 시 graceful 거부 (PRD: session_read)

> **자동화 상태(2026-09-04)**: 도구는 구현됐고 **Go 단위는 작성됨**. 통합(`tests/integration/`)은
> 아직 미작성이라 아래 "자동화" 필드에서 `(미작성)`으로 남는다 — 그 축의 소유자는 AC ↔ e2e 1:1
> 렌즈(`doc-tracker.md`)다.

## 테스트 시나리오

### 시나리오 1: 전체 읽기 → 증분 읽기 → 재읽기
- **사전 조건**: 출력이 누적된 `active` shell 세션
- **실행 단계**: `offset` 미지정으로 호출 → 반환된 `nextOffset`으로 재호출 → 세션에 새
  출력을 발생시킨 뒤 같은 커서로 호출 → 최초 커서(0)로 다시 호출
- **기대 결과**: 첫 호출은 전체 출력, 두 번째는 빈 payload와 동일 커서, 세 번째는 증분만,
  네 번째는 첫 호출과 같은 구간을 반환(비파괴).
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestReadFullThenIncremental`, `TestReadIsNonDestructive`) + (미작성) 통합
  `tests/integration/session.py::test_session_read_ac1_offset_cursor`

### 시나리오 2: 상태 분기와 그 노출
- **사전 조건**: `active`·`idle`·`snapshot` 세션 각 1개
- **실행 단계**: 세 세션에 각각 read 호출
- **기대 결과**: 세 호출 모두 출력을 반환하고, 결과의 `path`가 각각 직접 읽기 / 유휴 승격 /
  스냅샷 복원으로 실제 분기와 일치하며, 호출 후 상태는 모두 `active`로 보고된다.
  스냅샷 케이스에서는 파드가 실제로 다시 기동된다.
- **검증 AC**: AC2
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestReadDisclosesStateBranch` — 세 분기의 `path`와 처리 후 상태) + `internal/server/mcp_test.go`
  (`TestSessionReadReturnsPayloadCursorAndBranch` — 도구 결과에 `path`·처리 후 세션이 실린다)
  + (미작성) 통합 `tests/integration/session.py::test_session_read_ac2_state_branch_disclosure`.
  통합 쪽이 남는 이유: 스냅샷 세션의 파드가 **실제로 다시 기동되는지**는 제어면 스텁이나 kind에
  띄운 제어면이 있어야 관측된다. Go 단위는 "제어면이 알려 준 분기를 도구가 감추지 않는다"까지만
  증명한다.

### 시나리오 3: 없는 세션·잘못된 커서
- **사전 조건**: 존재하는 세션 1개(상태·`lastAccess` 기준값 확보)
- **실행 단계**: 존재하지 않는 id로 호출 → 존재하는 id에 `offset=-1`로 호출
- **기대 결과**: 각각 not found 계열 / 인자 검증 계열로 **구분되는** 도구 에러가 반환되고,
  기존 세션의 상태·`lastAccess`는 변하지 않는다.
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestReadRejectsNegativeOffset`, `TestReadNotFound`) + `internal/server/mcp_test.go`
  (`TestSessionReadRejectsBadArguments`, `TestSessionReadSurfacesNotFound`) + (미작성) 통합
  `tests/integration/session.py::test_session_read_ac3_invalid_target_and_cursor`.
  "상태가 변하지 않는다"는 **요청이 0건임**으로 증명한다 — 음수 커서·빈 id는 HTTP에 닿기 전에
  거부되므로 대상 세션을 건드릴 수 없다.

### 시나리오 4: 미설정 시 도구 에러
- **사전 조건**: `SESSION_PLATFORM_ENDPOINT` 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러. 직후 `ping`은 여전히 `pong`.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestUnavailableRefusesRead`) + `internal/server/mcp_test.go`
  (`TestSessionReadUnavailableReturnsToolError` — 거부가 이 도구에 갇히고 `ping`은 계속 `pong`)
  + (미작성) 통합
  `tests/integration/session_read_ac4.py::test_session_read_ac4_unconfigured_refusal`
