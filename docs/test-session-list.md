# 테스트 문서: session_list

## 검증 대상 AC
- AC1: 세션 열거 (PRD: session_list)
- AC2: 상태를 바꾸지 않는 조회 (PRD: session_list)
- AC3: 미설정 시 graceful 거부 (PRD: session_list)

> **자동화 상태**: 도구 미구현(구현 선행 문서). 아래 "자동화" 필드는 구현 시 작성할 대상
> 경로를 지목한다. e2e는 제어면 스텁 또는 kind에 띄운 session-platform 제어면을 픽스처로
> 하며, per-AC 케이스 규약(doc-tracker 규칙 1·2·3)을 따른다.

## 테스트 시나리오

### 시나리오 1: 세션 목록 열거
- **사전 조건**: 제어면 픽스처에 세션 2개(`active` 1, `snapshot` 1)가 존재
- **실행 단계**: `session_list` 호출 → 세션이 하나도 없는 제어면에서 재호출
- **기대 결과**: 두 세션이 각각 id·이름·워크로드 타입·상태·`lastAccess`와 함께 열거되고,
  빈 제어면에서는 에러 없이 빈 목록이 반환된다.
- **검증 AC**: AC1
- **자동화**: (미작성) Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestListSessionsReturnsAllSessions`, `TestListSessionsEmpty`) + 통합
  `tests/integration/session.py::test_session_list_ac1_enumerates_sessions`

### 시나리오 2: 조회가 상태를 바꾸지 않음
- **사전 조건**: `snapshot` 세션 1개 + `idle` 세션 1개 존재
- **실행 단계**: `session_list`를 연속 3회 호출하고, 각 호출 전후로 각 세션의 `state`·
  `lastAccess`와 네임스페이스의 파드 수를 비교
- **기대 결과**: 상태·`lastAccess`가 호출 전후로 동일하고, 스냅샷 복원으로 인한 새 파드가
  기동되지 않는다. (제어면이 read/write에만 "접근=active화"를 적용함을 도구 수준에서 확인)
- **검증 AC**: AC2
- **자동화**: (미작성) 통합
  `tests/integration/session.py::test_session_list_ac2_no_state_change`

### 시나리오 3: 미설정 시 도구 에러
- **사전 조건**: `SESSION_PLATFORM_ENDPOINT` 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러. 직후 `ping`은 여전히 `pong`.
- **검증 AC**: AC3
- **자동화**: (미작성) Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestUnavailableFailsEveryCall`) + 통합
  `tests/integration/no_config.py::test_session_list_ac3_unconfigured_refusal`
  (자격증명 미부착 배포 변형 `auth-fixture.yaml` 재사용)
