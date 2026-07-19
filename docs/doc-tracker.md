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
  ping, namespace_list, workload_list, workload_logs(전체 — AC3 크래시 루프 previous
  내용은 e2e `crashloop-fixture`), pod_describe(전체),
  workload_restart, workload_scale, dear_baby_reset_user, 자격증명 3종의 발급/스코프/
  비노출(github·grafana AC4)/unavailable,
  opensearch 3종 전 AC(14 — 단위 + `tests/integration/opensearch.py`,
  픽스처는 security off 단일노드 OpenSearch + MinIO STS),
  platform AC1·AC2(인증 게이트·디스커버리)·AC5·AC6·AC7·AC8(API 키 게이트·구성 유연성·
  디스커버리 조건부 — `internal/auth/auth_test.go`·`internal/server/auth_routing_test.go`).
- 🟡 **정적 검증** (매니페스트 리뷰): platform AC3(RBAC 경계 — `k8s/rbac.yaml`),
  platform AC4(하드닝 — `k8s/deployment.yaml`).
- 🔴 **자동화 공백 — 추가 권장**:
  - opensearch 3종 — **프로덕션 스모크 미수행**(env 배선이 infrastructure/flux-cd-apps
    반영에 걸려 있음). CI 자동화는 완료; 실제 `kubernetes-docs` 컬렉션 대상
    put→search→delete 확인은 배선 완료 후 수행.

## AC ↔ e2e 1:1 정합성 (reconciler 렌즈)

> **렌즈 차이**: reconciler 정합성 모델(`tbm_homelab-k3s-mcp-ac-e2e`)은 **`tests/integration/`의 통합 e2e만** 검증으로 인정한다 — `internal/`의 Go 단위 테스트는 정의상 e2e가 아니다. 따라서 위 "자동화 커버리지"에서 🟢로 세는 다수 AC가 이 e2e 렌즈에서는 **e2e 공백**으로 계수된다. 이 섹션은 그 e2e-전용 렌즈의 레지스트리다.

### 케이스 식별 규약 (규칙 1·2·3)

- **규칙 1 (AC→e2e)**: 예외 목록에 없는 모든 AC는 자신을 검증하는 e2e 케이스를 **정확히 하나** 가진다.
- **규칙 2 (e2e→AC)**: 모든 e2e 케이스는 **정확히 하나의 AC**를 검증 대상으로 선언한다.
- **규칙 3 (식별)**: 각 e2e 케이스는 이름+docstring으로 대상 AC를 명시한다 — 예: `def test_<domain>_ac<n>_<slug>():` + docstring 첫 줄 `AC: <domain>/ACn`. 본 레지스트리가 AC↔케이스 매핑 SSOT이며, `test-*.md` 자동화 필드는 케이스 신설 후 해당 케이스 경로를 지목한다.
- **현재 미충족(후속 리팩터)**: `tests/integration/`의 7개 파일은 도메인당 평면 스크립트로 여러 AC를 함께 실행한다(아래 표의 ✅는 파일 수준 커버). 규칙 1·2를 충족하려면 이 스크립트들을 **per-AC 케이스 함수로 분리**해야 하며, 이는 kind 클러스터 CI 검증이 필요한 후속 작업이다.

### AC 레지스트리 (52) — ✅ 통합 e2e 32 · ⬜ e2e 보강 14 · 🔧 정적/단위 검증 5 · 🚫 e2e 예외 1

