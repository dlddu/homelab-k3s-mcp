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

- 정의된 가치: **5개** (V1~V5)
- PRD: **18개** (도구 17 + 공통 기반 1)
- Acceptance Criteria: **64개** (가치 연결됨: 64 / 미연결: 0)
- 테스트 문서: **18개** (AC 커버됨: 64 / 미커버: 0)
- **건강 상태**: 🟢 **건강함** — 가치 → PRD → AC → 테스트 전 계층 연결 완료

> 문서 체계의 모든 화살표가 연결되었다(고아 가치·미정렬 문서·무가치 PRD·AC 없는 PRD·
> 미연결 AC·미검증 AC·고아 테스트 없음). 별도로, 테스트 문서가 참조하는 **자동화의 실제
> 커버리지**는 아래 "자동화 커버리지"에 정리한다(문서 구조와 별개의 일부 공백 존재).

## 문서 인벤토리

| 종류 | 파일 |
|------|------|
| 가치 문서 | `values.md` |
| PRD (도구) | `prd-ping.md`, `prd-namespace-list.md`, `prd-workload-list.md`, `prd-workload-logs.md`, `prd-pod-describe.md`, `prd-workload-restart.md`, `prd-workload-scale.md`, `prd-dear-baby-reset-user.md`, `prd-github-app-installation-token.md`, `prd-grafana-token.md`, `prd-aws-config-get.md`, `prd-opensearch-search.md`, `prd-opensearch-document-put.md`, `prd-opensearch-document-delete.md`, `prd-session-list.md`, `prd-session-read.md`, `prd-session-write.md` |
| PRD (공통) | `prd-platform-auth-safety.md` |
| 테스트 문서 | 각 PRD에 대응하는 `test-*.md` (18개) |
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
| dear_baby_reset_user | V5, V3 | 3 | test-dear-baby-reset-user | ✅ 완전 |
| github_app_installation_token | V2, V3 | 4 | test-github-app-installation-token | ✅ 완전 |
| grafana_token | V2, V3 | 4 | test-grafana-token | ✅ 완전 |
| aws_config_get | V2, V3 | 3 | test-aws-config-get | ✅ 완전 |
| opensearch_search | V4, V2, V3 | 4 | test-opensearch-search | ✅ 완전 |
| opensearch_document_put | V4, V2, V3 | 5 | test-opensearch-document-put | ✅ 완전 |
| opensearch_document_delete | V4, V2, V3 | 5 | test-opensearch-document-delete | ✅ 완전 |
| session_list | V5, V3 | 3 | test-session-list | ✅ 완전 (구현 선행) |
| session_read | V5, V3 | 4 | test-session-read | ✅ 완전 (구현 선행) |
| session_write | V5, V3 | 5 | test-session-write | ✅ 완전 (구현 선행) |
| platform (인증·안전 공통) | V3 | 8 | test-platform-auth-safety | ✅ 완전 |

## 가치 커버리지

| 가치 | 이 가치를 달성하는 PRD |
|------|------------------------|
| V1: 자연어로 클러스터 운영 | namespace_list, workload_list, workload_logs, pod_describe, workload_restart, workload_scale |
| V2: 단명·최소권한 자격증명 | github_app_installation_token, grafana_token, aws_config_get, opensearch_search, opensearch_document_put, opensearch_document_delete |
| V3: 안전한 운영(Safe-by-default) | platform(인증·안전), ping, pod_describe, workload_restart, workload_scale, dear_baby_reset_user, github_app_installation_token, grafana_token, aws_config_get, opensearch_search, opensearch_document_put, opensearch_document_delete, session_list, session_read, session_write |
| V4: 운영 지식의 축적·검색 | opensearch_search, opensearch_document_put, opensearch_document_delete |
| V5: 클러스터 내부 앱 기능의 도구화 | dear_baby_reset_user, session_list, session_read, session_write |

## 위험 진단

### 고아 가치 (소유자 없는 가치)
- (없음) — 모든 가치의 소유자는 "홈랩 운영자"

### 미정렬 문서 (가치 참조 없는 문서)
- (없음)

### 무가치 PRD / AC 없는 PRD
- (없음) — 18개 PRD 모두 가치를 달성하고 AC를 보유

### 미연결 AC (가치와 연결되지 않은 AC)
- (없음) — 64개 AC 모두 가치에 연결

### 미검증 AC (테스트 없는 AC)
- (없음) — 64개 AC 모두 테스트 문서의 시나리오로 커버

### 고아 테스트 (AC를 참조하지 않는 테스트)
- (없음) — 18개 테스트 문서 모두 검증 대상 AC를 명시

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
  픽스처는 security off 단일노드 OpenSearch + MinIO STS + 접근 경로 관측용
  `http-trace` 기록 프록시),
  platform AC1·AC2(인증 게이트·디스커버리)·AC5·AC6·AC7·AC8(API 키 게이트·구성 유연성·
  디스커버리 조건부 — `internal/auth/auth_test.go`·`internal/server/auth_routing_test.go`).
- 🟡 **정적 검증** (매니페스트 리뷰): platform AC3(RBAC 경계 — `k8s/rbac.yaml`),
  platform AC4(하드닝 — `k8s/deployment.yaml`).
- 🔴 **자동화 공백 — 추가 권장**:
  - **session 3종 12 AC — 도구 미구현(구현 선행 문서)**. session-platform 배포가
    2026-08-06에 클러스터에서 제거된 상태라(레포 `dlddu/session-platform`은 유지) 실제
    제어면 대상 검증이 불가능하다. 배선 순서: 앱 재배포 → `SESSION_PLATFORM_ENDPOINT`
    배선 → `internal/sessionplatform` 구현 → 단위·통합 테스트 작성.
  - opensearch 3종 — **프로덕션 스모크 미수행**(env 배선이 infrastructure/flux-cd-apps
    반영에 걸려 있음). CI 자동화는 완료; 실제 `kubernetes-docs` 컬렉션 대상
    put→search→delete 확인은 배선 완료 후 수행.

## AC ↔ e2e 1:1 정합성 (reconciler 렌즈)

> **렌즈 차이**: reconciler 정합성 모델(`tbm_homelab-k3s-mcp-ac-e2e`)은 **`tests/integration/`의 통합 e2e만** 검증으로 인정한다 — `internal/`의 Go 단위 테스트는 정의상 e2e가 아니다. 따라서 위 "자동화 커버리지"에서 🟢로 세는 다수 AC가 이 e2e 렌즈에서는 **e2e 공백**으로 계수된다. 이 섹션은 그 e2e-전용 렌즈의 레지스트리다.

