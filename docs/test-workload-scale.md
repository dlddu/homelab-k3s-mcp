# 테스트 문서: workload_scale

## 검증 대상 AC
- AC1: 레플리카 설정 (PRD: workload_scale)
- AC2: DaemonSet 거부 (PRD: workload_scale)
- AC3: 파괴적 작업 표기 (PRD: workload_scale)

## 테스트 시나리오

### 시나리오 1: 레플리카 설정(0 포함)
- **사전 조건**: Deployment `workload-fixture` 실행 중
- **실행 단계**: replicas=3 → 1 순으로 호출, 각 단계 후 spec.replicas 확인
- **기대 결과**: 지정 값이 spec.replicas에 반영(3, 1), replicas=0도 허용. 음수/누락은 거부.
- **검증 AC**: AC1
- **자동화**: Go 단위 `mcp_test.go::TestWorkloadScaleDispatchesToService`,
  `TestWorkloadScaleSupportsZeroReplicas`, `TestWorkloadScaleRejectsNegativeReplicas`,
  `TestWorkloadScaleRequiresReplicas`. 통합 `workload.py`(scale up 3 / down 1 + kubectl 확인).

### 시나리오 2: DaemonSet/미지원 종류 거부
- **사전 조건**: 동일
- **실행 단계**: kind=DaemonSet으로 `workload_scale` 호출
- **기대 결과**: 도구 에러, 메시지 "DaemonSet does not have replicas"
- **검증 AC**: AC2
- **자동화**: Go 단위 `mcp_test.go::TestWorkloadRejectsUnknownKind`. 통합 `workload.py`
  (DaemonSet rejection).

### 시나리오 3: 파괴적 어노테이션 광고
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 조회
- **기대 결과**: `workload_scale`이 `destructiveHint=true`로 광고됨
- **검증 AC**: AC3
- **자동화**: Go 단위 `mcp_test.go::TestToolsListAdvertisesWorkloadScale`,
  `TestToolsListAdvertisesAnnotations`.
