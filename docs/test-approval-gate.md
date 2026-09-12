# 테스트 문서: approval-gate (승인 게이트 공통)

## 검증 대상 AC

- AC1: 동사 기반 게이트 강제 (PRD: approval-gate)
- AC2: 승인 요청 생성 (PRD: approval-gate)
- AC3: 판정 가능한 context (PRD: approval-gate)
- AC4: 판정 폴링 (PRD: approval-gate)
- AC5: Fail-closed (PRD: approval-gate)
- AC6: 승인–실행 정합성 (TOCTOU) (PRD: approval-gate)
- AC7: 1승인 1실행 (PRD: approval-gate)
- AC8: 감사 기록 (PRD: approval-gate)
- AC9: 자동 응답 모드의 가시화 (PRD: approval-gate)

## 픽스처

E2E는 `e2e-mocking-policy.md`의 실물 우선 원칙을 따른다. gatekeeper는 SQLite 한 파일로
기동하는 Go 백엔드이므로, 모킹 대신 **kind 클러스터에 실물 gatekeeper를 띄운다**
(`tests/k8s/kind/gatekeeper-fixture.yaml`). 승인·거절은 forward-auth 헤더(`Remote-User`)를
직접 실어 `PATCH /api/requests/{id}/approve|reject`를 호출해 사람 조작을 대신한다.

Go 단위 테스트에서는 `httptest.Server`로 gatekeeper HTTP 계약만 흉내 낸다. 이는 실물을 피하는
모킹이 아니라, 실물로는 만들기 어려운 실패 경로(5xx·타임아웃·연결 실패)를 만들기 위한 것이다.

## 테스트 시나리오

### 시나리오 1: 승인 없이는 클러스터에 닿지 않는다
- **사전 조건**: 가짜 k8s 서비스(호출 카운터 포함), gatekeeper는 `PENDING`을 유지
- **실행 단계**: `resource_apply`, `resource_delete`, `resource_scale`, `resource_restart`,
  `resource_exec`을 각각 호출하고 `GATEKEEPER_TIMEOUT_SECONDS` 경과까지 대기
- **기대 결과**: 다섯 호출 모두 에러 반환. 가짜 k8s 서비스 호출 카운트 **0**
- **검증 AC**: AC1, AC5
- **자동화**: (미작성) — 계획: Go 단위 `gatekeeper_test.go::TestGatedVerbsNeverReachKubeWithoutApproval`.
  통합 `approval_gate_ac1.py`

### 시나리오 2: 요청 본문 계약
- **사전 조건**: gatekeeper 스텁이 요청 본문을 기록
- **실행 단계**: 게이트 대상 도구를 서로 다른 대상으로 2회 호출
- **기대 결과**: `x-api-key` 헤더 존재. `externalId`·`context`·`requesterName`·
  `timeoutSeconds` 모두 존재. 두 호출의 `externalId`가 서로 다름.
  `GATEKEEPER_USER_ID` 설정 시 `userId` 포함
- **검증 AC**: AC2
- **자동화**: (미작성) — 계획: Go 단위 `gatekeeper_test.go::TestCreateRequestBodyContract`,
  `TestExternalIDIsUniquePerCall`. 통합 `approval_gate_ac2.py`

### 시나리오 3: context가 판정을 가능하게 한다
- **사전 조건**: 동일
- **실행 단계**: 다섯 동사 각각을 호출하고 생성된 `context` 문자열을 수집
- **기대 결과**: 모든 `context`에 도구 이름·`apiVersion`/`kind`/`namespace`/`name`·요청 시각이
  포함. `scale`은 현재→목표 레플리카, `restart`는 교체될 파드 수, `delete`는
  `gracePeriodSeconds`, `apply`는 생성/갱신 구분, `exec`는 **명령 인자 전문**이 축약 없이 포함.
  좌표를 해석할 수 없는 호출은 승인 요청을 만들지 않고 거부
- **검증 AC**: AC3
- **자동화**: (미작성) — 계획: Go 단위 `gatekeeper_test.go::TestContextIncludesVerbSpecificDetail`,
  `TestExecContextIncludesFullCommand`, `TestUnresolvableTargetIsRejectedBeforeRequest`.
  통합 `approval_gate_ac3.py`

