# PRD: session_list

session-platform 제어면(control plane)이 보유한 세션 목록을 조회하는 도구.
`session_read`·`session_write`가 대상으로 삼을 세션 id를 얻는 진입점이다.

## 달성 가치
- **V5: 클러스터 내부 앱 기능의 도구화** — 클러스터 내부에서만 닿는 제어면 API
  (`GET /api/v1/sessions`)를 인그레스 공개나 포트포워딩 없이 도구 한 개로 연다.
- **V3: 안전한 운영(Safe-by-default)** — 세션 상태를 바꾸지 않는 순수 조회이며, 미설정 시
  graceful 거부가 기본값이다.

## 도구 개요
- 입력: 없음
- 동작: 제어면의 `GET /api/v1/sessions`를 호출해 세션 목록을 반환한다. 세션별로 `id`,
  `name`, `workloadType`(`shell` | `claude-code`), `state`(`active` | `idle` | `snapshot`),
  `pod`, `createdAt`, `lastAccess`를 노출한다.
- 서버 요구 설정: `SESSION_PLATFORM_ENDPOINT` (제어면 베이스 URL. 운영 기준 클러스터 내부
  주소 `http://control-plane.session-platform.svc.cluster.local` — 세션 도구 3종 공유)
- 어노테이션: `readOnlyHint=true`, `destructiveHint=false`, `idempotentHint=true`,
  `openWorldHint=true`

> **구현 상태**: 이 PRD는 구현에 선행해 작성됐다(2026-08-12 — 그 시점에는 session-platform
> 배포가 클러스터에서 제거돼 있었고 도구도 없었다). 2026-09-03 기준 제어면은 재배포됐고
> (`session-platform` 네임스페이스, Service `control-plane`), `SESSION_PLATFORM_ENDPOINT`
> 배선과 도구 구현(`internal/sessionplatform` + `internal/mcp`)이 끝났다. 자동화 현황은
> `doc-tracker.md`가 SSOT다.

## Acceptance Criteria

### AC1: 세션 열거
- **설명**: 제어면이 보유한 모든 세션을 반환하며, 각 세션의 id·이름·워크로드 타입·상태·
  최종 접근 시각을 포함한다. 세션이 없으면 빈 목록을 반환하고 에러로 취급하지 않는다.
- **달성 가치**: V5
- **검증 방법**: 세션 2개(서로 다른 상태)를 만든 뒤 호출하면 두 세션이 각각의 상태와 함께
  열거되고, 세션이 없는 제어면에서는 빈 목록이 반환된다.

### AC2: 상태를 바꾸지 않는 조회
- **설명**: 목록 조회는 어떤 세션도 `active`로 승격하지 않고, 스냅샷을 복원하지 않으며,
  `lastAccess`를 갱신하지 않는다. `read`·`write`의 "접근=active화" 규칙(AC-C2/C3)과 달리
  이 도구는 수동적 조회이므로, 유휴 세션 목록을 확인하는 것만으로 컴퓨트가 되살아나지
  않는다.
- **달성 가치**: V3
- **검증 방법**: `snapshot` 세션이 포함된 상태에서 목록을 반복 조회한 뒤, 각 세션의
  `state`와 `lastAccess`가 호출 전후로 동일하고 새 파드가 기동되지 않음을 확인한다.

### AC3: 미설정 시 graceful 거부
- **설명**: `SESSION_PLATFORM_ENDPOINT`가 없으면 unavailable 류 에러를 반환하며, 서버
  기동·다른 도구에는 영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
