# PRD: session_recover

삭제된 session-platform 세션을 예전 id로 되살리는 도구. 세션이 마지막으로 동결된 시점의
상태로 새 파드에 복원된다.

## 달성 가치
- **V5: 클러스터 내부 앱 기능의 도구화** — 제어면의 `POST /api/v1/sessions/{id}/recover`
  (session-platform AC-B4)를 도구로 연다. `session_delete`로 지웠거나 파드를 잃어 정리된 세션도
  id만 알면 같은 표면에서 되돌릴 수 있다.
- **V3: 안전한 운영(Safe-by-default)** — 거부 사유(없음·아직 있음·지금은 불가)를 구분해 전달하고,
  미설정 시 graceful 거부가 기본값이다.

## 도구 개요
- 입력: `id`(필수 — 되살릴 세션의 id)
- 동작: 제어면에 복원을 요청한다. 제어면은 세션의 마지막 동결이 남긴 매니페스트로 같은 id의
  레코드를 다시 세우고, 그 아카이브를 새 파드에 전개해 `active`로 돌려준다. 마지막 동결 뒤의 작업은
  돌아오지 않는다. 파드를 세우므로 `session_create`와 같은 긴 대기 예산(5분)을 쓴다.
- 반환: 복원된 세션(`session` — `session_list`와 같은 필드)
- 서버 요구 설정: `SESSION_PLATFORM_ENDPOINT` (세션 도구 공유)
- 어노테이션: `readOnlyHint=false`, `destructiveHint=false`, `idempotentHint=false`,
  `openWorldHint=true`
  (destructive=false: 아무것도 지우지 않고 사라진 것을 되돌린다.
  idempotent=false: 두 번째 호출은 같은 결과가 아니라 「아직 있음」 거부로 끝난다)
- 승인 게이트: 대상 밖이다. 제어면 API 호출이지 쿠버네티스 권한을 행사하지 않으므로
  `prd-approval-gate`의 정의 범위 밖이다(`session_create`와 같은 자리).
- 범위: 테넌트 토큰을 싣는 서버(`prd-session-create` AC5)는 제어면에서 그 테넌트로 보이므로 자기
  테넌트 소속 세션만 되살릴 수 있다. 제어면이 그 기록을 남기기 전에 사라진 세션은 대상이 아니다.

> **구현 상태(2026-11-12)**: `internal/sessionplatform`의 `RecoverSession`과 `internal/mcp`의
> `session_recover` 등록·디스패치가 이 PRD와 같은 변경에서 착지했다. 제어면 쪽은
> dlddu/session-platform#229(AC-B4)다. 시나리오별 자동화 현황은 `test-session-recover.md`의 각
> 「자동화」 필드가 말한다.

## Acceptance Criteria

### AC1: 삭제된 세션의 복원
- **설명**: 동결된 적이 있는 세션을 지운 뒤 그 id로 호출하면, 같은 id·이름·워크로드 타입의 세션이
  `active`로 돌아오고 새 파드를 가진다. 복원 요청은 제어면에 한 번만 나가며, 목록·읽기·쓰기·생성은
  부르지 않는다. 결과의 세션은 곧바로 `session_read`·`session_write`의 대상이다.
- **달성 가치**: V5
- **검증 방법**: 세션을 만들어 동결하고 지운 뒤 복원하면 결과가 같은 id의 `active` 세션이고,
  `session_list`에 다시 나타나며, offset 0 `session_read`가 동결 전 출력을 돌려준다.

### AC2: 거부 응답의 구분 전달
- **설명**: 제어면이 거부한 이유가 호출자에게 구분되어 전달된다 — 되살릴 기록이 없는 id(not found:
  동결된 적이 없거나, 기록 이전에 사라졌거나, 다른 테넌트의 세션), 아직 살아 있는 id(still exists:
  되살릴 것이 없다), 지금은 복원할 수 없음(unavailable: 그 타입의 아카이브가 꺼졌거나 로그인 슬롯이
  없다)이 서로, 그리고 제어면 장애와 다르게 읽힌다. 앞의 둘은 재시도를 권하지 않는다. `id`가 없거나
  문자열이 아니면 제어면에 요청을 보내기 전에 인자 오류로 거부된다.
- **달성 가치**: V3
- **검증 방법**: 없는 id·살아 있는 id·제어면 503을 각각 재현해 서로 다른 도구 에러로 읽히는지
  확인한다.

### AC3: 도구 표기
- **설명**: `tools/list`에서 이 도구가 `readOnlyHint=false`, `destructiveHint=false`,
  `idempotentHint=false`, `openWorldHint=true`로 광고되고 `id`를 필수 인자로 받는다.
- **달성 가치**: V3
- **검증 방법**: `tools/list` 응답의 어노테이션과 `required`를 확인한다.

### AC4: 미설정 시 graceful 거부
- **설명**: `SESSION_PLATFORM_ENDPOINT`가 없으면 unavailable 류 에러를 반환하며, 서버
  기동·다른 도구에는 영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
