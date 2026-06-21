# PRD: ping

서버 가용성을 확인하는 헬스체크 도구.

## 달성 가치
- **V3: 안전한 운영(Safe-by-default)** — 서버가 응답 가능한 상태인지 빠르게 확인하는
  최소 가용성 신호를 제공한다.

## 도구 개요
- 입력: 없음
- 어노테이션: `readOnlyHint=true`, `idempotentHint=true`, `openWorldHint=false`

## Acceptance Criteria

### AC1: 항상 pong 응답
- **설명**: 인자 없이 호출하면 항상 성공 결과 `pong`을 반환한다.
- **달성 가치**: V3
- **검증 방법**: 호출 시 에러 없이 `pong` 텍스트가 반환된다.