### 파일 식별 규약 (규칙 1·2·3·5·6)

> **2026-08-14 개정 — 매칭 단위가 "테스트 케이스"에서 "파일"로 바뀌었다.** 모델 정의(`tbm_homelab-k3s-mcp-ac-e2e`)가 `ac-e2e` 템플릿 고정부에 맞춰 판정 단위를 파일로 옮겼다. 파일 안에서 케이스가 몇 개로 쪼개져 있는지는 이제 판정과 **무관**하다. 케이스 단위 시절에 쌓인 per-AC 케이스는 그대로 자산이며, 분할은 "새 검증 작성"이 아니라 **케이스를 파일로 승격**하는 작업이다.

- **규칙 1 (AC→파일)**: 예외 목록에 없는 모든 AC는 자신을 주검증하는 파일을 **정확히 하나** 가진다. 여러 AC를 겸하는 파일은 그 AC의 전용 파일이 아니므로, 겸용 상태의 AC는 여전히 **공백**으로 계수한다.
- **규칙 2 (파일→AC)**: 모든 매칭 단위 파일은 **정확히 하나의 AC**만 주검증 대상으로 선언한다. 2개 이상을 선언한 파일은 **분할 대기**(규칙 2 위반)다.
- **규칙 3 (식별)**: 매칭 단위 파일은 **모듈 docstring**에 `검증 AC: <domain>/AC<n>` 을 선언한다. AC 대신 스모크/인프라를 검증하는 파일은 `검증 AC: 없음 (스모크/인프라)` 을 선언하고 아래 "비-AC 파일" 목록에 등재한다. 어디에도 매핑되지 않은 파일은 고아다.
- **매칭 단위**: `tests/integration/` 최상위 `*.py`. 단 공유 헬퍼 `_helpers.py` 와 하네스 자신(`run_all.py` · `check_ac_mapping.py`)은 매칭 단위가 아니다.
- **기계 검사**: `python3 tests/integration/check_ac_mapping.py` 가 위 규칙과 아래 집계를 CI(`fmt + vet` 잡)에서 강제한다. 이 표의 행별 상태·집계 숫자가 실측과 **정확히** 같아야 통과하므로, 파일을 쪼개거나 AC를 추가한 PR은 같은 PR에서 이 절을 갱신해야 한다.
- **실행 하네스**: `tests/integration/run_all.py` 가 매칭 단위 파일을 자동 발견해 각 파일이 신고한 `실행 대상`(primary · auth-variant)별로 실행한다. CI는 파일을 이름으로 나열하지 않으므로 분할할 때마다 `ci.yml` 을 고칠 필요가 없고, 체커가 "매칭 단위 파일 전부가 정확히 한 번 배차된다"를 검사해 **만들어 놓고 실행되지 않는 파일**을 구조적으로 막는다.

<!-- ac-e2e-집계 -->
- AC 전집: 64
- 예외 등재: 1
- 1:1 대상: 63
- 매칭 파일(전용): 9
- 분할 대기 파일(규칙 2 위반): 6
- 공백 AC: 54
<!-- /ac-e2e-집계 -->

> 공백 54건의 내역: **40건**은 분할 대기 파일 6개(`workload.py` 16 · `opensearch.py` 11 · `no_config.py` 7 · `aws_config.py` 2 · `auth.py` 2 · `smoke.py` 2)가 겸용으로 커버하고 있어 분할만 하면 ✅가 되고, **14건**은 케이스 자체가 없다(아래 backlog). 진행 방향은 매칭 파일 ↑ / 분할 대기 ↓ / 공백 ↓ 이다.

### AC 레지스트리 (64) — ✅ 전용 파일 9 · ⬜ 분할 대기 40 · ⬜ 공백(케이스 없음) 14 · 🚫 예외 1

