# 테스트 문서: workload_logs

## 검증 대상 AC
- AC1: 워크로드 기준 로그 조회 (PRD: workload_logs)
- AC2: tail 라인 제어 (PRD: workload_logs)
- AC3: 크래시 루프 후 직전 로그 (PRD: workload_logs)
- AC4: 컨테이너 선택과 필터 (PRD: workload_logs)

## 테스트 시나리오

### 시나리오 1: 셀렉터 해석 후 파드 로그 반환
- **사전 조건**: `workload-test`의 Deployment `workload-fixture` 실행 중
- **실행 단계**: `workload_logs` 호출 (kind/namespace/name)
- **기대 결과**: 워크로드 셀렉터가 해석되어 대상 파드(`workload-fixture-*`)의 로그 반환.
  존재하지 않는 워크로드는 도구 에러.
- **검증 AC**: AC1
- **자동화**: Go 단위 `mcp_test.go::TestWorkloadLogsDispatchesWithDefaults`,
  `TestWorkloadLogsRequiresNamespaceAndName`. 통합 `workload.py`(defaults / missing-workload).

### 시나리오 2: 기본 200, 초과 시 거부
- **사전 조건**: 동일
- **실행 단계**: (a) tail_lines 생략 호출, (b) tail_lines=999999 호출
- **기대 결과**: (a) tailLines=200으로 동작, (b) `tail_lines` 관련 거부 에러(클램프하지 않음)
- **검증 AC**: AC2
- **자동화**: Go 단위 `mcp_test.go::TestWorkloadLogsRejectsTailLinesOverMax`,
  `TestWorkloadLogsEmptyOutputPlaceholder`. 통합 `workload.py`(defaults / over-max 거부).

### 시나리오 3: 직전 컨테이너 로그
- **사전 조건**: 재시작(크래시) 이력이 있는 파드
- **실행 단계**: `previous=true`로 호출
- **기대 결과**: 종료된 직전 인스턴스의 로그 반환(Running 파드 없어도 매칭 파드 사용)
- **검증 AC**: AC3
- **자동화**: 옵션 전달은 Go 단위 `mcp_test.go::TestWorkloadLogsHonoursOverrides`로 검증.
  실제 previous 로그 **내용**은 e2e `tests/integration/workload.py`가 크래시 루프 픽스처
  (`crashloop-fixture` — busybox가 마커 라인 출력 후 exit 1, restartCount ≥ 1 대기)로 검증.

### 시나리오 4: 컨테이너 지정·출력 옵션
- **사전 조건**: 동일
- **실행 단계**: container/tail_lines/timestamps/since_seconds 지정 호출
- **기대 결과**: 각 옵션이 payload에 반영(container, tailLines, timestamps, sinceSeconds)
- **검증 AC**: AC4
- **자동화**: Go 단위 `mcp_test.go::TestWorkloadLogsHonoursOverrides`. 통합
  `workload.py`(explicit options).
