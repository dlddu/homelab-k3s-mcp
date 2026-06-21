# PRD: dear_baby_reset_user

dear-baby 백엔드 파드에서 특정 사용자의 온보딩을 리셋하는 앱별 운영 도구.

## 달성 가치
- **V1: 자연어로 클러스터 운영** — 파드 조회 → exec → CLI 실행의 다단계 수작업을 단일 호출로
  캡슐화한다.
- **V3: 안전한 운영(Safe-by-default)** — 파괴적 작업 표기, 명시적 이메일 인자를 강제한다.

## 도구 개요
- 입력: `namespace`(필수), `email`(필수), `selector`(기본 `app=dear-baby`),
  `container`(기본 `backend`)
- 동작: 셀렉터로 백엔드 파드를 찾아 번들된 `/reset-user` CLI를 exec
- 어노테이션: `readOnlyHint=false`, `destructiveHint=true`, `idempotentHint=true`

## Acceptance Criteria

### AC1: 온보딩 리셋 실행
- **설명**: 대상 파드에서 `/reset-user`를 exec하여 지정 이메일 사용자의 온보딩 필드
  (onboarded_at, due_date, voice coachmark dismissal, first_record_at, ai_preview)를 초기화한다.
  사용자의 레코드 자체는 보존된다.
- **달성 가치**: V1
- **검증 방법**: 유효 이메일로 호출 시 해당 필드가 초기화되고 레코드는 유지되며 성공 결과를
  반환한다.

### AC2: 명시적 대상 지정
- **설명**: `email`이 필수이며 없으면 거부된다. `selector`·`container`는 기본값
  (`app=dear-baby`/`backend`)을 사용하되 재정의할 수 있다.
- **달성 가치**: V3
- **검증 방법**: email 누락 호출이 거부되고, 기본 셀렉터/컨테이너로 대상 파드가 해석된다.

### AC3: 파괴적 작업 표기
- **설명**: `tools/list`에서 `destructiveHint=true`로 광고된다.
- **달성 가치**: V3
- **검증 방법**: `tools/list` 응답의 어노테이션이 destructiveHint=true이다.