| AC | 제목 | e2e 상태 |
|----|------|----------|
| aws-config-get/AC1 | 고정 객체 조회 | ⬜ 분할 대기 — `aws_config.py`(2 AC 겸용) |
| aws-config-get/AC2 | 정적 키 미사용 | ⬜ 분할 대기 — `aws_config.py`(2 AC 겸용) |
| aws-config-get/AC3 | 미설정 시 graceful 거부 | ⬜ 분할 대기 — `no_config.py`(7 AC 겸용) |
| dear-baby-reset-user/AC1 | 온보딩 리셋 실행 | ✅ 전용 파일 `dear_baby_reset_user_ac1.py` |
| dear-baby-reset-user/AC2 | 명시적 대상 지정 | ✅ 전용 파일 `dear_baby_reset_user_ac2.py` |
| dear-baby-reset-user/AC3 | 파괴적 작업 표기 | ✅ 전용 파일 `dear_baby_reset_user_ac3.py` |
| github-app-installation-token/AC1 | 단명 설치 토큰 발급 | ✅ 전용 파일 `github_app_installation_token_ac1.py` |
| github-app-installation-token/AC2 | 스코프 제한 | ✅ 전용 파일 `github_app_installation_token_ac2.py` |
| github-app-installation-token/AC3 | 미설정 시 graceful 거부 | ⬜ 분할 대기 — `no_config.py`(7 AC 겸용) |
| github-app-installation-token/AC4 | 베이스 키 비노출 | ✅ 전용 파일 `github_app_installation_token_ac4.py` |
| grafana-token/AC1 | read-only 토큰 발급 | ✅ 전용 파일 `grafana_token_ac1.py` |
| grafana-token/AC2 | 즉시 사용 가능한 형태 | ✅ 전용 파일 `grafana_token_ac2.py` |
| grafana-token/AC3 | 미설정 시 graceful 거부 | ⬜ 분할 대기 — `no_config.py`(7 AC 겸용) |
| grafana-token/AC4 | 발급자 토큰 비노출 | ✅ 전용 파일 `grafana_token_ac4.py` |
| namespace-list/AC1 | 네임스페이스 열거 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| opensearch-document-delete/AC1 | 단일 문서 삭제 | ⬜ 분할 대기 — `opensearch.py`(11 AC 겸용) |
| opensearch-document-delete/AC2 | 부재 문서의 명확한 처리 | ⬜ 분할 대기 — `opensearch.py`(11 AC 겸용) |
| opensearch-document-delete/AC3 | 파괴적 작업 표기 | ⬜ 분할 대기 — `opensearch.py`(11 AC 겸용) |
| opensearch-document-delete/AC4 | AssumeRole·SigV4 접근 | ⬜ 분할 대기 — `opensearch.py`(11 AC 겸용) |
| opensearch-document-delete/AC5 | 미설정 시 graceful 거부 | ⬜ 분할 대기 — `no_config.py`(7 AC 겸용) |
| opensearch-document-put/AC1 | 문서 색인·업서트 | ⬜ 분할 대기 — `opensearch.py`(11 AC 겸용) |
| opensearch-document-put/AC2 | 인덱스 자동 생성 | ⬜ 분할 대기 — `opensearch.py`(11 AC 겸용) |
| opensearch-document-put/AC3 | 파괴적 작업 표기 | ⬜ 분할 대기 — `opensearch.py`(11 AC 겸용) |
| opensearch-document-put/AC4 | AssumeRole·SigV4 접근 | ⬜ 분할 대기 — `opensearch.py`(11 AC 겸용) |
| opensearch-document-put/AC5 | 미설정 시 graceful 거부 | ⬜ 분할 대기 — `no_config.py`(7 AC 겸용) |
| opensearch-search/AC1 | 질의 검색 | ⬜ 분할 대기 — `opensearch.py`(11 AC 겸용) |
| opensearch-search/AC2 | 결과 상한 | ⬜ 분할 대기 — `opensearch.py`(11 AC 겸용) |
| opensearch-search/AC3 | AssumeRole·SigV4 접근 | ⬜ 분할 대기 — `opensearch.py`(11 AC 겸용) |
| opensearch-search/AC4 | 미설정 시 graceful 거부 | ⬜ 분할 대기 — `no_config.py`(7 AC 겸용) |
| ping/AC1 | 항상 pong 응답 | ⬜ 분할 대기 — `smoke.py`(2 AC 겸용) |
| platform-auth-safety/AC1 | 인증 게이트 | ⬜ 분할 대기 — `auth.py`(2 AC 겸용) |
| platform-auth-safety/AC2 | 인증 디스커버리 | ⬜ 공백 — 케이스 없음 |
| platform-auth-safety/AC3 | 최소권한 RBAC 경계 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| platform-auth-safety/AC4 | 하드닝된 런타임 | 🚫 예외 |
| platform-auth-safety/AC5 | 서버 수준 graceful degradation | ⬜ 분할 대기 — `no_config.py`(7 AC 겸용) |
| platform-auth-safety/AC6 | 헬스·레디니스 | ⬜ 분할 대기 — `smoke.py`(2 AC 겸용) |
| platform-auth-safety/AC7 | API 키 인증 | ⬜ 분할 대기 — `auth.py`(2 AC 겸용) |
| platform-auth-safety/AC8 | 인증 방식 구성 유연성 | ⬜ 공백 — 케이스 없음 |
| pod-describe/AC1 | 파드 상세 스냅샷 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| pod-describe/AC2 | 대상 지정 방식 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| pod-describe/AC3 | 이벤트 best-effort | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| session-list/AC1 | 세션 열거 | ⬜ 공백 — 케이스 없음 |
| session-list/AC2 | 상태를 바꾸지 않는 조회 | ⬜ 공백 — 케이스 없음 |
| session-list/AC3 | 미설정 시 graceful 거부 | ⬜ 공백 — 케이스 없음 |
| session-read/AC1 | 오프셋 커서 읽기 | ⬜ 공백 — 케이스 없음 |
| session-read/AC2 | 상태 분기 노출 | ⬜ 공백 — 케이스 없음 |
| session-read/AC3 | 대상 부재·잘못된 커서 처리 | ⬜ 공백 — 케이스 없음 |
| session-read/AC4 | 미설정 시 graceful 거부 | ⬜ 공백 — 케이스 없음 |
| session-write/AC1 | 워크로드 입력 주입 | ⬜ 공백 — 케이스 없음 |
| session-write/AC2 | 상태 분기 처리와 노출 | ⬜ 공백 — 케이스 없음 |
| session-write/AC3 | 파괴적 작업 표기 | ⬜ 공백 — 케이스 없음 |
| session-write/AC4 | 거부 응답의 구분 전달 | ⬜ 공백 — 케이스 없음 |
| session-write/AC5 | 미설정 시 graceful 거부 | ⬜ 공백 — 케이스 없음 |
| workload-list/AC1 | 종류별 워크로드 조회 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| workload-list/AC2 | 네임스페이스 스코프 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| workload-logs/AC1 | 워크로드 기준 로그 조회 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| workload-logs/AC2 | tail 라인 제어 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| workload-logs/AC3 | 크래시 루프 후 직전 로그 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| workload-logs/AC4 | 컨테이너 선택과 필터 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| workload-restart/AC1 | 롤링 재시작 트리거 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| workload-restart/AC2 | 파괴적 작업 표기 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| workload-scale/AC1 | 레플리카 설정 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| workload-scale/AC2 | DaemonSet 거부 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |
| workload-scale/AC3 | 파괴적 작업 표기 | ⬜ 분할 대기 — `workload.py`(16 AC 겸용) |

### ⬜ e2e 보강 backlog (14) — e2e 가능 클러스터 동작, 전용 케이스 신설 필요

> 새 통합 e2e는 kind 클러스터 실서버 배포로 실행되므로 앱 구동 검증이 필요 — 후속 task로 저작한다.

- **platform-auth-safety/AC2** 인증 디스커버리 → `tests/integration/smoke.py`: 디스커버리 엔드포인트가 인증 방식을 반환하는지 (OAuth/OIDC 발급자 mock 픽스처 필요)
- **platform-auth-safety/AC8** 인증 방식 구성 유연성 → `tests/integration/smoke.py`: env-게이팅 다중 구성 배포 변형에서 인증 방식 전환
- **session-list/AC1·AC2·AC3 · session-read/AC1~AC4 · session-write/AC1~AC5 (12)** → `tests/integration/session.py`(신규) + 미설정 거부 3건은 기존 `no_config.py`(port 8088, 자격증명 미부착 변형). **선행 조건이 다르다**: 위 platform 2건과 달리 이쪽은 e2e 이전에 **도구 자체가 미구현**이고 session-platform 배포도 제거된 상태다. 순서는 앱 재배포 → env 배선 → `internal/sessionplatform` 구현 → 단위 → e2e. e2e 픽스처는 kind에 제어면을 띄우는 방식과 제어면 스텁 중 택일이며, AC2(상태 분기 노출)·write/AC4(거부 사유 구분)는 `idle`/`snapshot` 전이와 429/507을 재현해야 해서 스텁 쪽이 현실적이다.

