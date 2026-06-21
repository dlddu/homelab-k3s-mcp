# homelab-k3s-mcp 문서 체계 상태 추적

이 문서는 homelab-k3s-mcp 문서 체계의 **현재 상태**를 기록·추적·진단한다.
가치 → PRD → Acceptance Criteria → 테스트의 연결이 끊긴 곳이 없는지 확인하며,
**문서를 생성·수정할 때마다 함께 갱신한다.**

```
[제품 가치] ← 참조 ← [가치 문서(최상위)]
     ↑
     └── 달성 ←── [Acceptance Criteria] ←── 포함 ←── [PRD]
                        ↑
                        └── 검증 ←── [테스트 문서]
```

## 현재 상태 요약

- 정의된 가치: **3개** (V1~V3)
- PRD: **12개** (도구 11 + 공통 기반 1)
- Acceptance Criteria: **36개** (가치 연결됨: 36 / 미연결: 0)
- 테스트 문서: **0개** (AC 커버됨: 0 / 미커버: 36)
- **건강 상태**: 🟢 가치·PRD·AC 계층 건강 / 🟢 검증 커버리지 0% (테스트 미착수 — Phase 3)

> PRD는 **도구 단위**로 작성되며, 특정 도구에 속하지 않는 인증·RBAC·하드닝 등 공통 요구사항은
> `prd-platform-auth-safety.md` 한 곳에 모았다. 가치 → PRD → AC 연결은 모두 건강하고, 남은
> 작업은 각 AC를 검증하는 테스트 문서(Phase 3)다.

## 문서 인벤토리

| 종류 | 파일 |
|------|------|
| 가치 문서 | `values.md` |
| PRD (도구) | `prd-ping.md`, `prd-namespace-list.md`, `prd-workload-list.md`, `prd-workload-logs.md`, `prd-pod-describe.md`, `prd-workload-restart.md`, `prd-workload-scale.md`, `prd-dear-baby-reset-user.md`, `prd-github-app-installation-token.md`, `prd-grafana-token.md`, `prd-aws-config-get.md` |
| PRD (공통) | `prd-platform-auth-safety.md` |
| 상태 추적 | `doc-tracker.md` |

## PRD ↔ 가치 ↔ AC 매트릭스

| PRD (도구) | 달성 가치 | AC 수 | 테스트 | 상태 |
|------------|-----------|:----:|:------:|------|
| ping | V3 | 1 | - | ⚠️ 테스트 없음 |
| namespace_list | V1 | 1 | - | ⚠️ 테스트 없음 |
| workload_list | V1 | 2 | - | ⚠️ 테스트 없음 |
| workload_logs | V1 | 4 | - | ⚠️ 테스트 없음 |
| pod_describe | V1, V3 | 3 | - | ⚠️ 테스트 없음 |
| workload_restart | V1, V3 | 2 | - | ⚠️ 테스트 없음 |
| workload_scale | V1, V3 | 3 | - | ⚠️ 테스트 없음 |
| dear_baby_reset_user | V1, V3 | 3 | - | ⚠️ 테스트 없음 |
| github_app_installation_token | V2, V3 | 4 | - | ⚠️ 테스트 없음 |
| grafana_token | V2, V3 | 4 | - | ⚠️ 테스트 없음 |
| aws_config_get | V2, V3 | 3 | - | ⚠️ 테스트 없음 |
| platform (인증·안전 공통) | V3 | 6 | - | ⚠️ 테스트 없음 |

## 가치 커버리지

| 가치 | 이 가치를 달성하는 PRD |
|------|------------------------|
| V1: 자연어로 클러스터 운영 | namespace_list, workload_list, workload_logs, pod_describe, workload_restart, workload_scale, dear_baby_reset_user |
| V2: 단명·최소권한 자격증명 | github_app_installation_token, grafana_token, aws_config_get |
| V3: 안전한 운영(Safe-by-default) | platform(인증·안전), ping, pod_describe, workload_restart, workload_scale, dear_baby_reset_user, github_app_installation_token, grafana_token, aws_config_get |

## 위험 진단

### 고아 가치 (소유자 없는 가치)
- (없음) — 모든 가치의 소유자는 "홈랩 운영자"

### 미정렬 문서 (가치 참조 없는 문서)
- (없음) — 12개 PRD 모두 달성 가치를 명시

### 무가치 PRD (가치를 달성하지 않는 PRD)
- (없음)

### AC 없는 PRD
- (없음) — 12개 PRD 모두 AC 보유

### 미연결 AC (가치와 연결되지 않은 AC)
- (없음) — 36개 AC 모두 하나 이상의 가치에 연결

### 미검증 AC (테스트 없는 AC)
- 🟢 **36개 전부** — Phase 3(테스트 문서 작성) 대상. 구조적 결함이 아니라 다음 단계 작업.

### 고아 테스트 (AC를 참조하지 않는 테스트)
- (없음) — 테스트 문서 없음

### 다음 단계 메모
- 🟢 도구 단위 PRD에 맞춰 테스트 문서도 도구 단위로 작성 가능. 레포의 통합 테스트
  (`tests/integration/`: workload·github_app·grafana·aws_config·dear_baby·smoke)가 다수 AC를
  이미 커버하므로, 테스트 문서에서 해당 자동화와 AC를 매핑하면 효율적이다.

## 변경 이력

| 시점 | 변경 내용 | 이전 상태 | 이후 상태 |
|------|-----------|-----------|-----------|
| 2026-06-19 | 가치 문서 생성, V1~V3 정의, 소유자 지정 | (없음) | 가치 3 / PRD 0 / AC 0 / 테스트 0 |
| 2026-06-19 | 가치별 PRD 3종 작성(AC 18) | 가치 3 / PRD 0 | 가치 3 / PRD 3 / AC 18 / 테스트 0 |
| 2026-06-19 | PRD를 **도구 단위**로 재구성(가치별 3종 → 도구 11 + 공통 1), AC 36 | 가치 3 / PRD 3 / AC 18 | 가치 3 / PRD 12 / AC 36 / 테스트 0 |
