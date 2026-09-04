# 테스트 문서: session_read

## 검증 대상 AC
- AC1: 오프셋 커서 읽기 (PRD: session_read)
- AC2: 상태 분기 노출 (PRD: session_read)
- AC3: 대상 부재·잘못된 커서의 명확한 처리 (PRD: session_read)
- AC4: 미설정 시 graceful 거부 (PRD: session_read)

> **자동화 상태**: 도구는 구현됐고 **Go 단위는 작성됨**. 통합 e2e의 시나리오별 현황은 **아래 각
> "자동화" 필드가 그 자리에서 말한다**(`(미작성)`이면 아직 파일이 없다는 뜻이고, 그 표기는
> `tests/integration/check_ac_mapping.py` 규칙 7이 실측 파일 집합과 대조해 CI에서 강제한다).
> 잔여 공백과 그 **선행 조건**은 `docs/doc-tracker.md`의 AC ↔ e2e 1:1 렌즈(공백 backlog)가 단일
> 사실 원천이며, 그 축의 소유자는 그 렌즈다 — 여기에 개수나 시나리오 번호를 다시 적지 않는다.
> 남은 공백의 선행이 **저작이 아니라 픽스처**라는 것(실 데이터 플레인 파드, 그리고 `snapshot`
> 분기에는 CRIU 게이트까지)도 그 backlog가 소스 근거와 함께 적고 있다.

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
  (`TestSessionReadRejectsBadArguments`, `TestSessionReadSurfacesNotFound`) + 통합
  `tests/integration/session_read_ac3.py`.
  "상태가 변하지 않는다"는 **요청이 0건임**으로 증명한다 — 음수 커서·빈 id는 HTTP에 닿기 전에
  거부되므로 대상 세션을 건드릴 수 없다. 통합 쪽은 그 구조를 밖에서 되받는다: 두 실패의 층이
  서로 다르고(not found는 도구 에러, 잘못된 커서는 `-32602` 프로토콜 에러) 두 호출 뒤 실재
  세션의 상태·`lastAccess`와 제어면 네임스페이스의 파드 집합이 모두 불변이다.

### 시나리오 4: 미설정 시 도구 에러
- **사전 조건**: `SESSION_PLATFORM_ENDPOINT` 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러. 직후 `ping`은 여전히 `pong`.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestUnavailableRefusesRead`) + `internal/server/mcp_test.go`
  (`TestSessionReadUnavailableReturnsToolError` — 거부가 이 도구에 갇히고 `ping`은 계속 `pong`)
  + 통합
  `tests/integration/session_read_ac4.py::test_session_read_ac4_unconfigured_refusal`
  (`실행 대상: auth-variant` — 자격증명을 하나도 붙이지 않은 변형이라 AC의 전제가 여기서만 참이다)
