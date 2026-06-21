# PRD: pod_describe

단일 파드의 kubectl describe 형식 스냅샷을 반환하는 도구.

## 달성 가치
- **V1: 자연어로 클러스터 운영** — 파드 상태·재시작·이벤트를 한눈에 확인해 진단한다.

## 도구 개요
- 입력: `namespace`(필수) + 대상 지정 방식 중 **정확히 하나**:
  `name`(정확한 파드 이름) / `selector`(라벨 셀렉터) / `workload_kind`+`workload_name`
- 어노테이션: `readOnlyHint=true`, `idempotentHint=true`

## Acceptance Criteria

### AC1: 파드 상세 스냅샷
- **설명**: 메타데이터, 컨테이너 상태(state·reason·restart count·exit code), conditions,
  최근 이벤트를 포함한 스냅샷을 반환한다.
- **달성 가치**: V1
- **검증 방법**: 대상 파드에 대해 컨테이너별 상태/재시작 횟수/직전 종료 정보와 이벤트 섹션이
  포함된다.

### AC2: 대상 지정 방식
- **설명**: `name` / `selector` / `workload_kind`+`workload_name` 중 정확히 하나로 파드를
  해석한다. selector·workload 경로는 첫 Running 파드를 우선한다.
- **달성 가치**: V1
- **검증 방법**: 각 지정 방식이 올바른 파드로 해석되고, 둘 이상을 동시에 주면 거부된다.

### AC3: 이벤트 best-effort
- **설명**: 이벤트 조회 권한이 없으면 빈 이벤트로 동작하며 실패하지 않는다.
- **달성 가치**: V1, V3
- **검증 방법**: 이벤트 권한이 없는 상황에서도 스냅샷이 (빈 이벤트로) 정상 반환된다.
