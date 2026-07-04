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

- 정의된 가치: **4개** (V1~V4)
- PRD: **15개** (도구 14 + 공통 기반 1)
- Acceptance Criteria: **52개** (가치 연결됨: 52 / 미연결: 0)
- 테스트 문서: **15개** (AC 커버됨: 52 / 미커버: 0)
- **건강 상태**: 🟢 **건강함** — 가치 → PRD → AC → 테스트 전 계층 연결 완료

> 문서 체계의 모든 화살표가 연결되었다(고아 가치·미정렬 문서·무가치 PRD·AC 없는 PRD·
> 미연결 AC·미검증 AC·고아 테스트 없음). 별도로, 테스트 문서가 참조하는 **자동화의 실제
> 커버리지**는 아래 "자동화 커버리지"에 정리한다(문서 구조와 별개의 일부 공백 존재).

## 문서 인벤토리

| 종류 | 파일 |
|------|------|
| 가치 문서 | `values.md` |
| PRD (도구) | `prd-ping.md`, `prd-namespace-list.md`, `prd-workload-list.md`, `prd-workload-logs.md`, `prd-pod-describe.md`, `prd-workload-restart.md`, `prd-workload-scale.md`, `prd-dear-baby-reset-user.md`, `prd-github-app-installation-token.md`, `prd-grafana-token.md`, `prd-aws-config-get.md`, `prd-opensearch-search.md`, `prd-opensearch-document-put.md`, `prd-opensearch-document-delete.md` |
| PRD (공통) | `prd-platform-auth-safety.md` |
| 테스트 문서 | 각 PRD에 대응하는 `test-*.md` (15개) |
| 상태 추적 | `doc-tracker.md` |

## PRD ↔ 가치 ↔ AC ↔ 테스트 매트릭스

| PRD (도구) | 달성 가치 | AC 수 | 테스트 문서 | 상태 |
|------------|-----------|:----:|--------------|------|
| ping | V3 | 1 | test-ping | ✅ 완전 |
| namespace_list | V1 | 1 | test-namespace-list | ✅ 완전 |
| workload_list | V1 | 2 | test-workload-list | ✅ 완전 |
| workload_logs | V1 | 4 | test-workload-logs | ✅ 완전 |
| pod_describe | V1, V3 | 3 | test-pod-describe | ✅ 완전 |
| workload_restart | V1, V3 | 2 | test-workload-restart | ✅ 완전 |
| workload_scale | V1, V3 | 3 | test-workload-scale | ✅ 완전 |
| dear_baby_reset_user | V1, V3 | 3 | test-dear-baby-reset-user | ✅ 완전 |
| github_app_installation_token | V2, V3 | 4 | test-github-app-installation-token | ✅ 완전 |
| grafana_token | V2, V3 | 4 | test-grafana-token | ✅ 완전 |
| aws_config_get | V2, V3 | 3 | test-aws-config-get | ✅ 완전 |
| opensearch_search | V4, V2, V3 | 4 | test-opensearch-search | ✅ 완전 |
| opensearch_document_put | V4, V2, V3 | 5 | test-opensearch-document-put | ✅ 완전 |
| opensearch_document_delete | V4, V2, V3 | 5 | test-opensearch-document-delete | ✅ 완전 |
| platform (인증·안전 공통) | V3 | 8 | test-platform-auth-safety | ✅ 완전 |

## 가치 커버리지

| 가치 | 이 가치를 달성하는 PRD |
|------|------------------------|
| V1: 자연어로 클러스터 운영 | namespace_list, workload_list, workload_logs, pod_describe, workload_restart, workload_scale, dear_baby_reset_user |
| V2: 단명·최소권한 자격증명 | github_app_installation_token, grafana_token, aws_config_get, opensearch_search, opensearch_document_put, opensearch_document_delete |
| V3: 안전한 운영(Safe-by-default) | platform(인증·안전), ping, pod_describe, workload_restart, workload_scale, dear_baby_reset_user, github_app_installation_token, grafana_token, aws_config_get, opensearch_search, opensearch_document_put, opensearch_document_delete |
| V4: 운영 지식의 축적·검색 | opensearch_search, opensearch_document_put, opensearch_document_delete |

## 위험 진단

### 고아 가치 (소유자 없는 가치)
- (없음) — 모든 가치의 소유자는 "홈랩 운영자"

### 미정렬 문서 (가치 참조 없는 문서)
- (없음)

### 무가치 PRD / AC 없는 PRD
- (없음) — 15개 PRD 모두 가치를 달성하고 AC를 보유

### 미연결 AC (가치와 연결되지 않은 AC)
- (없음) — 52개 AC 모두 가치에 연결

### 미검증 AC (테스트 없는 AC)
- (없음) — 52개 AC 모두 테스트 문서의 시나리오로 커버

### 고아 테스트 (AC를 참조하지 않는 테스트)
- (없음) — 15개 테스트 문서 모두 검증 대상 AC를 명시

## 자동화 커버리지 (문서 구조와 별개)