| AC | 제목 | e2e 상태 |
|----|------|----------|
| aws-config-get/AC1 | 고정 객체 조회 | ✅ 통합 `aws_config.py` |
| aws-config-get/AC2 | 정적 키 미사용 | ✅ 통합 `aws_config.py` |
| aws-config-get/AC3 | 미설정 시 graceful 거부 | ⬜ 보강 필요 |
| dear-baby-reset-user/AC1 | 온보딩 리셋 실행 | ✅ 통합 `dear_baby.py` |
| dear-baby-reset-user/AC2 | 명시적 대상 지정 | ✅ 통합 `dear_baby.py` |
| dear-baby-reset-user/AC3 | 파괴적 작업 표기 | 🔧 정적/단위 검증 |
| github-app-installation-token/AC1 | 단명 설치 토큰 발급 | ✅ 통합 `github_app.py` |
| github-app-installation-token/AC2 | 스코프 제한 | ✅ 통합 `github_app.py` |
| github-app-installation-token/AC3 | 미설정 시 graceful 거부 | ⬜ 보강 필요 |
| github-app-installation-token/AC4 | 베이스 키 비노출 | ✅ 통합 `github_app.py` |
| grafana-token/AC1 | read-only 토큰 발급 | ✅ 통합 `grafana.py` |
| grafana-token/AC2 | 즉시 사용 가능한 형태 | ✅ 통합 `grafana.py` |
| grafana-token/AC3 | 미설정 시 graceful 거부 | ⬜ 보강 필요 |
| grafana-token/AC4 | 발급자 토큰 비노출 | ⬜ 보강 필요 |
| namespace-list/AC1 | 네임스페이스 열거 | ✅ 통합 `workload.py` |
| opensearch-document-delete/AC1 | 단일 문서 삭제 | ✅ 통합 `opensearch.py` |
| opensearch-document-delete/AC2 | 부재 문서의 명확한 처리 | ✅ 통합 `opensearch.py` |
| opensearch-document-delete/AC3 | 파괴적 작업 표기 | 🔧 정적/단위 검증 |
| opensearch-document-delete/AC4 | AssumeRole·SigV4 접근 | ✅ 통합 `opensearch.py` |
| opensearch-document-delete/AC5 | 미설정 시 graceful 거부 | ⬜ 보강 필요 |
| opensearch-document-put/AC1 | 문서 색인·업서트 | ✅ 통합 `opensearch.py` |
| opensearch-document-put/AC2 | 인덱스 자동 생성 | ✅ 통합 `opensearch.py` |
| opensearch-document-put/AC3 | 파괴적 작업 표기 | 🔧 정적/단위 검증 |
| opensearch-document-put/AC4 | AssumeRole·SigV4 접근 | ✅ 통합 `opensearch.py` |
| opensearch-document-put/AC5 | 미설정 시 graceful 거부 | ⬜ 보강 필요 |
| opensearch-search/AC1 | 질의 검색 | ✅ 통합 `opensearch.py` |
| opensearch-search/AC2 | 결과 상한 | ✅ 통합 `opensearch.py` |
| opensearch-search/AC3 | AssumeRole·SigV4 접근 | ✅ 통합 `opensearch.py` |
| opensearch-search/AC4 | 미설정 시 graceful 거부 | ⬜ 보강 필요 |
| ping/AC1 | 항상 pong 응답 | ✅ 통합 `smoke.py` |
| platform-auth-safety/AC1 | 인증 게이트 | ⬜ 보강 필요 |
| platform-auth-safety/AC2 | 인증 디스커버리 | ⬜ 보강 필요 |
| platform-auth-safety/AC3 | 최소권한 RBAC 경계 | ✅ 통합 `workload.py` |
| platform-auth-safety/AC4 | 하드닝된 런타임 | 🚫 e2e 예외 |
| platform-auth-safety/AC5 | 서버 수준 graceful degradation | ✅ 통합 `smoke.py` |
| platform-auth-safety/AC6 | 헬스·레디니스 | ✅ 통합 `smoke.py` |
| platform-auth-safety/AC7 | API 키 인증 | ⬜ 보강 필요 |
| platform-auth-safety/AC8 | 인증 방식 구성 유연성 | ⬜ 보강 필요 |
| pod-describe/AC1 | 파드 상세 스냅샷 | ⬜ 보강 필요 |
| pod-describe/AC2 | 대상 지정 방식 | ⬜ 보강 필요 |
| pod-describe/AC3 | 이벤트 best-effort | ⬜ 보강 필요 |
| workload-list/AC1 | 종류별 워크로드 조회 | ✅ 통합 `workload.py` |
| workload-list/AC2 | 네임스페이스 스코프 | ✅ 통합 `workload.py` |
| workload-logs/AC1 | 워크로드 기준 로그 조회 | ✅ 통합 `workload.py` |
| workload-logs/AC2 | tail 라인 제어 | ✅ 통합 `workload.py` |
| workload-logs/AC3 | 크래시 루프 후 직전 로그 | ✅ 통합 `workload.py` |
| workload-logs/AC4 | 컨테이너 선택과 필터 | ✅ 통합 `workload.py` |
| workload-restart/AC1 | 롤링 재시작 트리거 | ✅ 통합 `workload.py` |
| workload-restart/AC2 | 파괴적 작업 표기 | 🔧 정적/단위 검증 |
| workload-scale/AC1 | 레플리카 설정 | ✅ 통합 `workload.py` |
| workload-scale/AC2 | DaemonSet 거부 | ✅ 통합 `workload.py` |
| workload-scale/AC3 | 파괴적 작업 표기 | 🔧 정적/단위 검증 |