> **AssumeRole·SigV4 4건 — 관측 수단 신설 후 ✅ 승격(2026-08-14)**: 직전 슬라이스가 `⬜ 보강 필요 (관측 수단 부재)`로 정정했던 4건(aws-config-get/AC2 · opensearch-search/AC3 · opensearch-document-put/AC4 · opensearch-document-delete/AC4)을, **관측 수단을 먼저 만든 뒤** per-AC 전용 케이스로 승격했다. 정정 자체는 옳았다 — 그때는 어떤 e2e 단언도 "역할을 한 번도 assume 하지 않는 서버"에서 그대로 통과했다. 바뀐 것은 픽스처다.

신규 픽스처 `tests/k8s/kind/http-trace.yaml`은 MinIO(S3+STS)와 OpenSearch **양쪽 앞단에 서는 기록 리버스 프록시** 하나다(기존 mock과 같은 ConfigMap + `python:3.12-alpine` 방식, 신규 이미지 빌드 없음). `AWS_CONFIG_S3_ENDPOINT`·`OPENSEARCH_STS_ENDPOINT`가 `:9000`(→MinIO), `OPENSEARCH_ENDPOINT`가 `:9200`(→OpenSearch)을 가리키고, 트레이스는 `:8081`로 읽는다 (CI 스텝 2개에 포트포워드 8090·8091 추가, 신규 검증 스텝은 없음).

**왜 MinIO 단독 trace가 아니라 프록시인가**: backlog가 든 후보 (a) `MINIO_HTTP_TRACE`는 MinIO에 닿는 트래픽만 본다. opensearch 3건이 요구하는 "AssumeRole **→ SigV4 서명 요청**"의 뒷부분, 즉 데이터 플레인 서명은 OpenSearch로 가지 MinIO로 가지 않으므로 (a)만으로는 "AssumeRole은 했다"까지만 관측되고 서명 절반은 여전히 공백으로 남는다. 한 프록시가 양쪽을 기록하면 STS가 발급한 AccessKeyId와 데이터 플레인 서명 키를 **같은 트레이스 안에서 대조**할 수 있어, 캐시 타이밍 의존(그 run의 첫 호출에서만 관측 가능)과 파드 로그 스크래핑도 함께 사라진다.

**단정의 구조**(`_helpers.py::assert_assumed_role_access`): (1) 설정된 role ARN의 AssumeRole이 성공했고 그 STS 호출 자체는 **베이스 자격증명으로 서명**됐다 — 베이스 키의 용도가 assume 뿐임을 고정, (2) 데이터 플레인 요청이 `AWS4-HMAC-SHA256`으로 해당 region의 `s3`/`aoss` 스코프에서 서명됐고 `host`(검색·색인은 payload 해시도)를 서명 대상에 포함한다, (3) 서명한 액세스 키가 **STS가 내준 키 집합에 속하고 베이스 키가 아니며** 세션 토큰을 동반한다. 셋은 각각 다른 위장 경로를 falsify한다 — (1)은 STS를 건너뛴 서버, (2)는 무서명 요청(security 꺼진 픽스처가 받아주는 것), (3)은 베이스 키 서명(MinIO가 받아주는 것).

**프록시의 전제와 그 검증**: SigV4는 `Host`를 서명하므로 프록시는 클라이언트가 서명한 `Host`를 그대로 전달해야 업스트림의 재계산이 일치한다. 이 투명성은 SigV4를 실제로 재계산해 불일치를 403으로 거부하는 가짜 업스트림에 대해 검증했고, `Host`를 재작성하면 403이 나오는 음성 대조로 그 검증이 vacuous하지 않음까지 확인했다. 기록은 식별자만 남긴다 — 서명값·세션 토큰·STS 시크릿 키·요청/응답 본문은 저장하지 않는다(STS 응답에서 꺼내는 것은 AccessKeyId 하나뿐이며, 이는 서명된 요청마다 평문으로 오가는 식별자다). 링버퍼가 넘칠 때 AssumeRole 레코드는 evict하지 않는다 — 자격증명이 캐시돼 프로세스당 한 번만 발급되므로 밀려나면 재관측이 불가능하다.

**잔여**: `workload-logs/AC4`의 부분 단정과 backlog 14건(platform 2 · session 12)은 그대로다.

> **부분 단정 잔여(계수 밖, ✅ 유지)**: `workload-logs/AC4`의 "파드에 컨테이너가 둘 이상이면 `container`가 필요하다" 절은 아직 단정되지 않는다 — 이 배포의 픽스처 파드가 전부 단일 컨테이너라 거부를 관측할 대상이 없다. 러닝 멀티컨테이너 픽스처 + 롤아웃 대기(= `ci.yml` 스텝 추가)가 필요해 후속 슬라이스로 남긴다. 케이스 docstring에 "단언하지 않는 것"으로 명시돼 있다(2026-08-07).

> **잔여 파일 수준 커버 13건 정리 — ✅ 완료(2026-08-13)**: `opensearch.py`(9) · `aws_config.py`(2) · `dear_baby.py`(2)를 마지막으로 `✅ 통합` 행이 0이 됐다. **9건은 per-AC 전용 케이스로 승격**(opensearch-document-put/AC1·AC2 · opensearch-search/AC1·AC2 · opensearch-document-delete/AC1·AC2 · aws-config-get/AC1 · dear-baby-reset-user/AC1·AC2), **4건은 관측 불가를 근거로 ⬜로 정정**(위 backlog 참조). 신규 픽스처·신규 CI 스텝·신규 롤아웃 대기 없음 — 이미 도는 스텁(`dear_baby.py` 8082 · `aws_config.py` 8084 · `opensearch.py` 8086) 안에서 평면 `run()`을 케이스 디스패처로 재구성했다. opensearch 쪽 공유 상태(put→search→delete 한 파이프라인)는 **케이스별 전용 인덱스 `ci-<case>-<RUN_ID>` + 케이스별 유일 질의 토큰**으로 없앴다(put이 인덱스를 자동 생성하므로 픽스처 추가가 필요 없고, 컬렉션 전체 검색도 다른 케이스의 문서와 겹치지 않는다). 분리하며 **AC 문언 대비 비어 있던 단정 4건을 채웠다**: (1) opensearch-search/AC2의 "기본값 10"은 한 번도 관측된 적이 없었다(문서 3건뿐이라 기본값과 무제한이 구분되지 않음) → 12건 시드 후 `len(hits)==10` · `total==12`; (2) opensearch-document-put/AC2의 "자동 생성"은 색인 전 상태를 보지 않아 성립하지 않았다 → 색인 전 해당 인덱스 검색이 404 `index_not_found_exception`으로 거부되는 것을 선행 관측; (3) dear-baby-reset-user/AC2의 "`email` 누락 거부"는 **단언이 0개**였다 → `McpError: email is required` 단언 신설, `container` 재정의도 관측(무시되면 성공해버리므로 실패 자체가 판별자); (4) aws-config-get/AC1의 메타데이터는 size만 봤다 → contentType·ETag(따옴표 제거된 다이제스트 모양)·lastModified(RFC3339 파싱, 미래 아님)까지 단정. tests/ 변경이라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변).

