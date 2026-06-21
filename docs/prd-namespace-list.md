# PRD: namespace_list

클러스터의 네임스페이스를 조회하는 도구.

## 달성 가치
- **V1: 자연어로 클러스터 운영** — `kubectl get ns` 없이 네임스페이스 현황을 파악한다.

## 도구 개요
- 입력: 없음
- 어노테이션: `readOnlyHint=true`, `idempotentHint=true`

## Acceptance Criteria

### AC1: 네임스페이스 열거
- **설명**: 클러스터의 모든 네임스페이스를 이름·phase(Active/Terminating)·생성 시각과 함께
  반환한다.
- **달성 가치**: V1
- **검증 방법**: 네임스페이스가 존재하는 클러스터에서 각 항목에 이름·phase·생성 시각이
  포함된 목록을 반환한다.
