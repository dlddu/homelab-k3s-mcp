# PRD: session_write

session-platform 세션의 워크로드에 입력을 주입하는 도구.

## 달성 가치
- **V5: 클러스터 내부 앱 기능의 도구화** — 제어면의 `POST /api/v1/sessions/{id}/write`를
  도구로 열어, 오래 걸리는 작업을 세션에 맡기고 대화를 이어갈 수 있게 한다.
- **V3: 안전한 운영(Safe-by-default)** — 세션 워크로드에서 임의 동작을 유발할 수 있음을
  `destructiveHint`로 명시하고, 거부 응답을 구분해 전달하며, 미설정 시 graceful 거부가
  기본값이다.

## 도구 개요
- 입력: `id`(필수 — 세션 id), `payload`(필수 — 주입할 입력)
- 동작: 제어면에 write를 요청한다. 워크로드 타입에 따라 의미가 다르다 — `shell`은 PTY
  stdin 주입(명령·키 입력), `claude-code`는 프롬프트 1회 실행을 큐에 넣는다. 두 타입 모두
  **비블로킹**으로 반환하므로, 실행 결과는 이후 `session_read`로 회수한다.
- 반환: `path`(어느 상태 분기로 처리됐는지), 처리 후 세션 상태
- 서버 요구 설정: `SESSION_PLATFORM_ENDPOINT` (session_list와 공유)
- 어노테이션: `readOnlyHint=false`, `destructiveHint=true`, `idempotentHint=false`,
  `openWorldHint=true`
  (destructive=true: 주입된 입력이 세션 안에서 임의의 명령·프롬프트로 실행된다.
  idempotent=false: 같은 payload 재전송은 같은 동작을 한 번 더 실행한다)

> **구현 상태(2026-09-04)**: 도구 계층은 구현됐다 — `internal/sessionplatform`의
> `WriteSession`(제어면 `POST /api/v1/sessions/{id}/write` 클라이언트)과 `internal/mcp`의
> `session_write` 등록·디스패치이며, AC1~AC5가 Go 단위로 검증된다. 이 PRD 작성 시점
> (2026-08-12)에 클러스터에서 제거돼 있던 session-platform 배포도 2026-09-03에 복구됐다.
> 통합 e2e(`tests/integration/`)는 아직 미작성이며, 그 축의 소유자는 AC ↔ e2e 1:1 렌즈다
> (`docs/doc-tracker.md`의 e2e 렌즈 절).

## Acceptance Criteria

### AC1: 워크로드 입력 주입
- **설명**: `payload`가 대상 세션의 워크로드에 전달된다. `shell`은 PTY stdin으로 주입되어
  실행되고, `claude-code`는 프롬프트 1회 실행이 큐에 적재된다. 호출은 실행 완료를 기다리지
  않고 반환하며, 산출물은 `session_read`의 누적 출력으로 관측된다.
- **달성 가치**: V5
- **검증 방법**: shell 세션에 명령을 write한 뒤 read로 그 명령의 출력이 누적 출력에
  나타남을 확인한다. claude-code 세션에 프롬프트를 write하면 즉시 반환되고, 이후 read에서
  응답 텍스트가 누적된다.

### AC2: 상태 분기 처리와 노출
- **설명**: write도 read와 같은 "접근=active화" 규칙을 따른다 — `active`는 직접,
  `idle`은 승격 후, `snapshot`은 **거부하지 않고 복원 후** 적용한다. 도구는 어느 분기로
  처리됐는지(`path`)와 처리 후 상태를 결과에 포함해, 쓰기 한 번이 스냅샷 세션을 되살렸다는
  사실이 호출자에게 드러나게 한다.
- **달성 가치**: V3
- **검증 방법**: `active`·`idle`·`snapshot` 세션에 각각 write하여 모두 반영되고, 결과의
  `path`가 실제 분기와 일치하며 호출 후 상태가 `active`로 보고된다.

### AC3: 파괴적 작업 표기
- **설명**: `tools/list`에서 이 도구가 `destructiveHint=true`(및 `readOnlyHint=false`)로
  광고된다. 주입된 입력은 세션 안에서 임의 명령·프롬프트로 실행되므로 되돌릴 수 없는 부수
  효과를 낼 수 있다.
- **달성 가치**: V3
- **검증 방법**: `tools/list` 응답의 어노테이션이 destructiveHint=true, readOnlyHint=false다.

### AC4: 거부 응답의 구분 전달
- **설명**: 제어면이 거부한 이유가 호출자에게 구분되어 전달된다 — 없는 세션(not found),
  페이로드 상한 초과(`claude-code` 프롬프트 1 MiB), 프롬프트 큐 포화(잠시 후 재시도 가능),
  출력 쿼터 소진(기존 출력은 계속 읽을 수 있으나 새 쓰기는 거부됨). 재시도가 의미 있는
  거부와 그렇지 않은 거부가 메시지에서 구별된다.
- **달성 가치**: V3
- **검증 방법**: 각 거부 상황에서 서로 다른 사유가 담긴 도구 에러가 반환되고, 큐 포화·쿼터
  소진의 경우 기존 세션 출력은 `session_read`로 계속 조회된다.

### AC5: 미설정 시 graceful 거부
- **설명**: `SESSION_PLATFORM_ENDPOINT`가 없으면 unavailable 류 에러를 반환하며, 서버
  기동·다른 도구에는 영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