> **`workload.py` 통합 커버 11건 → per-AC 전용 케이스 — ✅ 완료(2026-08-07)**: 잔여 `✅ 통합` 24건 중 `workload.py`가 소유한 **11건 전부**(namespace-list/AC1 · workload-list/AC1·AC2 · workload-logs/AC1·AC2·AC3·AC4 · workload-restart/AC1 · workload-scale/AC1·AC2 · platform-auth-safety/AC3)를 per-AC 전용 케이스로 분리했다. **신규 CI 스텝·신규 네임스페이스·신규 롤아웃 대기 없음** — 이미 도는 `workload.py`(port 8081) 스텝 안에서 평면 `run()`을 케이스 디스패처로 재구성했고, 픽스처는 목록 조회 전용 오브젝트 2개(`workload-fixture-sts` StatefulSet · `workload-fixture-ds` DaemonSet)만 `test-deployment.yaml`에 더했다(러닝 파드를 기다리는 케이스가 없어 `ci.yml` 무변경). 분리하며 **AC 본문과 대조해 비어 있거나 반쪽이던 단정 6건을 실단정으로 채웠다**: (1) platform/AC3(RBAC 경계)은 `workload.py`에 단언이 **0개**였다 → `kubectl auth can-i`로 배포된 ServiceAccount 신원(그룹 3개 포함)을 임퍼소네이트해 허용 15동사·금지 11동사를 apiserver SubjectAccessReview로 관측(매니페스트 재독이 아니라 실제로 바인딩된 RBAC를 본다). (2) workload-list/AC1은 AC가 요구하는 "각 enum 종류"를 Deployment 하나만 보고 "레플리카 요약"을 아예 안 봤다 → 3 enum 전부 호출 + kind별 요약 필드를 단정하고 **다른 kind의 필드가 섞여 나오지 않음**까지 확인. (3) namespace-list/AC1은 AC 문언의 **생성 시각**을 빼먹었다 → 전 항목에 phase·creation_timestamp가 있고 파싱 가능한 실제 instant임을 단정. (4) workload-logs/AC1은 로그를 내지 않는 `pause` 픽스처에서 `logs == ""`만 봐 "최근 로그가 반환된다"를 관측하지 못했다 → 실제로 출력을 내는 `crashloop-fixture`의 현재 인스턴스 마커를 단정(이전 인스턴스 마커와 문자열이 달라 AC3와 서로를 대신할 수 없다). (5) workload-scale/AC1은 AC가 명시한 **replicas=0**을 한 번도 시도하지 않았다 → 3 → 0 → 1 경로로 확장(0은 `.status.replicas` 드레인으로 확인). (6) workload-restart/AC1은 "delete가 아닌 patch"를 단언하지 않았다 → 재시작 전후 `metadata.uid`·`creationTimestamp` 불변 + `metadata.generation` 증가를 단정. workload-logs/AC4도 에코 필드 확인에서 **출력 반영**(timestamps → RFC3339 접두, since_seconds → 시작 마커 탈락) + 잘못된 컨테이너 거부로 강화했다. 잔여 통합 13건(`aws_config.py` 2 · `dear_baby.py` 2 · `opensearch.py` 9)은 후속 슬라이스.

> **통합 커버 8건 → per-AC 전용 케이스 — ✅ 완료(2026-08-07)**: 규칙 1·2를 미충족하던 파일 수준 `✅ 통합` 32건 중 **8건**(ping/AC1 · platform-auth-safety/AC5·AC6 · github-app-installation-token/AC1·AC2·AC4 · grafana-token/AC1·AC2)을 per-AC 전용 케이스로 분리했다. **신규 픽스처·신규 CI 스텝 없음** — 이미 도는 `smoke.py`(8080) · `github_app.py`(8083) · `grafana.py`(8085) · `no_config.py`(8088) 스텝 안에서 평면 `run()` 본문을 케이스 함수로 재구성했을 뿐이다. 분리 과정에서 **단정이 비어 있던 세 곳을 실제 단언으로 채웠다**: ping/AC1은 `smoke.py`가 `ping`을 호출조차 하지 않고 tools/list 존재만 확인했고, github/AC4(개인키 비노출)는 어떤 단언도 없었으며, grafana/AC1은 `# token expires` 주석의 **존재**만 봤다(이제 RFC3339를 파싱해 TTL이 50~70분임을 단언 — mock이 서버가 보낸 `expiresAt`을 되돌려주므로 1시간 TTL이 실제로 관측된다). **platform/AC5는 파일을 옮겼다**: AC 문언이 "자격증명 env를 비운 채 기동"을 전제하는데 `smoke.py`가 도는 주 배포는 모든 자격증명이 배선돼 있어 그 전제가 성립하지 않는다 — 전제가 성립하는 유일한 배포인 자격증명 미부착 변형(`no_config.py`, port 8088)으로 옮겨 `/healthz` + 전체 도구 표면 유지를 단언한다. 잔여 통합 24건(`aws_config.py` 2 · `dear_baby.py` 2 · `opensearch.py` 9 · `workload.py` 11)은 후속 슬라이스.

> **platform 인증 게이트(2) — ✅ 완료(2026-08-07)**: platform-auth-safety/AC1(인증 게이트)·AC7(API 키 인증)을, 인증을 켠 최소 배포 변형(`tests/k8s/kind/auth-fixture.yaml` — 같은 `:ci` 이미지에 `MCP_API_KEYS` 세팅·`MCP_AUTH_DISABLED` 미설정, 자격증명 시크릿 미부착 graceful degrade)을 별 네임스페이스에 띄워 검증하는 per-AC 전용 e2e 케이스로 승격했다(신규 CI 스텝 port 8087, 기존 배포·kustomize 불변). 케이스: `auth.py::test_platform_auth_safety_ac1_gate`(무Authorization → 401 `missing_token`), `::test_platform_auth_safety_ac7_api_key`(무효 키 → 401 `invalid_token`, 유효 키 → tools/list 인가). 잔여 platform 2건(AC2 디스커버리·AC8 구성 유연성)은 OIDC 발급자 mock·다중 구성 전환이 필요한 후속 슬라이스.

