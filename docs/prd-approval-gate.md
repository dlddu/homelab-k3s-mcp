# PRD: approval-gate (승인 게이트 공통)

클러스터 상태를 바꾸는 도구 호출이 실행되기 **전에**, gatekeeper(`dlddu/gatekeeper`)의
사람 승인을 받아야만 통과하는 공통 경계.

개별 도구가 아니라 **동사(verb) 집합에 걸리는 횡단 관심사**다. `prd-platform-auth-safety`가
"누가 서버에 접근할 수 있는가"를 정하듯, 이 문서는 "인증을 통과한 호출이 무엇을 실행해도
되는가"를 정한다.

## 달성 가치

- **V3: 안전한 운영(Safe-by-default)** — `destructiveHint`는 클라이언트에 대한 *광고*일 뿐
  실행을 막지 못한다. 이 게이트는 서버 쪽에서 실행 자체를 막아, 파괴적 동작이 사람의
  명시적 판단 없이는 클러스터에 도달하지 못하게 한다.
- **V1: 자연어로 클러스터 운영** — 게이트가 있기 때문에 `resource_apply`·`resource_delete`
  같은 넓은 표면을 열 수 있다. 게이트는 기능을 제한하는 장치가 아니라, 넓은 표면을
  열기 위한 전제 조건이다.

## 게이트 대상

게이트는 **도구 이름이 아니라 동사로** 판정한다. 이름으로 판정하면 같은 일을 하는 도구가
새로 생길 때 게이트 밖으로 샌다.

| 동사 | 뜻 | 해당 도구 |
|------|-----|-----------|
| `apply` | 리소스 생성 또는 갱신(Server-Side Apply) | `resource_apply` |
| `delete` | 리소스 삭제 | `resource_delete` |
| `scale` | 레플리카 수 변경 | `resource_scale` |
| `exec` | 실행 중인 컨테이너 안에서 명령 실행 | `resource_exec` |

읽기 동사(`list`, `get`)는 게이트 대상이 아니다. 다만 Secret 배제는 읽기에도 똑같이
적용된다(`prd-resource-generic` AC5).

> **범위 밖 — 미결 사항**: 기존 도구 `workload_scale`, `workload_restart`,
> `dear_baby_reset_user`, `session_write`도 위 동사 정의에 해당하지만, 이 PRD는 아직
> 이들을 게이트에 편입하지 않는다. 편입하지 않으면 `resource_scale`이 막혀도
> `workload_scale`로 같은 일을 할 수 있어 게이트가 우회 가능해진다. 이 공백은
> `doc-tracker.md`의 "수용된 위험"에 미결 사항으로 기록한다.

## 구성

| 환경 변수 | 필수 | 설명 |
|-----------|------|------|
| `GATEKEEPER_BASE_URL` | O | gatekeeper 백엔드 주소 (예: `http://gatekeeper.gatekeeper.svc:3000`) |
| `GATEKEEPER_API_KEY` | O | `x-api-key` 헤더 값. `POST /api/requests`와 `GET /api/requests/{id}` 양쪽에 필요 |
| `GATEKEEPER_USER_ID` | X | 푸시 알림을 받을 사용자. 미지정 시 알림 없이 웹 UI 확인에만 의존 |
| `GATEKEEPER_TIMEOUT_SECONDS` | X | 승인 대기 상한. 기본 300 |
| `GATEKEEPER_POLL_INTERVAL_SECONDS` | X | 판정 폴링 주기. 기본 2 |

## Acceptance Criteria

### AC1: 동사 기반 게이트 강제
- **설명**: `apply`·`delete`·`scale`·`exec` 동사에 해당하는 모든 도구 호출은 gatekeeper
  판정이 `APPROVED`인 경우에만 쿠버네티스 API에 도달한다. 게이트를 건너뛰는 경로는
  존재하지 않으며, 게이트 호출은 도구 핸들러가 선택적으로 부르는 것이 아니라 디스패처
  단계에서 동사 테이블로 강제된다.
- **달성 가치**: V3
- **검증 방법**: 게이트 대상 도구를 승인 없이 호출했을 때 쿠버네티스 클라이언트가 단 한 번도
  호출되지 않는다(가짜 k8s 서비스의 호출 카운트가 0).

### AC2: 승인 요청 생성
- **설명**: 게이트 대상 호출은 `POST /api/requests`로 승인 요청을 만든다. `x-api-key` 헤더,
  `externalId`(호출마다 고유), `context`(AC3), `requesterName`,
  `timeoutSeconds`(`GATEKEEPER_TIMEOUT_SECONDS`)를 보낸다. `GATEKEEPER_USER_ID`가 있으면
  `userId`를 함께 보내 푸시 알림이 가게 한다.
- **달성 가치**: V3
- **검증 방법**: 요청 본문에 위 필드가 모두 담기고, `externalId`가 호출마다 달라진다.

