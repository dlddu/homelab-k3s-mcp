# 테스트 문서: pocket_aide_chat_presence_get

## 검증 대상 AC
- AC1: 접속 상태 조회 (PRD: pocket_aide_chat_presence_get)
- AC2: 「모름」과 「떠남」을 구분한다 (PRD: pocket_aide_chat_presence_get)
- AC3: 인자를 받지 않는다 (PRD: pocket_aide_chat_presence_get)
- AC4: PocketAide 거부 사유의 구분 전달 (PRD: pocket_aide_chat_presence_get)
- AC5: 미설정 시 graceful 거부 (PRD: pocket_aide_chat_presence_get)

> **자동화 상태**: 도구 계층은 구현·검증됐다 — 아래 "자동화" 필드의 Go 단위는 실재하는 테스트
> 이름이다. 통합 e2e 는 **다섯 시나리오 모두 `(미작성)`** 이고, 그 이유는 픽스처 핀이다:
> `POCKET_AIDE_SHA` 가 가리키는 판에 `GET /api/external/chat-presence` 가 아직 없어 실물 상대가
> 404 만 답한다(그 경로는 dlddu/pocket-aide PRD-12 AC22 와 같은 변경으로 들어간다). 핀이 그 경로를
> 담은 판으로 올라가면 시나리오 1·3·4 가 통합으로 닫히고, 시나리오 2 의 `last_seen_at: null` 은
> PocketAide 파드를 막 띄운 직후 창에서만 관측되므로 그때 재판정한다. 시나리오 1 의 「45초가 지나면
> 떠난 것으로 보인다」와 실기기 포그라운드 전환은 PocketAide 쪽(dlddu/pocket-aide
> `test-approval-gate.md#시나리오 26`)이 검증하고 여기서는 관측 밖이다.

## 테스트 시나리오

### 시나리오 1: 사용자가 채팅 화면을 보고 있는지 그대로 돌려받는다
- **사전 조건**: PocketAide 에 이 서버의 ServiceAccount 가 호출자로 등록돼 있고, 그 사용자가 채팅
  화면을 열어 둔 상태
- **실행 단계**: `pocket_aide_chat_presence_get` 을 `{}` 로 호출
- **기대 결과**: 결과가 `{present: true, last_seen_at: <직전 읽기 시각>, ttl_seconds: 45}` 로
  PocketAide 가 답한 값과 같다. PocketAide 가 받은 요청은 `GET /api/external/chat-presence` 한 건에
  projected 토큰 Bearer 이고, 본문·질의 문자열이 없다(조회 대상이 요청에 실리지 않는다). 토큰 파일이
  교체되면 다음 호출이 새 토큰을 쓴다. 승인 요청·알림은 생기지 않는다.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/server/pocket_aide_chat_presence_test.go`
  (`TestPocketAideChatPresenceGetReportsPresent`) +
  `internal/pocketaide/chatpresence_test.go` (`TestChatPresenceGetsWithTheServiceAccountToken`,
  `TestChatPresenceReadsTheTokenOnEveryCall`). 화면을 떠난 뒤 45초가 지나 `present` 가 뒤집히는 것은
  PocketAide 쪽이 검증한다. 통합 e2e `(미작성)`
- **구현 상태**: 구현됨

### 시나리오 2: 「한 번도 못 봤다」와 「있었다가 떠났다」가 다르게 돌아온다
- **사전 조건**: PocketAide 가 방금 떠서 그 화면을 아직 본 적 없는 상태(1), 사용자가 채팅 화면을 열었다
  닫고 45초가 지난 상태(2)
- **실행 단계**: 두 상태에서 각각 `{}` 로 호출
- **기대 결과**: (1) `present: false` 이고 `last_seen_at` 키가 있으면서 값이 `null` 이다 — 0 이나 키
  누락이 아니다. (2) `present: false` 이고 `last_seen_at` 이 마지막 읽기 시각이다. 두 답이 구분돼
  에이전트가 「모름」을 「방금 떠남」으로 읽지 않는다.
- **검증 AC**: AC2
- **자동화**: Go 단위 `internal/server/pocket_aide_chat_presence_test.go`
  (`TestPocketAideChatPresenceGetDistinguishesUnknownFromAway`) +
  `internal/pocketaide/chatpresence_test.go` (`TestChatPresenceKeepsNullLastSeenDistinctFromZero`).
  통합 e2e `(미작성)`
- **구현 상태**: 구현됨

### 시나리오 3: 인자를 실으면 거부되고 조회가 나가지 않는다
- **사전 조건**: 서버 기동(PocketAide 설정 여부 무관)
- **실행 단계**: `{"user": "alice"}` 로 호출 → `{"userId": 1}` 로 호출 → `{}` 로 호출 → `tools/list`
- **기대 결과**: 앞의 둘은 「takes no arguments」 인자 오류이고 PocketAide 에 요청이 나가지 않는다 —
  조용히 무시되지 않는다. 셋째 호출은 조회 1건이 나간다. `tools/list` 의 입력 스키마는 속성 0 ·
  `additionalProperties: false` 이고 어노테이션이 `readOnlyHint: true`·`idempotentHint: true` 다.
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/server/pocket_aide_chat_presence_test.go`
  (`TestPocketAideChatPresenceGetTakesNoArguments`,
  `TestToolsListAdvertisesPocketAideChatPresenceGet`). 통합 e2e `(미작성)`
- **구현 상태**: 구현됨

### 시나리오 4: PocketAide 의 거부 사유가 구분돼 돌아오고 「접속 아님」으로 둔갑하지 않는다
- **사전 조건**: 실물 PocketAide(조회 엔드포인트 포함)와, 그 경로가 없는 구 버전 PocketAide
- **실행 단계**: 호출자로 등록되지 않은 ServiceAccount 의 배포에서 호출 → 구 버전 상대로 호출 →
  5xx 를 내는 상대로 호출
- **기대 결과**: 차례로 「호출자로 등록돼 있는지(401)」, 「그 엔드포인트가 있는 PocketAide 를 배포하라
  (404, PRD-12 AC22)」, 상태 코드를 담은 도구 에러다. 셋 다 미설정(unavailable) 거부와 다르게 읽히고,
  **어느 것도 `present: false` 결과로 돌아오지 않는다.**
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/server/pocket_aide_chat_presence_test.go`
  (`TestPocketAideChatPresenceGetSurfacesRefusals`) + `internal/pocketaide/chatpresence_test.go`
  (`TestChatPresenceExplainsEachRefusal`). 통합 e2e `(미작성)`
- **구현 상태**: 구현됨

### 시나리오 5: PocketAide 미설정 배포에서 graceful 거부
- **사전 조건**: `APPROVAL_GATE_URL` 없이 기동한 배포
- **실행 단계**: `pocket_aide_chat_presence_get` 을 `{}` 로 호출 → 직후 `ping`
- **기대 결과**: unavailable 류 도구 에러(`pocket-aide unavailable: … APPROVAL_GATE_URL`)이고
  `present` 를 꾸며 내지 않는다. 직후 `ping` 이 `pong` 으로, 서버와 다른 도구는 영향받지 않는다.
- **검증 AC**: AC5
- **자동화**: Go 단위 `internal/server/pocket_aide_chat_presence_test.go`
  (`TestPocketAideChatPresenceGetSurfacesRefusals` 의 미설정 구간) +
  `internal/pocketaide/chatpresence_test.go` (`TestUnavailableChatPresenceRefusesWithTheReason`).
  통합 e2e `(미작성)`
- **구현 상태**: 구현됨