> **pod-describe(3) — ✅ 완료(2026-07-30)**: pod-describe/AC1·AC2·AC3을 기존 CI 스텝(`workload.py`, port 8081)과 `workload-fixture` 러닝 파드를 재사용하는 per-AC 전용 케이스로 승격했다(신규 파일·픽스처·`ci.yml` 변경 없음). 케이스: `workload.py::test_pod_describe_ac1_snapshot`(스냅샷 필드), `::test_pod_describe_ac2_target_resolution`(name/selector/workload 해석 + 상호배타 거부), `::test_pod_describe_ac3_events_best_effort`(events 필드 best-effort present).

> **미설정 graceful 거부(6) — ✅ 완료(2026-08-07)**: 별도 no-config 픽스처를 만들지 않고, 인증 변형 `tests/k8s/kind/auth-fixture.yaml`이 이미 **자격증명 시크릿을 하나도 붙이지 않은** 배포(= `GITHUB_APP_CLIENT_ID`·`AWS_CONFIG_S3_BUCKET`·`GRAFANA_ISSUER_TOKEN`·`OPENSEARCH_ENDPOINT` 전부 미설정 → `main.go`의 `build*Service`가 모두 `NewUnavailable("")`로 degrade)라는 점을 이용해 그 파드에 6개 per-AC 전용 케이스를 신설했다(`tests/integration/no_config.py`, CI 스텝 port 8088 하나 추가, 신규 픽스처·신규 롤아웃 대기 없음). 각 케이스는 (1) 호출이 `isError=true` + `<도메인> unavailable: <미구성 사유>` 텍스트로 돌아오고 (2) 직후 `ping`이 여전히 `pong`을 반환함을 단언해 AC 문언의 "서버 기동·다른 도구에 영향 없음"까지 관측한다. 그 대가로 이 변형에는 자격증명을 붙이면 안 된다(픽스처 헤더 주석에 명시).

> **파괴적 작업 표기(5) — ✅ 완료(2026-07-21)**: 파괴 동작을 실제로 실행하지 않고 배포 서버 `tools/list`의 `annotations.destructiveHint == true`(및 `readOnlyHint == false`)를 e2e로 단언하는 per-AC 전용 케이스를 신설해 위 레지스트리에서 ✅로 승격했다(`internal/server/mcp_test.go`의 in-process 단언을 배포 서버 통합 e2e로 승격). 케이스: `dear_baby.py::test_dear_baby_reset_user_ac3_destructive_hint`, `opensearch.py::test_opensearch_document_{put,delete}_ac3_destructive_hint`, `workload.py::test_workload_{restart_ac2,scale_ac3}_destructive_hint`. 남은 backlog 14건은 no-config 배포 변형·신규 픽스처가 필요한 후속 슬라이스.

### 비-AC 파일 (스모크·인프라) (0)

> AC 대신 스모크/인프라 확인(서버 기동·`/healthz`·도구 표면 존재)을 주검증한다고 선언한 매칭 단위 파일의 등재 자리다(규칙 3). 현재 0건이다 — smoke.py 는 ping/AC1 과 platform-auth-safety/AC6 을 선언하고 있어 비-AC 파일이 아니라 **분할 대기** 파일이다. 그 둘을 전용 파일로 떼어내는 후속 슬라이스에서, 남는 도구 표면 확인(공유 선행 조건)을 비-AC 파일로 등재할지 판단한다.

- (없음)

### 🚫 e2e 예외 (1) — e2e 비현실적, 모델 정의 예외 개정 제안

> e2e로 커버하기 비현실적이고 정적 검토로 대체하는 AC. definition이 `task에서 제안한다`고 명시하므로 모델 정의(`to-be-models.json`)에 일방 적용하지 않고 ratify 후 예외 목록에 반영한다.
>
> **정의 예외 개정 제안(1건, e2e 비대상)**: e2e 1:1 계수에서 빠져야 하는 AC는 아래 platform/AC4 1건뿐이다. 파괴적 표기 5건은 `tools/list` 메타데이터를 e2e로 단언해 커버하므로(✅ 전용 케이스, 2026-07-21 완료) e2e 예외가 아니다. 정의의 예외 목록에는 이 1건만 등재하도록 제안한다.

- **platform-auth-safety/AC4** 하드닝된 런타임 — [정적 매니페스트] `k8s/deployment.yaml` securityContext(비루트·읽기전용 루트FS·capability drop 등) 정적 검증 — definition이 든 e2e 예외 예시. 대체: 정적 매니페스트 리뷰 + (선택) 런타임 securityContext 단언 단위.

## 변경 이력

