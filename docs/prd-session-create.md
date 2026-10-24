# PRD: session_create

session-platform 제어면에 새 세션을 만드는 도구. 세션마다 전용 워크로드 파드가 프로비저닝되고,
만들어진 세션은 곧바로 `session_read`·`session_write`의 대상이 된다.

## 달성 가치
- **V5: 클러스터 내부 앱 기능의 도구화** — 제어면의 `POST /api/v1/sessions`를 도구로 열어,
  세션을 쓰려고 SPA나 포트포워딩을 거치지 않게 한다. 기존 세션 도구 3종은 이미 있는 세션만
  다룰 수 있었고, 세션을 새로 여는 것은 도구 밖의 일이었다.
- **V3: 안전한 운영(Safe-by-default)** — 잘못된 요청은 파드를 만들기 전에 거부되고, 생성이
  끝나기 전에 도구가 먼저 포기해 세션을 고아로 남기지 않으며, 미설정 시 graceful 거부가
  기본값이다.

## 도구 개요
- 입력: `name`(필수 — 공백만으로 된 이름은 거부), `workloadType`(선택 — **제어면이 제공하는
  타입 중 하나**, 생략 시 `shell`), `model`(선택 — 에이전트 타입 전용, 생략 시 제어면이
  `platform-default`로 기록)
- 워크로드 타입 목록은 이 서버가 갖지 않는다. 제어면의 `GET /api/v1/config`가 그 배포에서
  제공하는 타입(`workloadTypes[].type`)을 내려주므로, `shell` 밖의 타입이 오면 호출마다 그
  목록을 읽어 판정한다. 그래서 플랫폼에 타입이 늘어도(예: `minimax-code`) 이 서버는 바뀌지
  않고, 이미지가 배선되지 않은 타입은 플랫폼에 있어도 거부된다. `tools/list`의 `workloadType`
  스키마에는 열거를 두지 않는다 — 고정된 열거가 제어면과 어긋나는 것이 이 결정의 이유다.
  목록을 내려주지 않는 오래된 제어면이면 판정을 그 제어면의 400에 맡긴다.
- 동작: 제어면에 생성을 요청한다. 제어면은 전용 파드를 띄우고 그 파드의 워크로드 에이전트가
  응답할 때까지 기다린 뒤에야 응답하므로, 이 도구는 **블로킹**이다 — 반환된 세션은 이미
  `active`이고 `pod`를 가진다. 워크로드 타입은 생성 후 바꿀 수 없다.
- 반환: 만들어진 세션(`id`, `name`, `workloadType`, `state`, `pod`, `createdAt`,
  `lastAccess`)
- 생성 대기 상한: 제어면의 파드 Ready 대기 기본값(2분)보다 넉넉한 **5분**. 도구가 제어면보다
  먼저 끊으면, 제어면이 띄운 파드는 남는데 호출자는 그 세션의 id를 받지 못한다.
- 서버 요구 설정: `SESSION_PLATFORM_ENDPOINT` (세션 도구 공유)
- 어노테이션: `readOnlyHint=false`, `destructiveHint=false`, `idempotentHint=false`,
  `openWorldHint=true`
  (destructive=false: 기존 세션·출력을 바꾸지 않는 추가적 변경이다. idempotent=false: 같은
  인자로 다시 부르면 이름이 같은 세션이 하나 더 생기고 파드도 하나 더 뜬다 — 이름은 고유
  키가 아니다)
- 승인 게이트: 대상 밖이다. 제어면 API 호출이지 쿠버네티스 권한을 행사하지 않으므로
  `prd-approval-gate`의 정의 범위 밖이다(`session_write`와 같은 자리).

> **개정(2026-10-24)**: `workloadType`의 고정 열거(`shell`·`claude-code`·`approval-gated`)를
> 걷고 제어면의 타입 목록으로 판정하도록 바꿨다 — 플랫폼이 `minimax-code`를 더한 뒤에도 이
> 도구가 그 타입을 거부하고 있었기 때문이다.
>
> **구현 상태(2026-10-16)**: `internal/sessionplatform`의 `CreateSession`과 `internal/mcp`의
> `session_create` 등록·디스패치가 이 PRD와 같은 변경에서 착지했다. 시나리오별 자동화 현황은
> `test-session-create.md`의 각 「자동화」 필드가 말한다.

## Acceptance Criteria

### AC1: 세션 생성과 활성 상태 반환
- **설명**: 호출이 성공하면 제어면에 새 세션이 생기고, 도구는 그 세션의 워크로드 파드가 뜰
  때까지 기다린 뒤 `state=active`이고 `pod`를 가진 세션을 반환한다. 만들어진 세션은
  `session_list`에 나타나고, 반환 시점에 그 파드는 이미 존재한다. 도구의 생성 대기 상한은
  제어면의 파드 대기 상한보다 길다.
- **달성 가치**: V5
- **검증 방법**: shell 세션을 생성한 뒤 반환된 세션이 `active`이고 `pod`를 가지며, 그 파드가
  네임스페이스에 실재하고, `session_list`가 같은 id를 열거함을 확인한다.

### AC2: 잘못된 요청은 아무것도 만들지 않는다
- **설명**: 이름이 비었거나 공백뿐인 요청과 문자열이 아닌 인자는 제어면에 요청을 보내기 전에
  인자 오류(`-32602`)로 거부된다. 제어면이 제공하지 않는 `workloadType`은 제어면의 타입
  목록(`GET /api/v1/config`)을 읽은 뒤 **생성 요청을 보내기 전에** `invalid argument` 도구
  에러로 거부되고, 그 메시지는 제공되는 타입을 나열한다. 제어면만 판정할 수 있는 거부(예:
  `shell` 세션에 `model` 지정)도 `invalid argument`로 구분되어 전달되며, 제어면 장애와 다르게
  읽힌다. 어느 경우든 세션도 파드도 생기지 않는다.
- **달성 가치**: V3
- **검증 방법**: 각 잘못된 요청 뒤 세션 목록과 세션 플랫폼 네임스페이스의 파드 집합이 호출
  전과 같다.

### AC3: 비파괴 추가 작업 표기
- **설명**: `tools/list`에서 이 도구가 `readOnlyHint=false`, `destructiveHint=false`,
  `idempotentHint=false`로 광고된다. 상태를 바꾸지만 기존 것을 지우거나 덮지 않으며, 재호출이
  새 세션을 하나 더 만든다는 사실이 어노테이션에 드러난다.
- **달성 가치**: V3
- **검증 방법**: `tools/list` 응답의 세 어노테이션이 위 값과 같다.

### AC4: 미설정 시 graceful 거부
- **설명**: `SESSION_PLATFORM_ENDPOINT`가 없으면 unavailable 류 에러를 반환하며, 서버
  기동·다른 도구에는 영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
