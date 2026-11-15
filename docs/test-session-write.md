# 테스트 문서: session_write

## 검증 대상 AC
- AC1: 워크로드 입력 주입 (PRD: session_write)
- AC2: 상태 분기 처리와 노출 (PRD: session_write)
- AC3: 파괴적 작업 표기 (PRD: session_write)
- AC4: 거부 응답의 구분 전달 (PRD: session_write)
- AC5: 미설정 시 graceful 거부 (PRD: session_write)
- AC6: claude-code 턴의 권한 모드를 그대로 전달한다 (PRD: session_write)

> **자동화 상태**: 도구 계층은 구현·검증됐다 — 아래 "자동화" 필드의 Go 단위는 실재하는 테스트
> 이름이다. 통합 e2e의 시나리오별 현황은 **아래 각 "자동화" 필드가 그 자리에서 말한다**
> (`(미작성)`이면 아직 파일이 없다는 뜻이고, 그 표기는 `tests/integration/check_ac_mapping.py`
> 규칙 7이 실측 파일 집합과 대조해 CI에서 강제한다). 잔여 공백과 그 **선행 조건**은
> `docs/doc-tracker/`의 AC ↔ e2e 1:1 렌즈(공백 backlog)가 단일 사실 원천이며, 그 축의 소유자는
> 그 렌즈다 — 여기에 개수나 시나리오 번호를 다시 적지 않는다. 도구 계층에서 재현할 수 없는 부분 —
> 실제 워크로드가 명령·프롬프트를 실행하는 것, 유휴 승격·스냅샷 복원이 진짜 파드를 만드는 것 —
> 이 그 렌즈에 남는다.

## 테스트 시나리오

### 시나리오 1: shell·claude-code 입력 주입
- **사전 조건**: `active` shell 세션 1개, `active` claude-code 세션 1개
- **실행 단계**: shell 세션에 명령을 write → `session_read`로 출력 확인. claude-code
  세션에 프롬프트를 write → 반환 시각 측정 후 read로 응답 누적 확인
- **기대 결과**: shell은 명령 출력이 누적 출력에 나타난다. claude-code write는 실행 완료를
  기다리지 않고 즉시 반환하며, 이후 read에서 응답 텍스트가 누적된다.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestWriteThenReadRecoversOutput` — 페이로드가 바이트 그대로 전달되고, 그 산출물은 write
  응답이 아니라 **뒤이은 read**로만 관측된다. 출력을 담을 필드가 `WriteResult`에 아예 없는
  것이 "완료를 기다리지 않는다"의 구조적 표현이다) + 통합
  `tests/integration/session_write_sc1.py` — 실 워크로드가 명령을 실제로 실행하는지는 여기서만
  관측된다

### 시나리오 2: 상태 분기와 그 노출
- **사전 조건**: `active`·`idle`·`snapshot` 세션 각 1개
- **실행 단계**: 세 세션에 각각 write 호출 후 read로 반영 확인
- **기대 결과**: `active`·`idle` 세션은 적용되고 결과의 `path`가 실제 분기와 일치하며 호출 후 상태는
  `active`로 보고된다. `snapshot` 세션은 `session_restore`를 먼저 부르라는 도구 에러이고, 제어면 write가
  나가지 않으며 세션은 `snapshot` 그대로다(*2026-11-13 개정 — 전에는 복원 후 적용했다*).
- **검증 AC**: AC2
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestWriteDisclosesStateBranch` — 제어면이 알려 준 분기가 그대로 전달되고 처리 후 상태가 `active`) +
  `internal/server/mcp_test.go` (`TestSessionWriteReturnsBranchAndSession`) +
  `internal/server/session_restore_test.go` (`TestSessionReadAndWriteRefuseAnArchivedSession` — 동결된
  세션은 write 없이 거부) + 통합 (미작성) `tests/integration/session_write_sc2.py` — 실 `idle`·`snapshot`
  세션의 분기는 여기서만 관측된다