| 시점 | 변경 내용 | 이전 상태 | 이후 상태 |
|------|-----------|-----------|-----------|
| 2026-08-14 | **판정 단위를 케이스 → 파일로 옮긴 개정(모델 `tbm_homelab-k3s-mcp-ac-e2e`, reconciler `7529b609`)에 맞춰 e2e 렌즈를 재작성했다.** ① 매칭 단위 파일 9개 전부에 모듈 docstring `검증 AC:`·`실행 대상:` 선언을 도입(개정 전에는 확인 지점이 레포에 0건이라 파일 단위 매핑을 기계로 확인할 방법 자체가 없었다). ② 체커 `tests/integration/check_ac_mapping.py` 신설 — AC 전집(PRD)·선언·레지스트리를 각각 재도출해 규칙 1·2·3·5·6과 집계 일치를 lint 잡에서 강제한다. ③ 러너 `tests/integration/run_all.py` 신설 — 파일을 자동 발견해 `실행 대상`별로 실행하고, `ci.yml`의 파일별 9개 스텝을 배포 대상별 2스텝으로 대체했다(앞으로 분할해도 CI 수정 불필요, 배차 누락은 체커가 차단). ④ **3개 도메인 9 AC를 전용 파일로 분할**(grafana-token AC1·AC2·AC4 · github-app-installation-token AC1·AC2·AC4 · dear-baby-reset-user AC1·AC2·AC3) — 케이스 함수·상수를 한 글자도 바꾸지 않은 순수 이동이며 AST 대조로 26개 정의가 원본과 동일함을 확인했다. ⑤ 레지스트리 64행을 파일 단위 표기(전용 파일 / 분할 대기 / 공백 / 예외)로 재작성하고 집계 블록을 기계 판독 가능하게 만들었다. PRD(AC 본문)는 불변. | 케이스 단위: ✅49(전용 케이스)·⬜14·🚫1 (파일 단위로는 매칭 파일 0 · 규칙 1 위반 63) | 파일 단위: ✅ 전용 파일 9 · ⬜ 분할 대기 40 · ⬜ 공백(케이스 없음) 14 · 🚫 예외 1 |
| 2026-08-14 | AssumeRole·SigV4 계열 4건(aws-config-get/AC2 · opensearch-search/AC3 · opensearch-document-put/AC4 · opensearch-document-delete/AC4)을 **관측 수단 신설 후** per-AC 전용 케이스로 승격. 신규 픽스처 `tests/k8s/kind/http-trace.yaml` — MinIO(S3+STS)와 OpenSearch 양쪽 앞단에 서는 기록 리버스 프록시 1개(ConfigMap + `python:3.12-alpine`, 신규 이미지 없음)로, `AWS_CONFIG_S3_ENDPOINT`·`OPENSEARCH_STS_ENDPOINT`·`OPENSEARCH_ENDPOINT`를 프록시로 재배선하고 트레이스를 `:8081`로 노출(기존 스텝 2개에 포트포워드만 추가, 신규 검증 스텝 없음). backlog가 든 MinIO 단독 trace 후보는 데이터 플레인 서명이 OpenSearch로 가 관측 범위 밖이라 채택하지 않았다. 단정은 AssumeRole 발급 키 = 데이터 플레인 서명 키 대조 + 베이스 키 배제 + 세션 토큰 + 서명 스코프(`s3`/`aoss`)로, 무서명·베이스 키 서명·STS 미호출을 각각 falsify한다. 프록시의 Host 보존 전제는 SigV4를 재계산하는 가짜 업스트림과 Host 재작성 음성 대조로 검증. tests/·ci.yml 변경이라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변). | ✅45(전용 45)·⬜18·🚫1 | ✅49(전용 49)·⬜14·🚫1 |
| 2026-08-13 | 잔여 파일 수준 `✅ 통합` 커버 **13건**(`opensearch.py` 9 · `aws_config.py` 2 · `dear_baby.py` 2)을 정리해 레지스트리에서 `✅ 통합` 행을 0으로 만들었다. **9건은 per-AC 전용 케이스로 승격**(opensearch-document-put/AC1·AC2 · opensearch-search/AC1·AC2 · opensearch-document-delete/AC1·AC2 · aws-config-get/AC1 · dear-baby-reset-user/AC1·AC2), **4건(opensearch-search/AC3 · opensearch-document-put/AC4 · opensearch-document-delete/AC4 · aws-config-get/AC2 — 전부 AssumeRole·SigV4 계열)은 ⬜로 정정**: 파일에 접근 경로를 관측하는 단언이 없었고, security 플러그인이 꺼진 OpenSearch 픽스처와 베이스 자격증명을 그대로 받아주는 MinIO에서는 어떤 e2e 단언도 '역할을 assume 하지 않는 서버'에서 그대로 통과해 vacuous하다(관측 수단 신설이 선행되는 후속 슬라이스). 신규 픽스처·CI 스텝·롤아웃 대기 없이 기존 스텝(8082·8084·8086) 안에서 `run()`을 케이스 디스패처로 재구성하고, opensearch의 put→search→delete 공유 상태는 케이스별 전용 인덱스 + 유일 질의 토큰으로 제거. 분리하며 빈 단정 4건 보강(search/AC2 기본값 10 최초 관측 — 12건 시드, put/AC2 색인 전 404 선행 관측, dear-baby/AC2 `email` 누락 거부 단언 신설, aws-config/AC1 contentType·ETag·lastModified 단정). tests/ 변경이라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변). | ✅49(통합 13·전용 36)·⬜14·🚫1 | ✅45(전용 45)·⬜18·🚫1 |
| 2026-08-13 | `workload.py`가 소유하던 파일 수준 `✅ 통합` 커버 **11건**(namespace-list/AC1 · workload-list/AC1·AC2 · workload-logs/AC1~AC4 · workload-restart/AC1 · workload-scale/AC1·AC2 · platform-auth-safety/AC3)을 per-AC 전용 케이스로 분리(규칙 1·2). 신규 CI 스텝·네임스페이스·롤아웃 대기 없이 기존 스텝(`workload.py` 8081) 안에서 `run()`을 케이스 디스패처로 재구성하고, `test-deployment.yaml`에 목록 조회 전용 StatefulSet·DaemonSet 1개씩만 추가(`ci.yml` 무변경). 분리하며 빈/반쪽 단정 6건을 실단정으로 보강(platform/AC3은 단언 0 → `kubectl auth can-i` 임퍼소네이션으로 허용 15·금지 11 동사 관측, workload-list/AC1은 enum 1종·요약 미단언 → 3종+kind별 요약, namespace-list/AC1 생성 시각 추가, workload-logs/AC1을 실제 출력 픽스처로 이관, workload-scale/AC1에 replicas=0 추가, workload-restart/AC1에 uid·generation 단정 추가). tests/ 변경이라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변). | ✅49(통합 24·전용 25)·⬜14·🚫1 | ✅49(통합 13·전용 36)·⬜14·🚫1 |
| 2026-08-12 | V5(클러스터 내부 앱 기능의 도구화) 신설 + session-platform 제어면 연동 도구 3종(`session_list`·`session_read`·`session_write`) PRD·테스트 문서 작성(AC 12, 구현 선행 문서). 기존 `dear_baby_reset_user`의 달성 가치를 V1→V5로 재배치(앱 상태 조작은 클러스터 운영이 아님 — V1/V5 경계를 values.md에 명문화). 도구는 제어면 `GET /sessions`·`POST /sessions/{id}/read`·`/write`에 각각 대응하며, read/write의 "접근=active화" 부수 효과(유휴 승격·스냅샷 복원)를 결과 `path`로 노출하는 것을 AC로 못박음. session-platform 배포가 2026-08-06 제거된 상태라 12 AC 전부 자동화 공백. | 가치 4 / PRD 15 / AC 52 / 테스트 15 · ✅49·⬜2·🚫1 | 가치 5 / PRD 18 / AC 64 / 테스트 18 · ✅49·⬜14·🚫1 |
| 2026-08-07 | 파일 수준 `✅ 통합` 커버 8건(ping/AC1·platform-auth-safety/AC5·AC6·github-app-installation-token/AC1·AC2·AC4·grafana-token/AC1·AC2)을 per-AC 전용 케이스로 분리(규칙 1·2). 신규 픽스처·신규 CI 스텝 없이 기존 스텝(`smoke.py` 8080·`github_app.py` 8083·`grafana.py` 8085·`no_config.py` 8088) 안에서 `run()`을 케이스 디스패처로 재구성. 분리하며 빈 단정 3건을 실제 단언으로 보강(ping은 호출 자체가 없었음 → `pong` 단언, github/AC4 개인키 비노출 단언 신설, grafana/AC1은 주석 존재 → RFC3339 파싱 후 TTL 50~70분 단언). platform/AC5는 AC 전제(자격증명 미설정)가 성립하지 않는 `smoke.py`에서 자격증명 미부착 변형 `no_config.py`로 이관. tests/ 변경이라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변). | ✅49(통합 32·전용 17)·⬜2·🚫1 | ✅49(통합 24·전용 25)·⬜2·🚫1 |
| 2026-08-07 | "미설정 시 graceful 거부" 6건(aws-config-get/AC3·github-app-installation-token/AC3·grafana-token/AC3·opensearch-{search/AC4,document-put/AC5,document-delete/AC5})을 per-AC 전용 e2e 케이스로 승격(`tests/integration/no_config.py`). 신규 픽스처 없이 직전 슬라이스가 올린 `auth-fixture.yaml`(자격증명 시크릿 미부착 = 정의상 no-config 배포)을 재사용하고 `ci.yml`에 검증 스텝 1개(port 8088)만 추가. 각 케이스는 `isError=true` + `<도메인> unavailable: <미구성 사유>` 단언에 더해 직후 `ping`이 정상임을 단언해 "서버 기동·다른 도구 무영향"까지 커버. tests/ additive라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변). | ✅43·⬜8·🚫1 (미설정 거부 6 = ⬜) | ✅49·⬜2·🚫1 (미설정 거부 6 = ✅ 전용 케이스) |
| 2026-08-07 | platform-auth-safety/AC1(인증 게이트)·AC7(API 키 인증)을 인증 켠 배포 변형(`tests/k8s/kind/auth-fixture.yaml`: `MCP_API_KEYS` 세팅·`MCP_AUTH_DISABLED` 미설정, 자격증명 시크릿 미부착 graceful degrade) 전용 e2e per-AC 케이스로 승격(`auth.py::test_platform_auth_safety_ac1_gate` 무Authorization→401 `missing_token`, `::test_platform_auth_safety_ac7_api_key` 무효 키→401 `invalid_token`·유효 키→tools/list 인가). 신규 파일 `auth-fixture.yaml`·`tests/integration/auth.py` + `ci.yml` 스텝 3개(별 네임스페이스 배포·롤아웃 대기·port 8087 검증), 기존 배포·kustomize 불변. tests/ additive라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변). | ✅41·⬜10·🚫1 (platform AC1·AC7 = ⬜) | ✅43·⬜8·🚫1 (platform AC1·AC7 = ✅ 전용 케이스) |
| 2026-07-30 | pod-describe/AC1(파드 상세 스냅샷)·AC2(대상 지정 방식)·AC3(이벤트 best-effort)을 배포 서버 통합 e2e per-AC 전용 케이스로 승격(`workload.py::test_pod_describe_ac1_snapshot`·`::test_pod_describe_ac2_target_resolution`·`::test_pod_describe_ac3_events_best_effort`). CI가 이미 실행하는 `workload.py::run()`에 케이스 추가(기존 `workload-fixture` 러닝 파드·CI 스텝 port 8081 재사용, 신규 파일·픽스처·`ci.yml` 변경 없음, 부작용 없음). AC2는 name/selector/workload 3경로 해석 + name+selector 상호배타 McpError 거부까지 단언. tests/ additive라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변). | ✅38·⬜13·🚫1 (pod-describe 3 = ⬜) | ✅41·⬜10·🚫1 (pod-describe 3 = ✅ 전용 케이스) |
| 2026-07-29 | grafana-token/AC4(발급자 토큰 비노출)을 배포 서버 응답 .env에 서버측 `GRAFANA_ISSUER_TOKEN`(키·구성값 `glsa_mock_issuer`·발급자 접두 `glsa_`)이 부재하고 단명 read 토큰 `glc_mock_…`만 노출됨을 단언하는 per-AC 전용 e2e 케이스로 승격(`grafana.py::test_grafana_token_ac4_issuer_token_not_exposed`). CI가 이미 실행하는 `grafana.py::run()`에 케이스 추가(발급 호출 재사용, 신규 픽스처·`ci.yml` 변경 없음, 부작용 없음). tests/ additive라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변). | ✅37·⬜14·🚫1 (grafana AC4 = ⬜) | ✅38·⬜13·🚫1 (grafana AC4 = ✅ 전용 케이스) |
| 2026-07-21 | 파괴적 작업 표기 5건(dear-baby-reset-user/AC3·opensearch-document-{put,delete}/AC3·workload-{restart/AC2,scale/AC3})을 배포 서버 `tools/list`의 `destructiveHint=true`·`readOnlyHint=false`를 단언하는 per-AC 전용 e2e 케이스로 승격(파괴 동작 미실행, 메타데이터만). 기존 CI가 실행하는 `workload.py`·`dear_baby.py`·`opensearch.py`에 케이스 추가(+공용 헬퍼 `_helpers.py::assert_destructive_annotation`), 새 파일·`ci.yml` 변경 없음. tests/ additive라 as-is 해시만 변경(prd 불변). | ✅32·⬜19·🚫1 (파괴적 표기 5 = ⬜) | ✅37·⬜14·🚫1 (파괴적 표기 5 = ✅ 전용 케이스) |
| 2026-07-12 | AC↔e2e 1:1 정합성(reconciler) 레지스트리 신설: e2e-only 렌즈로 52 AC 분류, per-AC 케이스 식별 규약 명문화, e2e 보강 backlog·예외 제안 작성. **2026-07-19 사용자 검토 반영 재분류**: 미설정 graceful 거부 6·grafana AC4 출력 비노출 1을 예외→⬜ 보강, 파괴적 표기 5도 `tools/list` 메타데이터를 e2e로 단언하는 ⬜ 보강으로(파괴 동작 미실행), platform AC4만 🚫 e2e 예외로 확정 → **✅32·⬜19·🚫1**. 전용 per-AC 케이스 분리·신설과 정의 예외 개정(1건)은 후속·ratify. | 통합 파일 7개 다중 AC 공유, 인코드 AC 선언 1건, e2e 케이스 규약 부재 | 52 AC 분류(✅32·⬜19·🚫1), 규약·backlog(19)·예외(1)·정의 개정 제안 문서화(tests/ 코드 미변경) |
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