### AC3: 판정 가능한 context
- **설명**: `context`는 운영자가 **푸시 알림과 승인 화면만 보고** 판단할 수 있어야 한다.
  다음을 모두 담는다 — 도구 이름, 대상 좌표(`apiVersion`/`kind`/`namespace`/`name`),
  동사별 상세, 그리고 요청 시각. 동사별 상세는:
  - `apply` — 신규 생성인지 갱신인지, 갱신이면 바뀌는 필드 요약
  - `delete` — 삭제 대상과 `gracePeriodSeconds`
  - `scale` — 현재 레플리카 → 목표 레플리카
  - `exec` — 컨테이너 이름과 **실행할 명령 전문**(요약·생략 금지)

  대상 좌표를 해석할 수 없거나 상세를 만들 수 없으면 승인 요청을 만들지 않고 거부한다.
  무엇을 승인하는지 모르는 승인 요청은 승인 버튼을 형식적 절차로 만든다.
- **달성 가치**: V3
- **검증 방법**: 동사별로 생성된 `context` 문자열에 위 구성 요소가 모두 포함된다. 특히
  `exec`의 명령 인자가 잘리거나 `[...]`로 축약되지 않는다.

### AC4: 판정 폴링
- **설명**: 요청 생성 후 `GET /api/requests/{id}`를 `GATEKEEPER_POLL_INTERVAL_SECONDS`
  주기로 폴링해 판정을 기다린다. `APPROVED`면 실행하고, `REJECTED`·`EXPIRED`면 거부한다.
  gatekeeper는 만료를 백그라운드로 처리하지 않고 **조회 시점에 평가**하므로
  (`expiresAt` 경과 + `PENDING` → `EXPIRED` 전이), 폴링 없이 최초 응답만 믿으면 만료를
  영영 관측하지 못한다.
- **달성 가치**: V3
- **검증 방법**: `PENDING` → `APPROVED` 전이가 폴링으로 관측되고 실행이 이어진다.
  `timeoutSeconds`가 지난 요청을 폴링하면 `EXPIRED`로 관측되고 거부된다.

### AC5: Fail-closed
- **설명**: 승인이 확인되지 않은 모든 경우는 거부다. 구체적으로 — `REJECTED`, `EXPIRED`,
  `GATEKEEPER_TIMEOUT_SECONDS` 내 판정 없음, `externalId` 충돌(409), gatekeeper 5xx,
  네트워크 오류·타임아웃, `GATEKEEPER_BASE_URL`/`GATEKEEPER_API_KEY` 미설정. 어느 경우든
  쿠버네티스 API를 호출하지 않고 에러를 반환한다.

  이는 이 서버의 기존 graceful degradation 원칙과 어긋나지 않는다. 통합이 미설정이면
  **해당 도구만** 에러를 반환하고 서버는 계속 산다는 원칙은 그대로이며, 여기서는 그
  "해당 도구"가 게이트 대상 도구 전체일 뿐이다.
- **달성 가치**: V3
- **검증 방법**: 위 실패 경로 각각에서 도구가 에러를 반환하고 가짜 k8s 서비스 호출 카운트가
  0이다. 특히 gatekeeper 미설정 상태에서 읽기 도구는 정상 동작한다.

### AC6: 승인–실행 정합성 (TOCTOU)
- **설명**: 승인 요청을 만든 시각과 실제로 실행하는 시각 사이에 대상이 바뀔 수 있다.
  운영자가 승인한 것은 "그 시점의 그 상태에 대한 그 변경"이므로, 실행 직전에 대상의
  `resourceVersion`이 승인 요청 작성 시 읽은 값과 같은지 확인한다. 다르면 실행하지 않고
  거부하며, 재승인이 필요함을 알린다. `exec`는 대상 파드의 `uid`로 같은 확인을 한다
  (같은 이름의 새 파드는 다른 파드다).
- **달성 가치**: V3
- **검증 방법**: 승인 후 실행 전에 대상을 외부에서 변경하면 실행이 거부된다.

### AC7: 1승인 1실행
- **설명**: 승인 하나는 단 한 번의 쿠버네티스 API 호출만 인가한다. 승인을 소비한 뒤
  그 `externalId`·요청 id는 폐기되며 재사용되지 않는다. 실행이 실패해 재시도할 때도
  승인을 새로 받는다.
- **달성 가치**: V3
- **검증 방법**: 같은 승인 id로 두 번째 실행을 시도하면 거부된다.

### AC8: 감사 기록
- **설명**: 게이트 대상 호출마다 요청 id, `externalId`, 도구·동사·대상 좌표, 판정 결과,
  판정자(`processedById`), 실행 결과를 서버 로그에 남긴다. 승인 기록 없이 실행된 게이트
  대상 호출은 로그상 존재할 수 없다.
- **달성 가치**: V3
- **검증 방법**: 승인 후 실행한 호출의 로그에 요청 id와 판정자가 남고, 거부된 호출은
  거부 사유와 함께 남는다.

### AC9: 자동 응답 모드의 가시화
- **설명**: gatekeeper는 사용자별 `autoResponseMode`(`AUTO_APPROVE`/`AUTO_REJECT`)를 지원하며,
  `AUTO_APPROVE`가 켜져 있으면 사람이 보지 않은 채 승인이 떨어진다. 이는 게이트를 사실상
  무력화하는 경로이므로 숨기지 않는다. 요청 생성 응답의 `autoApproved`/`autoRejected`를
  로그에 남기고, 자동 승인으로 통과한 실행은 **도구 응답 본문에도** 자동 승인이었음을
  표기한다.
- **달성 가치**: V3
- **검증 방법**: `AUTO_APPROVE` 사용자로 게이트 대상 도구를 호출하면 응답에 자동 승인 표기가
  포함되고, 로그에도 남는다.
