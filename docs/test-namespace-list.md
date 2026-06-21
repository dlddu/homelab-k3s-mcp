# 테스트 문서: namespace_list

## 검증 대상 AC
- AC1: 네임스페이스 열거 (PRD: namespace_list)

## 테스트 시나리오

### 시나리오 1: 네임스페이스 목록·phase·생성 시각
- **사전 조건**: kind 클러스터에 `workload-test` 등 네임스페이스 존재
- **실행 단계**: 인자 없이 `namespace_list` 호출
- **기대 결과**: 항목에 이름·phase·생성 시각 포함, `workload-test`는 phase=Active,
  `kube-system` 포함
- **검증 AC**: AC1
- **자동화**: Go 단위 `mcp_test.go::TestNamespaceListDispatchesToService`. 통합
  `workload.py`(namespace_list 블록: workload-test=Active, kube-system 존재).

### 시나리오 2: 통합 미설정 시 도구 에러
- **사전 조건**: k8s 통합 미구성
- **실행 단계**: `namespace_list` 호출
- **기대 결과**: 서버는 정상, 호출만 도구 에러 반환
- **검증 AC**: AC1 (degradation 동작)
- **자동화**: Go 단위 `mcp_test.go::TestNamespaceListUnavailableIsToolError`.