### ⬜ e2e 보강 backlog (14) — e2e 가능 클러스터 동작, 전용 케이스 신설 필요

> 새 통합 e2e는 kind 클러스터 실서버 배포로 실행되므로 앱 구동 검증이 필요 — 후속 task로 저작한다.

- **platform-auth-safety/AC1** 인증 게이트 → `tests/integration/smoke.py`: 미인증(Bearer 없음) 호출이 401로 거부되는지
- **platform-auth-safety/AC2** 인증 디스커버리 → `tests/integration/smoke.py`: 디스커버리 엔드포인트가 인증 방식을 반환하는지
- **platform-auth-safety/AC7** API 키 인증 → `tests/integration/smoke.py`: MCP_API_KEYS 배선 후 유효 키 호출 인가·무효 키 거부
- **platform-auth-safety/AC8** 인증 방식 구성 유연성 → `tests/integration/smoke.py`: env-게이팅 다중 구성 배포 변형에서 인증 방식 전환
- **pod-describe/AC1** 파드 상세 스냅샷 → `tests/integration/pod.py`: 실행 중 파드 describe → 스냅샷 필드 반환
- **pod-describe/AC2** 대상 지정 방식 → `tests/integration/pod.py`: name/selector 지정이 해석되는지
- **pod-describe/AC3** 이벤트 best-effort → `tests/integration/pod.py`: 이벤트 필드가 best-effort로 포함되는지

> **미설정 graceful 거부(6)**: kind 픽스처가 모든 시크릿을 제공하므로, 대상 시크릿만 뺀 **no-config/env-게이팅 배포 변형**을 띄워 해당 도구 호출이 크래시 없이 명확한 거부를 반환하는지 검증한다(platform-auth-safety/AC8의 env-게이팅 다중 구성 메커니즘 공유).
- **aws-config-get/AC3** 미설정 시 graceful 거부 → `tests/integration/aws_config.py`(no-config 변형): AWS 시크릿 미배선에서 조회가 graceful 거부(비크래시·명확 에러)
- **github-app-installation-token/AC3** 미설정 시 graceful 거부 → `tests/integration/github_app.py`(no-config 변형): App 자격 미배선에서 발급이 graceful 거부
- **grafana-token/AC3** 미설정 시 graceful 거부 → `tests/integration/grafana.py`(no-config 변형): Grafana 자격 미배선에서 발급이 graceful 거부
- **opensearch-document-delete/AC5** 미설정 시 graceful 거부 → `tests/integration/opensearch.py`(no-config 변형): OpenSearch 미배선에서 삭제가 graceful 거부
- **opensearch-document-put/AC5** 미설정 시 graceful 거부 → `tests/integration/opensearch.py`(no-config 변형): OpenSearch 미배선에서 색인이 graceful 거부
- **opensearch-search/AC4** 미설정 시 graceful 거부 → `tests/integration/opensearch.py`(no-config 변형): OpenSearch 미배선에서 검색이 graceful 거부
- **grafana-token/AC4** 발급자 토큰 비노출 → `tests/integration/grafana.py`: 발급 응답 본문에 발급자(원본) 토큰 문자열이 포함되지 않음을 단언(출력-내용 e2e 단정)

