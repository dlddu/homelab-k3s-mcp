# PRD: session_read

session-platform 세션의 누적 워크로드 출력을 오프셋 커서로 읽는 도구.

## 달성 가치
- **V5: 클러스터 내부 앱 기능의 도구화** — 제어면의 `POST /api/v1/sessions/{id}/read`를
  도구로 열어, 세션에 맡겨 둔 작업의 결과를 대화에서 바로 회수한다.
- **V3: 안전한 운영(Safe-by-default)** — 읽기가 유발하는 상태 전이(유휴 승격·스냅샷 복원)를
  결과에 명시적으로 드러내고, 미설정 시 graceful 거부가 기본값이다.

## 도구 개요
- 입력: `id`(필수 — 세션 id), `offset`(선택, 기본 0 — 이전 호출이 발급한 `nextOffset`)
- 동작: 제어면에 read를 요청해 `offset` 이후 누적된 출력 텍스트와 다음 커서를 받는다.
  `offset=0`은 세션 시작 이후 전체 출력을 반환한다. 워크로드 타입에 따라 반환되는 것이
  다르다 — `shell`은 PTY stdout/stderr 병합분, `claude-code`는 assistant 텍스트 델타와
  진단 stderr의 투영이다. 커서 규약(append-only 바이트 오프셋·비파괴)은 두 타입이 동일하다.
- 반환: `payload`, `nextOffset`, `path`(어느 상태 분기로 처리됐는지), 처리 후 세션 상태
- 서버 요구 설정: `SESSION_PLATFORM_ENDPOINT` (session_list와 공유)
- 어노테이션: `readOnlyHint=false`, `destructiveHint=false`, `idempotentHint=true`,
  `openWorldHint=true`
  (readOnly=false: 제어면 read는 대상 세션을 먼저 `active`로 만든 뒤 읽으므로 유휴 승격·
  스냅샷 복원이라는 부수 효과가 있다. 다만 기존 출력을 소비·삭제하지 않으므로 파괴적이지는
  않다)

> **구현 선행 문서**: 이 PRD 작성 시점(2026-08-12)에 session-platform 배포는 클러스터에서
> 제거된 상태이며(레포는 유지), 도구도 미구현이다.

## Acceptance Criteria

### AC1: 오프셋 커서 읽기
- **설명**: `offset` 이후 누적된 출력과 서버가 발급한 `nextOffset`을 반환한다. `offset=0`
  또는 미지정은 전체 출력을 반환하고, 직전 `nextOffset`으로 재호출하면 그 사이 새로 쌓인
  출력만 반환한다. 새 출력이 없으면 빈 payload와 동일한 커서를 반환한다. 읽기는 비파괴적
  이므로 같은 커서로 재호출하면 같은 구간을 다시 읽을 수 있다.
- **달성 가치**: V5
- **검증 방법**: `offset=0` 호출로 전체 출력을 받고, 반환된 `nextOffset`으로 재호출하면 빈
  payload가, 이후 새 출력이 쌓인 뒤 같은 커서로 호출하면 증분만 반환된다. 동일 커서 반복
  호출이 같은 구간을 반환한다.

### AC2: 상태 분기 노출
- **설명**: 제어면 read는 대상 세션을 먼저 `active`로 만든 뒤 읽는다(`active`는 직접,
  `idle`은 승격 후, `snapshot`은 복원 후). 도구는 어느 분기로 처리됐는지(`path`)와 처리
  후 세션 상태를 결과에 포함해, **읽기 한 번이 스냅샷 세션의 파드를 되살렸다는 사실**이
  호출자에게 드러나게 한다.
- **달성 가치**: V3
- **검증 방법**: `active`·`idle`·`snapshot` 세션에 각각 호출해 결과의 `path`가 실제 분기와
  일치하고, 호출 후 상태가 `active`로 보고된다.

### AC3: 대상 부재·잘못된 커서의 명확한 처리
- **설명**: 존재하지 않는 세션 id는 not found로, 음수·비정수 `offset`은 잘못된 인자로
  구분되는 도구 에러를 반환한다. 어느 경우에도 세션 상태는 바뀌지 않는다.
- **달성 가치**: V3
- **검증 방법**: 없는 id 호출은 not found 계열 에러, 음수 offset 호출은 인자 검증 에러를
  반환하며, 호출 후 대상 세션의 상태·`lastAccess`가 변하지 않는다.

### AC4: 미설정 시 graceful 거부
- **설명**: `SESSION_PLATFORM_ENDPOINT`가 없으면 unavailable 류 에러를 반환하며, 서버
  기동·다른 도구에는 영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
