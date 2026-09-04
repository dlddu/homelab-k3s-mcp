# 테스트 문서: session_list

## 검증 대상 AC
- AC1: 세션 열거 (PRD: session_list)
- AC2: 상태를 바꾸지 않는 조회 (PRD: session_list)
- AC3: 미설정 시 graceful 거부 (PRD: session_list)

> **자동화 상태**: 도구가 구현돼 있고 Go 단위 테스트와 **통합 e2e가 세 AC를 모두 덮는다**
> (2026-09-04). 픽스처는 스텁이 아니라 **실 session-platform 제어면**이다
> (`tests/k8s/kind/session-platform.yaml` — 공개 이미지를 SHA로 핀해 kind에 띄운다).
> 모킹 정책상 실 구성요소를 CI에서 확보할 수 있으면 스텁을 쓸 수 없기 때문이고, 덕분에
> `docs/e2e-mocking-policy.md`의 허용목록에 새 항목이 늘지 않았다. 세션은 제어면의 실 상태
> 저장소에 시드한다(`tests/integration/_session_platform.py`). 파일 단위 매핑의 SSOT는
> `doc-tracker.md`이고 `check_ac_mapping.py`가 CI에서 강제한다.

## 테스트 시나리오

### 시나리오 1: 세션 목록 열거
- **사전 조건**: 제어면 픽스처에 세션 2개(`active` 1, `snapshot` 1)가 존재
- **실행 단계**: `session_list` 호출 → 세션이 하나도 없는 제어면에서 재호출
- **기대 결과**: 두 세션이 각각 id·이름·워크로드 타입·상태·`lastAccess`와 함께 열거되고,
  빈 제어면에서는 에러 없이 빈 목록이 반환된다.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestListSessionsReturnsAllSessions`, `TestListSessionsEmpty`) + MCP 표면
  `internal/server/mcp_test.go::TestSessionListEnumeratesSessions`·
  `::TestSessionListEmptyInventoryIsNotAnError` + 통합
  `tests/integration/session_list_ac1.py`
  (`test_session_list_ac1_empty_inventory` → 재고를 비워 빈 목록을 관측하고,
  `test_session_list_ac1_enumerates_sessions` → active·snapshot 둘을 시드해 열거와
  상태별 응답 모양(`pod` 유무)까지 단언한다 — 두 절이 한 파일 안에 있어 파일 간 실행 순서에
  의존하지 않는다)

### 시나리오 2: 조회가 상태를 바꾸지 않음
- **사전 조건**: `snapshot` 세션 1개 + `idle` 세션 1개 존재
- **실행 단계**: `session_list`를 연속 3회 호출하고, 각 호출 전후로 각 세션의 `state`·
  `lastAccess`와 네임스페이스의 파드 수를 비교
- **기대 결과**: 상태·`lastAccess`가 호출 전후로 동일하고, 스냅샷 복원으로 인한 새 파드가
  기동되지 않는다. (제어면이 read/write에만 "접근=active화"를 적용함을 도구 수준에서 확인)
- **검증 AC**: AC2
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go::TestListSessionsIsPassive`
  (연속 3회 호출이 `GET /api/v1/sessions` 외의 요청을 하나도 내지 않고 상태·`lastAccess`가
  불변임을 단언 — 도구가 상태를 바꿀 경로 자체를 갖지 않음을 보인다) + 통합
  `tests/integration/session_list_ac2.py::test_session_list_ac2_listing_is_passive`
  (파드 집합 비교는 실 제어면이 있어야 판별력을 가지므로 통합의 몫이다 — 스텁 앞에서는
  만들어질 파드가 없어 vacuous하다)

### 시나리오 3: 미설정 시 도구 에러
- **사전 조건**: `SESSION_PLATFORM_ENDPOINT` 미설정
- **실행 단계**: 호출
- **기대 결과**: 서버 정상, 호출만 unavailable 도구 에러. 직후 `ping`은 여전히 `pong`.
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/sessionplatform/sessionplatform_test.go`
  (`TestUnavailableFailsEveryCall`, `TestFromEnv`) + MCP 표면
  `internal/server/mcp_test.go::TestSessionListUnavailableReturnsToolError`
  (거부 직후 `ping`이 여전히 `pong`임까지 단언) + 통합
  `tests/integration/session_list_ac3.py::test_session_list_ac3_unconfigured_refusal`
  (자격증명 미부착 배포 변형 `auth-fixture.yaml` 재사용)
