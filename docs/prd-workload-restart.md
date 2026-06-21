# PRD: workload_restart

워크로드를 무중단 롤링 재시작하는 도구.

## 달성 가치
- **V1: 자연어로 클러스터 운영** — 재배포 없이 워크로드를 재시작한다.
- **V3: 안전한 운영(Safe-by-default)** — 파괴적 작업임을 명시하고 비파괴적 patch로 수행한다.

## 도구 개요
- 입력: `kind`(필수, enum: Deployment/StatefulSet/DaemonSet), `namespace`(필수), `name`(필수)
- 어노테이션: `readOnlyHint=false`, `destructiveHint=true`, `idempotentHint=false`

## Acceptance Criteria

### AC1: 롤링 재시작 트리거
- **설명**: 재시작 트리거(restartedAt 어노테이션) patch로 워크로드 롤아웃을 새로 시작한다.
  재생성/삭제를 사용하지 않는다.
- **달성 가치**: V1
- **검증 방법**: 호출 후 새 롤아웃이 시작되며, 사용된 동작이 delete가 아닌 patch이다.

### AC2: 파괴적 작업 표기
- **설명**: `tools/list`에서 이 도구가 `destructiveHint=true`로 광고된다.
- **달성 가치**: V3
- **검증 방법**: `tools/list` 응답의 어노테이션이 destructiveHint=true이다.