테스트 문서는 모든 AC를 커버하지만, 그 시나리오가 참조하는 **자동화 상태**는 세 가지로 나뉜다.

- 🟢 **자동 검증됨** (Go 단위 `internal/server/mcp_test.go`·`health_test.go`·
  `internal/auth/auth_test.go`, `internal/awsconfig`·`internal/github`·`internal/grafana`·
  `internal/opensearch` 단위 테스트 + Python 통합 `tests/integration/`):
  ping, namespace_list, workload_list, workload_logs(AC1·2·4), pod_describe(전체),
  workload_restart, workload_scale, dear_baby_reset_user, 자격증명 3종의 발급/스코프/
  비노출(github·grafana AC4)/unavailable,
  opensearch 3종 전 AC(14 — 단위 + `tests/integration/opensearch.py`,
  픽스처는 security off 단일노드 OpenSearch + MinIO STS),
  platform AC1·AC2(인증 게이트·디스커버리)·AC5·AC6.
- 🟡 **정적 검증** (매니페스트 리뷰): platform AC3(RBAC 경계 — `k8s/rbac.yaml`),
  platform AC4(하드닝 — `k8s/deployment.yaml`).
- 🔴 **자동화 공백 — 추가 권장**:
  - **platform AC7·AC8 (API 키 인증·구성 유연성)** — 구현 선행 문서. 코드 미구현 상태이며,
    구현 시 `internal/auth/auth_test.go`(키 게이트 table-driven·JWT 병행·상수시간·키 비노출·
    `FromEnv` env 게이팅)와 `internal/server`(디스커버리 조건부 제공 라우팅 테스트)로 자동
    검증 예정. 상세는 test-platform-auth-safety 시나리오 7·8 및 작업 계획 참조.
  - opensearch 3종 — **프로덕션 스모크 미수행**(env 배선이 infrastructure/flux-cd-apps
    반영에 걸려 있음). CI 자동화는 완료; 실제 `kubernetes-docs` 컬렉션 대상
    put→search→delete 확인은 배선 완료 후 수행.
  - workload_logs AC3 — 실제 previous/크래시 루프 로그 **내용** 미커버(픽스처가 pause 이미지).

## 변경 이력

| 시점 | 변경 내용 | 이전 상태 | 이후 상태 |
|------|-----------|-----------|-----------|
| 2026-06-19 | 가치 문서 생성, V1~V3 정의, 소유자 지정 | (없음) | 가치 3 / PRD 0 / AC 0 / 테스트 0 |
| 2026-06-19 | 가치별 PRD 3종 작성(AC 18) | 가치 3 / PRD 0 | 가치 3 / PRD 3 / AC 18 / 테스트 0 |
| 2026-06-19 | PRD를 도구 단위로 재구성(도구 11 + 공통 1), AC 36 | PRD 3 / AC 18 | 가치 3 / PRD 12 / AC 36 / 테스트 0 |
| 2026-06-19 | workload_logs AC2 정정(초과 시 클램프 → 거부), 테스트 문서 12종 작성 | 테스트 0 | 가치 3 / PRD 12 / AC 36 / 테스트 12 (전 계층 연결) |
| 2026-06-22 | platform AC1·AC2 인증 게이트/디스커버리 단위 테스트 추가(`internal/auth/auth_test.go`) | AC1·AC2 자동화 공백 | platform AC1·AC2 자동 검증(자동화 공백 7→5) |
| 2026-06-22 | github·grafana AC4 베이스 시크릿 비노출 단위 테스트 추가(`internal/github`·`internal/grafana`) | github·grafana AC4 자동화 공백 | github·grafana AC4 자동 검증(잔여 공백: workload_logs AC3 1건) |
| 2026-07-02 | V4(운영 지식의 축적·검색) 추가, OpenSearch Serverless 도구 3종 PRD(AC 14)·테스트 문서 작성 — 구현 선행 문서(인프라 `kubernetes-docs` 컬렉션·권한은 부여 완료, 코드 미구현) | 가치 3 / PRD 12 / AC 36 / 테스트 12 | 가치 4 / PRD 15 / AC 50 / 테스트 15 |
| 2026-07-02 | OpenSearch 도구 3종 구현(`internal/opensearch` + 도구 표면 + CI 통합 테스트), 테스트 문서 자동화 필드를 실제 테스트 경로로 갱신 | opensearch 14 AC 자동화 공백(도구 미구현) | opensearch 14 AC 자동 검증(프로덕션 스모크만 잔여 — env 배선 후) |
| 2026-07-04 | platform PRD에 API 키 인증 AC7·AC8 추가(비대화형 자동화용, 구현 선행 문서), values V3 서술 확장, 테스트 시나리오 7·8 추가. 위험 진단 수치 정합성 보정(PRD 15/AC 52/테스트 15) | 가치 4 / PRD 15 / AC 50 / 테스트 15 | 가치 4 / PRD 15 / AC 52 / 테스트 15 (전 계층 연결, AC7·AC8만 자동화 공백) |
