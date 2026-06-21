# PRD: workload_scale

워크로드의 레플리카 수를 조정하는 도구.

## 달성 가치
- **V1: 자연어로 클러스터 운영** — 레플리카 수를 자연어로 조정한다.
- **V3: 안전한 운영(Safe-by-default)** — 파괴적 작업 표기, 레플리카가 없는 종류는 거부한다.

## 도구 개요
- 입력: `kind`(필수, enum: **Deployment/StatefulSet**), `namespace`(필수), `name`(필수),
  `replicas`(필수, 정수 ≥ 0)
- 어노테이션: `readOnlyHint=false`, `destructiveHint=true`, `idempotentHint=true`

## Acceptance Criteria

### AC1: 레플리카 설정
- **설명**: `spec.replicas`를 지정한 값으로 설정한다. 0으로의 스케일다운도 허용한다.
- **달성 가치**: V1
- **검증 방법**: 지정 레플리카 수가 워크로드에 반영되고, replicas=0도 정상 적용된다.

### AC2: DaemonSet 거부
- **설명**: DaemonSet은 레플리카 개념이 없으므로 입력 enum에서 제외되며, 요청 시 거부된다.
- **달성 가치**: V3
- **검증 방법**: kind=DaemonSet 요청이 거부된다.

### AC3: 파괴적 작업 표기
- **설명**: `tools/list`에서 `destructiveHint=true`로 광고된다.
- **달성 가치**: V3
- **검증 방법**: `tools/list` 응답의 어노테이션이 destructiveHint=true이다.
