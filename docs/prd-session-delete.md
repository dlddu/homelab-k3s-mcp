# PRD: session_delete

session-platform 세션 하나를 삭제하고, 그 세션이 쥐고 있던 파드를 회수시키는 도구.

## 달성 가치
- **V5: 클러스터 내부 앱 기능의 도구화** — 제어면의 `DELETE /api/v1/sessions/{id}`를 도구로
  열어, `session_create`로 연 세션을 같은 표면에서 닫을 수 있게 한다. 닫는 경로가 없으면 다 쓴
  세션의 파드가 유휴 회수(스냅샷)를 기다리며 남는다.
- **V3: 안전한 운영(Safe-by-default)** — 되돌릴 수 없는 삭제임을 `destructiveHint`로 명시하고,
  거부 사유를 구분해 전달하며, 미설정 시 graceful 거부가 기본값이다.

## 도구 개요
- 입력: `id`(필수 — 세션 id)
- 동작: 제어면에 삭제를 요청한다. 제어면은 세션의 워크로드 파드(와 보조 파드)의 삭제를 걸고
  세션 기록을 지운다. 세션 상태와 무관하게 같은 경로이며, **삭제는 대상을 활성화하지 않는다**
  — `idle`을 승격하거나 `snapshot`을 복원한 뒤 지우지 않는다. 제어면은 파드 삭제를 건 시점에
  응답하므로, 파드는 종료 유예 동안 `Terminating`으로 잠시 남을 수 있다.
- 반환: 삭제한 세션 `id`와 `deleted: true`
- 서버 요구 설정: `SESSION_PLATFORM_ENDPOINT` (세션 도구 공유)
- 어노테이션: `readOnlyHint=false`, `destructiveHint=true`, `idempotentHint=true`,
  `openWorldHint=true`
  (destructive=true: 세션의 누적 출력과 워크로드 상태가 사라지고 되돌릴 수 없다.
  idempotent=true: 같은 id의 재호출은 not found로 수렴하며 환경에 추가 효과가 없다)
- 승인 게이트: 대상 밖이다. 제어면 API 호출이지 쿠버네티스 권한을 행사하지 않으므로
  `prd-approval-gate`의 정의 범위 밖이다(`session_write`·`opensearch_document_delete`와 같은
  자리).

> **구현 상태(2026-10-16)**: `internal/sessionplatform`의 `DeleteSession`과 `internal/mcp`의
> `session_delete` 등록·디스패치가 이 PRD와 같은 변경에서 착지했다. 시나리오별 자동화 현황은
> `test-session-delete.md`의 각 「자동화」 필드가 말한다.

## Acceptance Criteria

### AC1: 세션 삭제와 파드 회수
- **설명**: 지정한 세션만 삭제된다 — 삭제 뒤 그 세션은 `session_list`에서 사라지고, 그 세션의
  파드는 회수된다. 다른 세션과 그 파드는 영향을 받지 않는다. 삭제 요청은 제어면에 한 번만
  나가며, 읽기·쓰기처럼 대상을 먼저 활성화하지 않는다.
- **달성 가치**: V5
- **검증 방법**: 세션을 만든 뒤 삭제하면 결과가 `deleted: true`이고, `session_list`에서 그 id가
  빠지며, 그 파드가 회수 예산 안에 네임스페이스에서 사라진다.

### AC2: 거부 응답의 구분 전달
- **설명**: 제어면이 거부한 이유가 호출자에게 구분되어 전달된다 — 없는 세션(not found: 이미
  지워졌거나 잘못된 id이며, 재시도할 일이 없다)과 진행 중인 생명주기 작업과의 충돌(conflict:
  스냅샷·복원 등이 끝나면 다시 시도할 수 있다)이 서로, 그리고 제어면 장애와 다르게 읽힌다.
  `id`가 없거나 문자열이 아니면 제어면에 요청을 보내기 전에 인자 오류로 거부된다.
- **달성 가치**: V3
- **검증 방법**: 없는 id를 삭제하면 not found 도구 에러가 반환되고 서버는 계속 동작한다.
  conflict는 제어면 응답 409를 재현해 not found와 다른 메시지로 읽히는지 확인한다.

### AC3: 파괴적 작업 표기
- **설명**: `tools/list`에서 이 도구가 `destructiveHint=true`(및 `readOnlyHint=false`)로
  광고된다.
- **달성 가치**: V3
- **검증 방법**: `tools/list` 응답의 어노테이션이 destructiveHint=true, readOnlyHint=false다.

### AC4: 미설정 시 graceful 거부
- **설명**: `SESSION_PLATFORM_ENDPOINT`가 없으면 unavailable 류 에러를 반환하며, 서버
  기동·다른 도구에는 영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