### 시나리오 3: destructiveHint 광고
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 호출
- **기대 결과**: session_write 어노테이션이 destructiveHint=true, readOnlyHint=false.
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/server/mcp_test.go`
  (`TestToolsListAdvertisesSessionWrite` — 네 어노테이션 전부와 `required: [id, payload]`) +
  통합 `tests/integration/session_write_sc3.py`
  (`tools/list` 메타데이터만 읽고 파괴 동작은 실행하지 않는다)

### 시나리오 4: 거부 사유 구분
- **사전 조건**: 제어면 스텁이 not found / 페이로드 상한 초과 / 큐 포화 / 출력 쿼터 소진을
  각각 재현
- **실행 단계**: 네 상황에서 각각 write 호출 → 큐 포화·쿼터 소진 케이스는 직후
  `session_read` 호출
- **기대 결과**: 네 거부가 서로 다른 사유로 구별되어 도구 에러로 전달되고, 재시도가 의미
  있는 거부(큐 포화)와 그렇지 않은 거부(쿼터 소진·상한 초과)가 메시지에서 구분된다. 거부
  이후에도 기존 세션 출력은 read로 계속 조회된다.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestWriteMapsControlPlaneRefusals` — 404/413/429/507이 각자의 오류 종류로 매핑되고,
  큐 포화만 "retry after"로, 상한 초과·쿼터 소진은 "retrying will not help"로 읽히며, 거부
  직후에도 기존 출력이 read로 조회된다. `TestWriteRefusalsAreDistinctWithoutTheControlPlanesProse`
  — 네 상태코드에 **같은 오류 본문**을 물려 놓고도 네 메시지가 쌍쌍이 달라야 한다: 제어면이
  문구를 달리 써 줘서 구별되는 것이 아님을 못박는다) + `internal/server/mcp_test.go`
  (`TestSessionWriteSurfacesRefusalsDistinctly`) + 통합 (미작성)
  `tests/integration/session_write_sc4.py`

### 시나리오 5: 미설정 시 도구 에러
- **사전 조건**: `SESSION_PLATFORM_ENDPOINT` 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러. 직후 `ping`은 여전히 `pong`.
- **검증 AC**: AC5
- **자동화**: Go 단위 `internal/server/mcp_test.go`
  (`TestSessionWriteUnavailableReturnsToolError` — 거부가 이 도구에 갇히고 직후 `ping`은
  여전히 `pong`) + `internal/sessionplatform/sessionplatform_test.go`
  (`TestUnavailableRefusesWrite`) + 통합
  `tests/integration/session_write_sc5.py`(`실행 대상: auth-variant`)

### 시나리오 6: 권한 모드가 제어면에 그대로 실린다
- **사전 조건**: 서버에 `SESSION_PLATFORM_ENDPOINT` 배선, write 의 `permissionMode`(session-platform AC-E2)를
  받는 제어면과 `claude-code` 세션 1개
- **실행 단계**: (a) `session_write`에 `permissionMode="acceptEdits"` → (b) 같은 세션에 `permissionMode` 생략 →
  (c) `permissionMode`에 `""`·`1`을 각각 준 호출 → (d) `tools/list`
- **기대 결과**: (a) 제어면이 받은 본문이 `{"payload": …, "permissionMode": "acceptEdits"}`다. (b) 본문에
  `permissionMode`가 없다. (c) 제어면에 요청이 가지 않고 `-32602`. (d) `permissionMode` 스키마가
  `enum: [auto, acceptEdits]`
- **검증 AC**: AC6
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`(`TestWriteSendsPermissionMode`) +
  `internal/server/mcp_test.go`(`TestSessionWritePassesPermissionModeThrough`, `TestSessionWriteRejectsBadArguments`,
  `TestToolsListAdvertisesSessionWrite`) + 통합 (미작성)
