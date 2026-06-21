# 테스트 문서: pod_describe

## 검증 대상 AC
- AC1: 파드 상세 스냅샷 (PRD: pod_describe)
- AC2: 대상 지정 방식 (PRD: pod_describe)
- AC3: 이벤트 best-effort (PRD: pod_describe)

## 테스트 시나리오

### 시나리오 1: 구조화된 스냅샷
- **사전 조건**: 대상 파드 존재
- **실행 단계**: `pod_describe` 호출 (namespace + name)
- **기대 결과**: 메타데이터·컨테이너 상태(state/reason/restart count/exit code)·conditions·
  이벤트를 포함한 스냅샷 반환
- **검증 AC**: AC1
- **자동화**: Go 단위 `mcp_test.go::TestPodDescribeRendersStructuredPayload`.

### 시나리오 2: 대상 지정과 상호배타
- **사전 조건**: 동일
- **실행 단계**: name / selector / workload_kind+workload_name 각각으로 호출, 그리고 둘 이상
  동시 지정·부분 지정·미지정으로 호출
- **기대 결과**: 각 단일 지정은 올바른 파드로 해석(selector/workload는 첫 Running 우선),
  복수/부분/미지정은 거부
- **검증 AC**: AC2
- **자동화**: Go 단위 `mcp_test.go::TestPodDescribeAcceptsSelectorTarget`,
  `TestPodDescribeAcceptsWorkloadTarget`, `TestPodDescribeRejectsMutuallyExclusiveTargets`,
  `TestPodDescribeRejectsPartialWorkloadTarget`, `TestPodDescribeRequiresTarget`.

### 시나리오 3: 이벤트 best-effort / 에러 처리
- **사전 조건**: 이벤트 미노출 상황 / apiserver 에러 상황
- **실행 단계**: `pod_describe` 호출
- **기대 결과**: 이벤트 없으면 빈 이벤트로 정상 반환, k8s 에러는 도구 에러로 표면화
- **검증 AC**: AC3
- **자동화**: Go 단위 `mcp_test.go::TestPodDescribeNoEventsPlaceholder`,
  `TestPodDescribeSurfacesK8sErrorAsToolError`.
