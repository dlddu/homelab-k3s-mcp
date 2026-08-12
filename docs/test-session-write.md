# 테스트 문서: session_write

## 검증 대상 AC
- AC1: 워크로드 입력 주입 (PRD: session_write)
- AC2: 상태 분기 처리와 노출 (PRD: session_write)
- AC3: 파괴적 작업 표기 (PRD: session_write)
- AC4: 거부 응답의 구분 전달 (PRD: session_write)
- AC5: 미설정 시 graceful 거부 (PRD: session_write)

> **자동화 상태**: 도구 미구현(구현 선행 문서). 아래 "자동화" 필드는 구현 시 작성할 대상
> 경로를 지목한다.

## 테스트 시나리오

### 시나리오 1: shell·claude-code 입력 주입
- **사전 조건**: `active` shell 세션 1개, `active` claude-code 세션 1개
- **실행 단계**: shell 세션에 명령을 write → `session_read`로 출력 확인. claude-code
  세션에 프롬프트를 write → 반환 시각 측정 후 read로 응답 누적 확인
- **기대 결과**: shell은 명령 출력이 누적 출력에 나타난다. claude-code write는 실행 완료를
  기다리지 않고 즉시 반환하며, 이후 read에서 응답 텍스트가 누적된다.
- **검증 AC**: AC1
- **자동화**: (미작성) Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestWriteSendsPayload`, `TestWriteReturnsWithoutWaiting`) + 통합
  `tests/integration/session.py::test_session_write_ac1_payload_injection`

### 시나리오 2: 상태 분기와 그 노출
- **사전 조건**: `active`·`idle`·`snapshot` 세션 각 1개
- **실행 단계**: 세 세션에 각각 write 호출 후 read로 반영 확인
- **기대 결과**: 스냅샷 세션도 거부되지 않고 복원 후 적용된다. 결과의 `path`가 실제 분기와
  일치하고 호출 후 상태는 모두 `active`로 보고된다.
- **검증 AC**: AC2
- **자동화**: (미작성) 통합
  `tests/integration/session.py::test_session_write_ac2_state_branch_disclosure`

### 시나리오 3: destructiveHint 광고
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 호출
- **기대 결과**: session_write 어노테이션이 destructiveHint=true, readOnlyHint=false.
- **검증 AC**: AC3
- **자동화**: (미작성) Go 단위 `internal/server/mcp_test.go`
  (`TestToolsListAdvertisesSessionWrite`) + 통합
  `tests/integration/session.py::test_session_write_ac3_destructive_hint`
  (공용 헬퍼 `_helpers.py::assert_destructive_annotation` 재사용, 파괴 동작 미실행)

### 시나리오 4: 거부 사유 구분
- **사전 조건**: 제어면 스텁이 not found / 페이로드 상한 초과 / 큐 포화 / 출력 쿼터 소진을
  각각 재현
- **실행 단계**: 네 상황에서 각각 write 호출 → 큐 포화·쿼터 소진 케이스는 직후
  `session_read` 호출
- **기대 결과**: 네 거부가 서로 다른 사유로 구별되어 도구 에러로 전달되고, 재시도가 의미
  있는 거부(큐 포화)와 그렇지 않은 거부(쿼터 소진·상한 초과)가 메시지에서 구분된다. 거부
  이후에도 기존 세션 출력은 read로 계속 조회된다.
- **검증 AC**: AC4
- **자동화**: (미작성) Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestWriteMapsControlPlaneRefusals`) + 통합
  `tests/integration/session.py::test_session_write_ac4_refusal_mapping`

### 시나리오 5: 미설정 시 도구 에러
- **사전 조건**: `SESSION_PLATFORM_ENDPOINT` 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러. 직후 `ping`은 여전히 `pong`.
- **검증 AC**: AC5
- **자동화**: (미작성) 통합
  `tests/integration/no_config.py::test_session_write_ac5_unconfigured_refusal`