### 시나리오 4: 폴링으로 판정을 관측한다
- **사전 조건**: kind 실물 gatekeeper
- **실행 단계**: (a) 게이트 대상 도구 호출 → 별도 경로로 `approve` → 실행 완료 확인.
  (b) `timeoutSeconds=2`로 호출하고 아무 판정도 하지 않은 채 대기
- **기대 결과**: (a) `PENDING`→`APPROVED` 전이가 폴링으로 관측되고 실행이 이어짐.
  (b) 폴링이 `EXPIRED`를 관측하고 거부. gatekeeper가 자발적으로 만료시키지 않으므로,
  폴링을 끊고 최초 응답만 본 구현은 이 시나리오를 통과하지 못함
- **검증 AC**: AC4
- **자동화**: (미작성) — 계획: 통합 `approval_gate_ac4.py`(실물 승인 + 만료 관측)

### 시나리오 5: 모든 실패는 거부로 수렴한다
- **사전 조건**: 가짜 k8s 서비스(호출 카운터), gatekeeper 스텁을 경로별로 구성
- **실행 단계**: 다음을 각각 재현 — `REJECTED`, `EXPIRED`, 타임아웃 내 판정 없음,
  `externalId` 충돌(409), 5xx, 연결 실패, `GATEKEEPER_BASE_URL` 미설정,
  `GATEKEEPER_API_KEY` 미설정
- **기대 결과**: 8가지 모두 도구 에러. 모든 경우 k8s 호출 카운트 0. 같은 조건에서
  `resource_list`·`resource_get`은 정상 동작(게이트 미설정이 읽기를 막지 않음)
- **검증 AC**: AC5
- **자동화**: (미작성) — 계획: Go 단위 `gatekeeper_test.go::TestFailClosedPaths`(표 기반 8 케이스),
  `TestReadToolsUnaffectedByGatekeeperOutage`. 통합 `approval_gate_ac5.py`

### 시나리오 6: 승인한 상태와 실행할 상태가 같아야 한다
- **사전 조건**: kind 실물 gatekeeper + 테스트 Deployment
- **실행 단계**: `resource_scale` 호출로 승인 요청 생성 → 승인 전에 외부에서 같은
  Deployment를 수정(`resourceVersion` 변경) → 승인
- **기대 결과**: 실행이 거부되고 재승인이 필요함을 알림. 레플리카는 변경되지 않음.
  `resource_exec`은 대상 파드를 삭제·재생성한 뒤 승인하면 `uid` 불일치로 거부
- **검증 AC**: AC6
- **자동화**: (미작성) — 계획: 통합 `approval_gate_ac6.py`(scale·exec 각 1케이스)

### 시나리오 7: 승인은 한 번만 쓰인다
- **사전 조건**: 동일
- **실행 단계**: 승인을 받아 실행한 뒤, 같은 승인 id로 실행을 재시도
- **기대 결과**: 두 번째 실행 거부. 실행 실패 후 재시도 시에도 새 승인 요청이 생성됨
  (기존 `externalId` 재사용 없음)
- **검증 AC**: AC7
- **자동화**: (미작성) — 계획: Go 단위 `gatekeeper_test.go::TestApprovalIsConsumedOnce`.
  통합 `approval_gate_ac7.py`

### 시나리오 8: 감사 로그
- **사전 조건**: 서버 로그 캡처
- **실행 단계**: 승인 후 실행 1회, 거절 1회
- **기대 결과**: 두 경우 모두 요청 id·`externalId`·도구·동사·대상 좌표가 로그에 남음.
  승인 실행에는 `processedById`가, 거부에는 거부 사유가 함께 남음
- **검증 AC**: AC8
- **자동화**: (미작성) — 계획: Go 단위 `gatekeeper_test.go::TestAuditLogFields`. 통합 `approval_gate_ac8.py`

### 시나리오 9: 자동 승인은 숨기지 않는다
- **사전 조건**: kind 실물 gatekeeper, 대상 사용자의 `autoResponseMode=AUTO_APPROVE`
- **실행 단계**: `GATEKEEPER_USER_ID`를 그 사용자로 두고 게이트 대상 도구 호출
- **기대 결과**: 실행은 되지만 도구 응답 본문에 자동 승인이었음이 표기되고, 로그에도
  `autoApproved=true`가 남음. `AUTO_REJECT` 사용자로는 실행이 거부됨
- **검증 AC**: AC9
- **자동화**: (미작성) — 계획: 통합 `approval_gate_ac9.py`(AUTO_APPROVE·AUTO_REJECT 각 1케이스)