### 🔧 정적/단위 검증 (5) — 도구 메타데이터 속성, e2e 비대상·검증 충족

> 파괴적 작업 표기는 **도구 정의 annotations의 `destructiveHint` 속성**이다 — 클러스터 동작이 아니라 도구 스키마 속성이므로 파괴 동작을 e2e로 실행해 검증할 대상이 아니다. `tools/list`가 광고하는 annotation을 검사하는 **Go 단위 테스트로 이미 검증**된다(아래 경로). e2e 1:1 관점에서 e2e 비대상이되 **예외가 아니라 정적/단위 검증으로 충족**이다. 후속 per-AC 리팩터 대상도 아님.

- **dear-baby-reset-user/AC3** 파괴적 작업 표기 → `internal/server/mcp_test.go::TestToolsListAdvertisesDearBabyReset` (reset `destructiveHint=true` 단언). ✔ 충족.
- **opensearch-document-put/AC3** 파괴적 작업 표기 → `internal/server/mcp_test.go::TestToolsListAdvertisesOpenSearchDocumentPut` (`destructiveHint=true`). ✔ 충족.
- **opensearch-document-delete/AC3** 파괴적 작업 표기 → `internal/server/mcp_test.go::TestToolsListAdvertisesOpenSearchDocumentDelete` (`destructiveHint=true`). ✔ 충족.
- **workload-restart/AC2** 파괴적 작업 표기 → `internal/server/mcp_test.go::TestToolsListAdvertisesAnnotations` (workload_restart `destructiveHint=true`). ✔ 충족.
- **workload-scale/AC3** 파괴적 작업 표기 → `internal/server/mcp_test.go::TestToolsListAdvertisesWorkloadScale` (`destructiveHint=true`). ✔ 충족.

### 🚫 e2e 예외 (1) — e2e 비현실적, 모델 정의 예외 개정 제안

> e2e로 커버하기 비현실적이고 정적 검토로 대체하는 AC. definition이 `task에서 제안한다`고 명시하므로 모델 정의(`to-be-models.json`)에 일방 적용하지 않고 ratify 후 예외 목록에 반영한다.
>
> **정의 예외 개정 제안(6건, e2e 비대상)**: e2e 1:1 계수에서 빠져야 하는 AC는 아래 🚫 1건 + 위 🔧 5건 = 6건이다. 단 🔧 5건은 정적/단위 메타데이터 검증으로 **충족**(테스트 경로 명시)이고, 아래 1건은 정적 매니페스트 리뷰로 대체한다. 정의의 예외 목록에는 6건을 등재하되 각 대체검증 수단을 함께 명시하도록 제안한다.

- **platform-auth-safety/AC4** 하드닝된 런타임 — [정적 매니페스트] `k8s/deployment.yaml` securityContext(비루트·읽기전용 루트FS·capability drop 등) 정적 검증 — definition이 든 e2e 예외 예시. 대체: 정적 매니페스트 리뷰 + (선택) 런타임 securityContext 단언 단위.

## 변경 이력

