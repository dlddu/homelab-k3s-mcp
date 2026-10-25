# 테스트 문서: pocket_aide_notification_send

## 검증 대상 AC
- AC1: 알림 발송과 도달 수 (PRD: pocket_aide_notification_send)
- AC2: 입력 검증이 PocketAide 보다 앞선다 (PRD: pocket_aide_notification_send)
- AC3: PocketAide 거부 사유의 구분 전달 (PRD: pocket_aide_notification_send)
- AC4: 미설정 시 graceful 거부 (PRD: pocket_aide_notification_send)

> **자동화 상태**: 도구 계층은 구현·검증됐다 — 아래 "자동화" 필드의 Go 단위는 실재하는 테스트
> 이름이다. 통합 e2e 의 시나리오별 현황은 **아래 각 "자동화" 필드가 그 자리에서 말한다**
> (`(미작성)` 이면 아직 파일이 없다는 뜻이고, 그 표기는 `tests/integration/check_ac_mapping.py`
> 규칙 8 이 실측 파일 집합과 대조한다). 시나리오 1 의 실제 도달은 실기기·Apple 자격 증명이 필요한
> 외부 경계라 e2e 예외이고, 시나리오 3 은 고정된 PocketAide 픽스처(`POCKET_AIDE_SHA`)에 알림
> 엔드포인트가 들어오기 전까지 e2e 가 관측할 대상이 없다.

## 테스트 시나리오

### 시나리오 1: 보낸 제목·본문이 등록 사용자의 기기로 가고 도달 수를 돌려받는다
- **사전 조건**: PocketAide 에 이 서버의 ServiceAccount 가 호출자로 등록돼 있고, 등록한 사용자가
  기기 두 대를 등록해 둔 상태
- **실행 단계**: `pocket_aide_notification_send` 를 `{title: "백업 완료", body: "스냅샷 3개"}` 로 호출 →
  `{title: "ping"}` 으로 한 번 더 호출
- **기대 결과**: 첫 결과가 `{devices: 2, sent: 2}` 이고 두 기기에 제목·본문이 그대로 도착한다.
  PocketAide 가 받은 요청은 `POST /api/external/notifications` 에 projected 토큰 Bearer 다. 둘째
  호출은 본문 없이 나간다. 토큰 파일이 교체되면 다음 호출이 새 토큰을 쓴다.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/pocketaide/pocketaide_test.go`
  (`TestNotifyPostsTitleAndBodyWithTheServiceAccountToken`, `TestNotifyOmitsAnEmptyBody`,
  `TestNotifyReadsTheTokenOnEveryCall`) + `internal/server/pocket_aide_test.go`
  (`TestPocketAideNotificationSendReturnsDeliveryCounts`, `TestPocketAideNotificationSendBodyIsOptional`).
  기기 도달은 PocketAide 쪽(dlddu/pocket-aide `test-approval-gate.md#시나리오 19`)이 검증한다.
  통합 e2e 는 예외(실 APNs·실기기).

### 시나리오 2: 잘못된 제목은 PocketAide 에 닿기 전에 거부된다
- **사전 조건**: 서버 기동(PocketAide 설정 여부 무관)
- **실행 단계**: `title` 없이, 공백뿐인 `title` 로, 121자 `title` 로 각각 호출 → 숫자 `title` 로 호출 →
  직후 `ping`
- **기대 결과**: 앞의 셋은 「invalid notification」 도구 에러이고 PocketAide 설정 여부와 무관하게 같은
  문구다. 숫자 `title` 은 인자 오류다. PocketAide 에 요청이 나가지 않고 서버는 계속 동작한다.
- **검증 AC**: AC2
- **자동화**: Go 단위 `internal/server/pocket_aide_test.go`
  (`TestPocketAideNotificationSendRefusesBadTitlesBeforeSending`,
  `TestPocketAideNotificationSendUnconfiguredRefusesEvenABadTitleTheSameWay`) +
  `internal/pocketaide/pocketaide_test.go` (`TestNotifyRefusesBadTitlesWithoutARequest`) + 통합
  `tests/integration/pocket_aide_notification_send_sc2.py`

### 시나리오 3: PocketAide 의 거부 사유가 구분돼 돌아온다
- **사전 조건**: 실물 PocketAide(알림 엔드포인트 포함, APNs 미설정)
- **실행 단계**: 호출자로 등록된 배포에서 호출 → 등록되지 않은 ServiceAccount 의 배포에서 호출
- **기대 결과**: 등록된 배포는 「APNs 미설정(503)」 도구 에러, 미등록 배포는 「호출자로 등록돼 있는지
  (401)」 도구 에러다. 둘 다 미설정(unavailable) 거부와 다르게 읽힌다. (400·500 의 문구는 Go 단위만
  검증)
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/pocketaide/pocketaide_test.go` (`TestNotifyExplainsEachRefusal`) +
  `internal/server/pocket_aide_test.go` (`TestPocketAideNotificationSendSurfacesPocketAideRefusal`) +
  통합 (미작성)

### 시나리오 4: 미설정 시 도구 에러
- **사전 조건**: `APPROVAL_GATE_URL` 미설정
- **실행 단계**: `{title: "x"}` 로 호출 → 직후 `ping`
- **기대 결과**: 서버 정상, 호출만 `pocket-aide unavailable` 도구 에러(`APPROVAL_GATE_URL` 을 이름으로
  댄다). 직후 `ping` 은 여전히 `pong`.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/server/pocket_aide_test.go`
  (`TestPocketAideNotificationSendUnconfiguredRefusesEvenABadTitleTheSameWay`) +
  `internal/pocketaide/pocketaide_test.go` (`TestUnavailableIsTheUnconfiguredKind`,
  `TestFromEnvFollowsTheApprovalGateSettings`) + 통합
  `tests/integration/pocket_aide_notification_send_sc4.py`(`실행 대상: auth-variant`)
