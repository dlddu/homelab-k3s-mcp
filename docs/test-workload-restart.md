# 테스트 문서: workload_restart

## 검증 대상 AC
- AC1: 롤링 재시작 트리거 (PRD: workload_restart)
- AC2: 파괴적 작업 표기 (PRD: workload_restart)

## 테스트 시나리오

### 시나리오 1: patch 기반 롤링 재시작
- **사전 조건**: `workload-test`의 Deployment `workload-fixture` 실행 중
- **실행 단계**: `workload_restart` 호출 (kind/namespace/name), 이후 롤아웃 완료 대기
- **기대 결과**: 응답에 비어 있지 않은 restartedAt, 리소스에 restartedAt 어노테이션이 patch로
  설정되고 롤아웃이 새로 시작됨(delete 미사용). ns/name 누락 시 거부.
- **검증 AC**: AC1
- **자동화**: Go 단위 `mcp_test.go::TestWorkloadRestartDispatchesToService`,
  `TestWorkloadRestartRequiresNamespaceAndName`. 통합 `workload.py`(restartedAt + kubectl
  annotation 확인 + rollout status).

### 시나리오 2: 파괴적 어노테이션 광고
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 조회
- **기대 결과**: `workload_restart`가 `destructiveHint=true`로 광고됨
- **검증 AC**: AC2
- **자동화**: Go 단위 `mcp_test.go::TestToolsListAdvertisesAnnotations`.