| 시점 | 변경 내용 | 이전 상태 | 이후 상태 |
|------|-----------|-----------|-----------|
| 2026-07-12 | AC↔e2e 1:1 정합성(reconciler) 레지스트리 신설: e2e-only 렌즈로 52 AC 분류, per-AC 케이스 식별 규약 명문화, e2e 보강 backlog·예외 제안 작성. **2026-07-19 사용자 검토 반영 재분류**: 미설정 graceful 거부 6·grafana AC4 출력 비노출 1을 예외→⬜ 보강(총 14), 파괴적 표기 5를 🔧 정적/단위 검증(기존 destructiveHint 단위 테스트로 충족)으로, platform AC4만 🚫 e2e 예외로 확정 → **✅32·⬜14·🔧5·🚫1**. 전용 per-AC 케이스 분리·신설과 정의 예외 개정(6건)은 후속·ratify. | 통합 파일 7개 다중 AC 공유, 인코드 AC 선언 1건, e2e 케이스 규약 부재 | 52 AC 분류(✅32·⬜14·🔧5·🚫1), 규약·backlog(14)·정적검증(5)·예외(1)·정의 개정 제안 문서화(tests/ 코드 미변경) |
| 2026-06-19 | 가치 문서 생성, V1~V3 정의, 소유자 지정 | (없음) | 가치 3 / PRD 0 / AC 0 / 테스트 0 |
| 2026-06-19 | 가치별 PRD 3종 작성(AC 18) | 가치 3 / PRD 0 | 가치 3 / PRD 3 / AC 18 / 테스트 0 |
| 2026-06-19 | PRD를 도구 단위로 재구성(도구 11 + 공통 1), AC 36 | PRD 3 / AC 18 | 가치 3 / PRD 12 / AC 36 / 테스트 0 |
| 2026-06-19 | workload_logs AC2 정정(초과 시 클램프 → 거부), 테스트 문서 12종 작성 | 테스트 0 | 가치 3 / PRD 12 / AC 36 / 테스트 12 (전 계층 연결) |
| 2026-06-22 | platform AC1·AC2 인증 게이트/디스커버리 단위 테스트 추가(`internal/auth/auth_test.go`) | AC1·AC2 자동화 공백 | platform AC1·AC2 자동 검증(자동화 공백 7→5) |
| 2026-06-22 | github·grafana AC4 베이스 시크릿 비노출 단위 테스트 추가(`internal/github`·`internal/grafana`) | github·grafana AC4 자동화 공백 | github·grafana AC4 자동 검증(잔여 공백: workload_logs AC3 1건) |
| 2026-07-03 | workload_logs AC3 크래시 루프 previous 로그 **내용** e2e 추가(`crashloop-fixture` + `workload.py`) | workload_logs AC3 자동화 공백 | workload_logs 전 AC 자동 검증(잔여: opensearch 프로덕션 스모크 — 외부 배선 대기) |
| 2026-07-02 | V4(운영 지식의 축적·검색) 추가, OpenSearch Serverless 도구 3종 PRD(AC 14)·테스트 문서 작성 — 구현 선행 문서(인프라 `kubernetes-docs` 컬렉션·권한은 부여 완료, 코드 미구현) | 가치 3 / PRD 12 / AC 36 / 테스트 12 | 가치 4 / PRD 15 / AC 50 / 테스트 15 |
| 2026-07-02 | OpenSearch 도구 3종 구현(`internal/opensearch` + 도구 표면 + CI 통합 테스트), 테스트 문서 자동화 필드를 실제 테스트 경로로 갱신 | opensearch 14 AC 자동화 공백(도구 미구현) | opensearch 14 AC 자동 검증(프로덕션 스모크만 잔여 — env 배선 후) |
| 2026-07-04 | platform PRD에 API 키 인증 AC7·AC8 추가(비대화형 자동화용, 구현 선행 문서), values V3 서술 확장, 테스트 시나리오 7·8 추가. 위험 진단 수치 정합성 보정(PRD 15/AC 52/테스트 15) | 가치 4 / PRD 15 / AC 50 / 테스트 15 | 가치 4 / PRD 15 / AC 52 / 테스트 15 (전 계층 연결, AC7·AC8만 자동화 공백) |
| 2026-07-04 | platform AC7·AC8 구현(`internal/auth` API 키 게이트·OAuth 선택화·디스커버리 조건부 + `MCP_API_KEYS`) 및 단위 테스트(`internal/auth/auth_test.go`·`internal/server/auth_routing_test.go`) 작성, 테스트 문서 자동화 필드를 실제 테스트 경로로 갱신 | platform AC7·AC8 자동화 공백(구현 선행 문서) | platform AC7·AC8 자동 검증(전 계층 연결·자동화 완료) |
