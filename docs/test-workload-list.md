# 테스트 문서: workload_list

## 검증 대상 AC
- AC1: 종류별 워크로드 조회 (PRD: workload_list)
- AC2: 네임스페이스 스코프 (PRD: workload_list)

## 테스트 시나리오

### 시나리오 1: 종류 지정 조회
- **사전 조건**: `workload-test` 네임스페이스에 Deployment `workload-fixture` 존재
- **실행 단계**: `workload_list` 호출 (kind=Deployment, namespace=workload-test)
- **기대 결과**: payload에 kind/namespace 반영, items에 `workload-fixture` 포함
- **검증 AC**: AC1, AC2
- **자동화**: Go 단위 `mcp_test.go::TestWorkloadListDispatchesToService`. 통합
  `workload.py`(namespace 지정 블록).

### 시나리오 2: 전체 네임스페이스 조회
- **사전 조건**: 위와 동일 + 다른 네임스페이스에도 워크로드 존재
- **실행 단계**: `workload_list` 호출 (kind=Deployment, namespace 생략)
- **기대 결과**: namespace=None, 여러 네임스페이스의 워크로드 반환(예: (workload-test,
  workload-fixture), (homelab-k3s-mcp, homelab-k3s-mcp))
- **검증 AC**: AC2
- **자동화**: Go 단위 `mcp_test.go::TestWorkloadListWithoutNamespaceListsAll`. 통합
  `workload.py`(all-namespaces 블록).
