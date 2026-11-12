# PRD: session_archive

session-platform 세션을 60분 유휴 동결을 기다리지 않고 지금 동결하는 도구. 세션은 id·출력을 그대로
가진 채 `snapshot`이 되고 파드는 회수된다.

## 달성 가치
- **V5: 클러스터 내부 앱 기능의 도구화** — 제어면의 `POST /api/v1/sessions/{id}/snapshot`
  (session-platform AC-B1 · AC-A3)을 도구로 연다. 다 쓴 세션의 파드를 지금 돌려받거나, 지우기 전에
  동결해 `session_recover`가 되살릴 시점을 지금으로 당길 수 있다.
- **V3: 안전한 운영(Safe-by-default)** — 거부 사유(없음·잠시 충돌·지금은 불가)를 구분해 전달하고,
  엣지 유휴 타임아웃보다 오래 걸리는 동결도 요청이 끊기며 반쯤 중단되지 않는다. 미설정 시 graceful
  거부가 기본값이다.

## 도구 개요
- 입력: `id`(필수 — 동결할 세션의 id)
- 동작: 제어면에 동결을 요청한다. 제어면은 워크로드 타입별 아카이브(파일·대화 기록·누적 출력)를 쓰고
  파드를 회수해 세션을 `snapshot`으로 둔다. `claude-code`는 진행 중인 턴이 끝나기를 최대 5분 기다린
  뒤 동결한다. `shell` 안에서 돌던 프로세스는 동결을 건너지 못한다(파일과 출력은 건넌다). 이미
  `snapshot`인 세션은 그대로 돌려준다. 동결된 세션은 다음 `session_read`·`session_write`가 되살린다.
- 반환: 동결된 세션(`session` — `session_list`와 같은 필드, `pod` 없음). 대기 상한(약 20초) 안에
  끝나지 않으면 `pending: true`·`session`(`id`·`state: "archiving"`)·`message`를 `isError=false`로
  돌려주고, 동결은 요청과 떼어 낸 맥락에서 끝까지 진행된다.
- 서버 요구 설정: `SESSION_PLATFORM_ENDPOINT` (세션 도구 공유)
- 어노테이션: `readOnlyHint=false`, `destructiveHint=false`, `idempotentHint=true`,
  `openWorldHint=true`
  (destructive=false: 세션과 출력은 남고 다음 읽기·쓰기로 되돌아온다.
  idempotent=true: 이미 동결된 세션에 다시 부르면 같은 세션을 그대로 돌려준다)
- 승인 게이트: 대상 밖이다. 제어면 API 호출이지 쿠버네티스 권한을 행사하지 않으므로
  `prd-approval-gate`의 정의 범위 밖이다(`session_delete`·`session_recover`와 같은 자리).
- 범위: 테넌트 토큰을 싣는 서버(`prd-session-create` AC5)는 제어면에서 그 테넌트로 보이므로 자기
  테넌트 소속 세션만 동결할 수 있다. 아카이브가 꺼진 워크로드 타입은 동결되지 않는다(AC3의 unavailable).

> **구현 상태(2026-11-12)**: `internal/sessionplatform`의 `ArchiveSession`과 `internal/mcp`의
> `session_archive` 등록·디스패치가 이 PRD와 같은 변경에서 착지했다. 제어면 쪽 엔드포인트는 이미
> 있다(session-platform AC-B1). 시나리오별 자동화 현황은 `test-session-archive.md`의 각 「자동화」
> 필드가 말한다.

## Acceptance Criteria

### AC1: 세션의 즉시 동결
- **설명**: 살아 있는 세션의 id로 호출하면 같은 id의 세션이 `snapshot`으로 돌아오고 `pod`가 없다.
  동결 요청은 제어면에 한 번만 나가며, 목록·읽기·쓰기·생성·삭제는 부르지 않는다. 이미 `snapshot`인
  세션에 부르면 오류 없이 같은 세션을 돌려준다. 동결된 세션은 다음 `session_read`가 되살려 동결 전
  출력을 돌려준다.
- **달성 가치**: V5
- **검증 방법**: 출력에 마커 줄이 있는 세션을 동결하면 결과가 같은 id의 `snapshot` 세션이고 파드가
  사라지며, `session_list`에서 `snapshot`으로 보이고, 다시 동결해도 같은 결과이며, offset 0
  `session_read`가 마커 줄을 돌려준다.

### AC2: 긴 동결의 pending 응답
- **설명**: 동결이 대기 상한(엣지의 30초 유휴 타임아웃 아래인 약 20초) 안에 끝나지 않으면 도구는
  오류가 아니라 `pending: true`와, `session_list`에서 `snapshot`을 기다리라는 안내 `message`로 답한다.
  동결은 요청 맥락이 끊겨도 취소되지 않고 끝까지 진행되며, 제어면 요청은 여전히 한 번이다.
- **달성 가치**: V3
- **검증 방법**: 제어면 응답을 대기 상한보다 늦추면 도구가 상한 안에 pending으로 답하고, 그 뒤 제어면
  호출이 끝까지 완료된다.

### AC3: 거부 응답의 구분 전달
- **설명**: 제어면이 거부한 이유가 호출자에게 구분되어 전달된다 — 없는 id(not found: 지워졌거나
  없었거나 다른 테넌트의 세션), 다른 수명주기 작업이 세션을 잡고 있음(conflict: 그 작업이 끝난 뒤
  재시도할 가치가 있다), 지금은 동결할 수 없음(unavailable: 그 타입의 아카이브가 꺼졌거나 로그인
  슬롯이 없다)이 서로, 그리고 제어면 장애와 다르게 읽힌다. not found는 재시도를 권하지 않는다. `id`가
  없거나 문자열이 아니면 제어면에 요청을 보내기 전에 인자 오류로 거부된다.
- **달성 가치**: V3
- **검증 방법**: 없는 id·제어면 409·아카이브가 꺼진 세션(503)을 각각 재현해 서로 다른 도구 에러로
  읽히는지 확인한다.

### AC4: 도구 표기
- **설명**: `tools/list`에서 이 도구가 `readOnlyHint=false`, `destructiveHint=false`,
  `idempotentHint=true`, `openWorldHint=true`로 광고되고 `id`를 필수 인자로 받는다.
- **달성 가치**: V3
- **검증 방법**: `tools/list` 응답의 어노테이션과 `required`를 확인한다.

### AC5: 미설정 시 graceful 거부
- **설명**: `SESSION_PLATFORM_ENDPOINT`가 없으면 unavailable 류 에러를 반환하며, 서버
  기동·다른 도구에는 영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
