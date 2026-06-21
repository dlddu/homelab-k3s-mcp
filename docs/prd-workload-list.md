# PRD: workload_list

Deployment·StatefulSet·DaemonSet 워크로드를 조회하는 도구.

## 달성 가치
- **V1: 자연어로 클러스터 운영** — 워크로드 현황과 레플리카 상태를 자연어로 조회한다.

## 도구 개요
- 입력: `kind`(필수, enum: Deployment/StatefulSet/DaemonSet), `namespace`(선택; 생략 시 전체 네임스페이스)
- 어노테이션: `readOnlyHint=true`, `idempotentHint=true`

## Acceptance Criteria

### AC1: 종류별 워크로드 조회
- **설명**: `kind`로 지정한 종류의 워크로드를 레플리카 수(desired/ready 등) 요약과 함께
  반환한다.
- **달성 가치**: V1
- **검증 방법**: 각 enum 종류에 대해 해당 종류의 워크로드 목록과 레플리카 요약을 반환한다.

### AC2: 네임스페이스 스코프
- **설명**: `namespace`를 지정하면 해당 네임스페이스로 한정하고, 생략하면 전체 네임스페이스를
  대상으로 한다.
- **달성 가치**: V1
- **검증 방법**: namespace 지정/생략 호출이 각각 해당 범위의 워크로드만 반환한다.
