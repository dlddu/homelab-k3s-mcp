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
| 정책 | `e2e-mocking-policy.md` (E2E 모킹 최소화 정책의 SSOT — 등재·예외의 단일 출처), `comment-policy.md` (주석 비중복성 정책의 SSOT — 복원 경로·유지 대상·판정 절차) |
| 배포 골격 | `index.html`(허브), `reader.html`(마크다운 뷰어), `.nojekyll` |

## 문서 공개 (GitHub Pages)

문서가 서로 연결되어 있어도 레포 안에서만 읽히면 git을 쓰지 않는 사람에게는 없는 것과 같다.
`docs/`를 Pages 배포 루트로 삼아 허브 하나로 전 문서에 도달하게 한다.

| 항목 | 상태 |
|------|------|
| 공개 URL | `https://dlddu.github.io/homelab-k3s-mcp/` |
| Pages 설정 | ⬜ **사용자 작업 대기** — Settings → Pages → Source `Deploy from a branch` → `main` + `/docs` |
| 배포 골격 | ✅ `index.html`(허브) · `reader.html`(뷰어) · `.nojekyll` |
| 허브 도달 가능 문서 | ✅ **40 / 40** (가치 1 + PRD 18 + 테스트 18 + 상태 추적 1 + 정책 2), 끊긴 링크 0 |
| 공개 범위 | 레포가 **public** — `docs/`의 마크다운은 이미 GitHub에서 공개 상태였고, Pages는 그것을 읽기 좋게 서빙할 뿐이다. 새로 공개되는 문서 없음 |
| 비공개 유지 문서 | (없음) |

### 이 레포에 맞춘 뷰어 규약

`reader.html`은 `?doc=<docs 기준 상대경로>.md`를 받아 클라이언트에서 렌더링한다.
절대경로·스킴·`..`·비-`.md` 경로는 거부하고, 문서 안의 `.md` 상대 링크는 `reader.html?doc=`로 재작성한다.

제목 앵커는 **식별자를 그대로 id로 쓴다** — `### AC1: 레플리카 설정` → `#AC1`,
`### V3: 안전한 운영` → `#V3`. 그래서 이 문서와 허브가 특정 AC·가치를 링크로 가리킬 수 있다
(예: `reader.html?doc=prd-workload-scale.md#AC1`). 인라인 코드로 시작하는 제목은 백틱 안 문자열이
id가 되고, 나머지는 일반 슬러그로 떨어진다.

여정 문서·mockup은 이 레포에 없으므로(백엔드 MCP 서버) 뷰어의 "여정 mockup 열기" 요건은 해당 없음.

### 대기·주의 항목

- ⬜ **디자인 시스템 도입 시 허브 재적용** — 이 레포에는 디자인 시스템이 없어 허브·뷰어를
  무채색 최소 구성(시스템 폰트, 명도만 쓰는 토큰)으로 두었다. 브랜드 색을 임의로 고르지 않았다.
- ⚠️ **문서를 추가하면 허브에 한 줄을 함께 추가한다.** 이 단계를 빼먹는 것이 허브 불일치의
  유일한 원인이다. 위 "허브 도달 가능 문서" 수치가 문서 수와 어긋나면 그 자체가 신호다.
- 뷰어는 `fetch`를 쓰므로 `file://`로 직접 열면 동작하지 않는다. 로컬 확인은
  `cd docs && python3 -m http.server` 후 `http://localhost:8000/`.

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
| session_list | V5, V3 | 3 | test-session-list | ✅ 완전 |
| session_read | V5, V3 | 4 | test-session-read | ✅ 완전 |
| session_write | V5, V3 | 5 | test-session-write | ✅ 완전 |
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
  `internal/opensearch`·`internal/sessionplatform` 단위 테스트 + Python 통합
  `tests/integration/`):
  ping, namespace_list, workload_list, workload_logs(전체 — AC3 크래시 루프 previous
  내용은 e2e `crashloop-fixture`), pod_describe(전체),
  workload_restart, workload_scale, dear_baby_reset_user, 자격증명 3종의 발급/스코프/
  비노출(github·grafana AC4)/unavailable,
  opensearch 3종 전 AC(14 — 단위 + `tests/integration/opensearch_*_ac*.py`,
  픽스처는 security off 단일노드 OpenSearch + MinIO STS + 접근 경로 관측용
  `http-trace` 기록 프록시),
  platform AC1·AC2(인증 게이트·디스커버리)·AC5·AC6·AC7·AC8(API 키 게이트·구성 유연성·
  디스커버리 조건부 — `internal/auth/auth_test.go`·`internal/server/auth_routing_test.go`),
  session_list 전 AC(3 — `internal/sessionplatform/sessionplatform_test.go`의 열거·빈 목록·
  수동적 조회(요청은 `GET /api/v1/sessions` 뿐)·unavailable + `internal/server/mcp_test.go`의
  도구 표면·미설정 거부),
  session_read AC1·AC3·AC4와 AC2의 **도구 계층 절반**
  (`internal/sessionplatform/sessionplatform_test.go`의 커서 전체→증분→재읽기·비파괴·
  음수 커서 사전 거부(요청 0건)·404 구분·분기 3종(`active`/`idle->active->read`/
  `snapshot->restore->read`) 노출·unavailable +
  `internal/server/mcp_test.go`의 도구 표면(`readOnlyHint=false`)·인자 검증·not found·미설정 거부),
  session_write AC3·AC5와 AC1·AC2·AC4의 **도구 계층 절반**
  (`internal/sessionplatform/sessionplatform_test.go`의 write→read 왕복(페이로드 바이트 그대로
  전달 + 증분은 **뒤이은 read로만** 관측 = 응답에 출력이 없다는 비블로킹 계약)·분기 3종
  (`active`/`idle->active->write`/`snapshot->restore->write`) 노출·거부 4종 매핑
  (404/413/429/507)과 **제어면 문구를 고정했을 때의 쌍쌍 구별**·거부 후에도 기존 출력 조회 가능·
  빈 id 사전 거부(요청 0건)·unavailable +
  `internal/server/mcp_test.go`의 도구 표면(`destructiveHint=true`·`idempotentHint=false`)·
  인자 검증(거부 시 write 호출 0건)·거부 4종·미설정 거부).
- 🟡 **정적 검증** (매니페스트 리뷰): platform AC3(RBAC 경계 — `k8s/rbac.yaml`),
  platform AC4(하드닝 — `k8s/deployment.yaml`).
- 🔴 **자동화 공백 — 추가 권장**:
  - **session 3종(read 4 · write 5 = 9 AC)의 통합 e2e — 미작성**. 세 도구 모두 구현됐고
    (`internal/sessionplatform` + `internal/mcp`, 도구 표면 17종) Go 단위로 검증되지만,
    `tests/integration/`에 **read·write의 전용 파일이 아직 없다**(아래 e2e 렌즈 레지스트리에서
    9건이 공백으로 계수된다). 이 축의 소유자는 자매 모델 `tbm_homelab-k3s-mcp-ac-e2e`이고,
    남은 것은 파일 저작뿐이다 — **선행 조건은 모두 해소됐다**:
    - 「도구 미구현」은 2026-09-03(`session_list`)·2026-09-04(`session_read`·`session_write`)로
      닫혔다.
    - 「제어면이 클러스터에 없다」는 2026-09-03에 닫혔다: 제어면은 `session-platform`
      네임스페이스에 재배포돼 Deployment `control-plane` 1/1 · Service `control-plane:80`
      (→ 8080)으로 떠 있고, `SESSION_PLATFORM_ENDPOINT`가 `k8s/deployment.yaml`에 배선됐다.
    - 「제어면 스텁이냐 kind의 실 제어면이냐」의 **택일도 이미 내려졌다** — 모킹 정책의 `IMG`
      조항에 따라 **실 제어면**이며, 픽스처 `tests/k8s/kind/session-platform.yaml`이 그것이다
      (근거는 아래 e2e 렌즈 절의 2026-09-04 항목). session-list 3건이 그 결론 위에서
      `session_list_ac{1,2,3}.py`로 이미 저작됐다.
    - 다만 read/write는 목록과 달리 **제어면이 파드를 실제로 프로비저닝하는 경로**(접근=active화,
      스냅샷 복원)를 타므로, 저작 시 픽스처에 `DATA_PLANE_IMAGE` 배선이 필요한지 먼저 판단할 것.
  - opensearch 3종 — **프로덕션 스모크 미수행**(env 배선이 infrastructure/flux-cd-apps
    반영에 걸려 있음). CI 자동화는 완료; 실제 `kubernetes-docs` 컬렉션 대상
    put→search→delete 확인은 배선 완료 후 수행.
  - session_write — **프로덕션 스모크는 수행하지 않는다**(공백이 아니라 의도적 제외).
    `destructiveHint=true`이고 접근만으로 스냅샷 세션을 복원하므로, 프로덕션의 임의 세션을
    대상으로 시험 호출하면 남의 워크로드에 입력을 주입하고 파드를 되살린다. 이 축의 대체
    검증은 위 통합 e2e(자매 렌즈)가 kind의 실 제어면에서 닫는다.

## AC ↔ e2e 1:1 정합성 (reconciler 렌즈)

> **렌즈 차이**: reconciler 정합성 모델(`tbm_homelab-k3s-mcp-ac-e2e`)은 **`tests/integration/`의 통합 e2e만** 검증으로 인정한다 — `internal/`의 Go 단위 테스트는 정의상 e2e가 아니다. 따라서 위 "자동화 커버리지"에서 🟢로 세는 다수 AC가 이 e2e 렌즈에서는 **e2e 공백**으로 계수된다. 이 섹션은 그 e2e-전용 렌즈의 레지스트리다.

### 파일 식별 규약 (규칙 1·2·3·5·6)

> **2026-08-14 개정 — 매칭 단위가 "테스트 케이스"에서 "파일"로 바뀌었다.** 모델 정의(`tbm_homelab-k3s-mcp-ac-e2e`)가 `ac-e2e` 템플릿 고정부에 맞춰 판정 단위를 파일로 옮겼다. 파일 안에서 케이스가 몇 개로 쪼개져 있는지는 이제 판정과 **무관**하다. 케이스 단위 시절에 쌓인 per-AC 케이스는 그대로 자산이며, 분할은 "새 검증 작성"이 아니라 **케이스를 파일로 승격**하는 작업이다.

- **규칙 1 (AC→파일)**: 예외 목록에 없는 모든 AC는 자신을 주검증하는 파일을 **정확히 하나** 가진다. 여러 AC를 겸하는 파일은 그 AC의 전용 파일이 아니므로, 겸용 상태의 AC는 여전히 **공백**으로 계수한다.
- **규칙 2 (파일→AC)**: 모든 매칭 단위 파일은 **정확히 하나의 AC**만 주검증 대상으로 선언한다. 2개 이상을 선언한 파일은 **분할 대기**(규칙 2 위반)다.
- **규칙 3 (식별)**: 매칭 단위 파일은 **모듈 docstring**에 `검증 AC: <domain>/AC<n>` 을 선언한다. AC 대신 스모크/인프라를 검증하는 파일은 `검증 AC: 없음 (스모크/인프라)` 을 선언하고 아래 "비-AC 파일" 목록에 등재한다. 어디에도 매핑되지 않은 파일은 고아다.
- **매칭 단위**: `tests/integration/` 최상위 `*.py`. 단 **`_` 접두 공유 모듈**(`_helpers.py` · `_workload.py` · `_auth_variant.py` · `_opensearch.py` · `_aws_config.py`)과 하네스 자신(`run_all.py` · `check_ac_mapping.py`)은 매칭 단위가 아니다 — 제외 판정은 러너와 체커가 `run_all.py::matching_unit_paths()` 하나로 공유한다.
- **기계 검사**: `python3 tests/integration/check_ac_mapping.py` 가 위 규칙과 아래 집계를 CI(`fmt + vet` 잡)에서 강제한다. 이 표의 행별 상태·집계 숫자가 실측과 **정확히** 같아야 통과하므로, 파일을 쪼개거나 AC를 추가한 PR은 같은 PR에서 이 절을 갱신해야 한다.
- **실행 하네스**: `tests/integration/run_all.py` 가 매칭 단위 파일을 자동 발견해 각 파일이 신고한 `실행 대상`(primary · auth-variant)별로 실행한다. CI는 파일을 이름으로 나열하지 않으므로 분할할 때마다 `ci.yml` 을 고칠 필요가 없고, 체커가 "매칭 단위 파일 전부가 정확히 한 번 배차된다"와 "각 파일의 `run()` 이 그 파일이 정의한 `test_*` 케이스를 전부 호출한다"를 검사해, **만들어 놓고 실행되지 않는 파일**과 **배차는 되지만 아무것도 단언하지 않고 통과하는 파일**을 둘 다 구조적으로 막는다.

<!-- ac-e2e-집계 -->
- AC 전집: 64
- 예외 등재: 1
- 1:1 대상: 63
- 매칭 파일(전용): 54
- 분할 대기 파일(규칙 2 위반): 0
- 공백 AC: 9
<!-- /ac-e2e-집계 -->

> 공백 9건의 내역: **분할 대기(규칙 2 위반)는 0건**이고, 남은 9건은 전부 **케이스 자체가 없는** backlog(아래)다. 규칙 2 위반이 소멸했으므로 잔여 공백을 줄이는 길은 이제 분할이 아니라 **신규 전용 파일 저작**뿐이다.
>
> **분할 대기 0 달성(2026-09-03)** — 마지막 겸용 파일 `workload.py`(16 AC)의 선결 판단이었던 「케이스가 공유 픽스처 상태에 순서 의존적」은 이 원장이 지목한 방식, 즉 **각 파일이 자기 선행 조건을 스스로 성립시키는 것**으로 해소했다(아래 완료 노트). 러너의 `실행 순서:` 로 파일 간 순서를 고정하는 길은 결합을 파일 단위로 옮길 뿐 없애지 않으므로 채택하지 않았고, 그 증거로 신규 16개 파일 중 **어느 것도 `실행 순서:` 를 선언하지 않는다**.
>
> 2026-08-31 슬라이스가 나머지 3개의 선결 판단을 확정하고 분할했다 — **`auth-variant` 배차 증가**(2 → 9)는 수용했고(포트포워드는 재시도 루프로 그룹 내내 유지되고 각 파일이 `wait_for_healthz` 로 시작하므로 배선이 바뀌지 않는다. 늘어나는 비용은 파일당 파이썬 기동 + 세션 개설뿐이다), **`smoke.py` 의 잔여 도구 표면 확인**은 규칙 3의 **비-AC 파일로 등재**했다(아래 「비-AC 파일」 절).

### AC 레지스트리 (64) — ✅ 전용 파일 54 · ⬜ 분할 대기 0 · ⬜ 공백(케이스 없음) 9 · 🚫 예외 1

| AC | 제목 | e2e 상태 |
|----|------|----------|
| aws-config-get/AC1 | 고정 객체 조회 | ✅ 전용 파일 `aws_config_get_ac1.py` |
| aws-config-get/AC2 | 정적 키 미사용 | ✅ 전용 파일 `aws_config_get_ac2.py` |
| aws-config-get/AC3 | 미설정 시 graceful 거부 | ✅ 전용 파일 `aws_config_get_ac3.py` |
| dear-baby-reset-user/AC1 | 온보딩 리셋 실행 | ✅ 전용 파일 `dear_baby_reset_user_ac1.py` |
| dear-baby-reset-user/AC2 | 명시적 대상 지정 | ✅ 전용 파일 `dear_baby_reset_user_ac2.py` |
| dear-baby-reset-user/AC3 | 파괴적 작업 표기 | ✅ 전용 파일 `dear_baby_reset_user_ac3.py` |
| github-app-installation-token/AC1 | 단명 설치 토큰 발급 | ✅ 전용 파일 `github_app_installation_token_ac1.py` |
| github-app-installation-token/AC2 | 스코프 제한 | ✅ 전용 파일 `github_app_installation_token_ac2.py` |
| github-app-installation-token/AC3 | 미설정 시 graceful 거부 | ✅ 전용 파일 `github_app_installation_token_ac3.py` |
| github-app-installation-token/AC4 | 베이스 키 비노출 | ✅ 전용 파일 `github_app_installation_token_ac4.py` |
| grafana-token/AC1 | read-only 토큰 발급 | ✅ 전용 파일 `grafana_token_ac1.py` |
| grafana-token/AC2 | 즉시 사용 가능한 형태 | ✅ 전용 파일 `grafana_token_ac2.py` |
| grafana-token/AC3 | 미설정 시 graceful 거부 | ✅ 전용 파일 `grafana_token_ac3.py` |
| grafana-token/AC4 | 발급자 토큰 비노출 | ✅ 전용 파일 `grafana_token_ac4.py` |
| namespace-list/AC1 | 네임스페이스 열거 | ✅ 전용 파일 `namespace_list_ac1.py` |
| opensearch-document-delete/AC1 | 단일 문서 삭제 | ✅ 전용 파일 `opensearch_document_delete_ac1.py` |
| opensearch-document-delete/AC2 | 부재 문서의 명확한 처리 | ✅ 전용 파일 `opensearch_document_delete_ac2.py` |
| opensearch-document-delete/AC3 | 파괴적 작업 표기 | ✅ 전용 파일 `opensearch_document_delete_ac3.py` |
| opensearch-document-delete/AC4 | AssumeRole·SigV4 접근 | ✅ 전용 파일 `opensearch_document_delete_ac4.py` |
| opensearch-document-delete/AC5 | 미설정 시 graceful 거부 | ✅ 전용 파일 `opensearch_document_delete_ac5.py` |
| opensearch-document-put/AC1 | 문서 색인·업서트 | ✅ 전용 파일 `opensearch_document_put_ac1.py` |
| opensearch-document-put/AC2 | 인덱스 자동 생성 | ✅ 전용 파일 `opensearch_document_put_ac2.py` |
| opensearch-document-put/AC3 | 파괴적 작업 표기 | ✅ 전용 파일 `opensearch_document_put_ac3.py` |
| opensearch-document-put/AC4 | AssumeRole·SigV4 접근 | ✅ 전용 파일 `opensearch_document_put_ac4.py` |
| opensearch-document-put/AC5 | 미설정 시 graceful 거부 | ✅ 전용 파일 `opensearch_document_put_ac5.py` |
| opensearch-search/AC1 | 질의 검색 | ✅ 전용 파일 `opensearch_search_ac1.py` |
| opensearch-search/AC2 | 결과 상한 | ✅ 전용 파일 `opensearch_search_ac2.py` |
| opensearch-search/AC3 | AssumeRole·SigV4 접근 | ✅ 전용 파일 `opensearch_search_ac3.py` |
| opensearch-search/AC4 | 미설정 시 graceful 거부 | ✅ 전용 파일 `opensearch_search_ac4.py` |
| ping/AC1 | 항상 pong 응답 | ✅ 전용 파일 `ping_ac1.py` |
| platform-auth-safety/AC1 | 인증 게이트 | ✅ 전용 파일 `platform_auth_safety_ac1.py` |
| platform-auth-safety/AC2 | 인증 디스커버리 | ✅ 전용 파일 `platform_auth_safety_ac2.py` |
| platform-auth-safety/AC3 | 최소권한 RBAC 경계 | ✅ 전용 파일 `platform_auth_safety_ac3.py` |
| platform-auth-safety/AC4 | 하드닝된 런타임 | 🚫 예외 |
| platform-auth-safety/AC5 | 서버 수준 graceful degradation | ✅ 전용 파일 `platform_auth_safety_ac5.py` |
| platform-auth-safety/AC6 | 헬스·레디니스 | ✅ 전용 파일 `platform_auth_safety_ac6.py` |
| platform-auth-safety/AC7 | API 키 인증 | ✅ 전용 파일 `platform_auth_safety_ac7.py` |
| platform-auth-safety/AC8 | 인증 방식 구성 유연성 | ✅ 전용 파일 `platform_auth_safety_ac8.py` |
| pod-describe/AC1 | 파드 상세 스냅샷 | ✅ 전용 파일 `pod_describe_ac1.py` |
| pod-describe/AC2 | 대상 지정 방식 | ✅ 전용 파일 `pod_describe_ac2.py` |
| pod-describe/AC3 | 이벤트 best-effort | ✅ 전용 파일 `pod_describe_ac3.py` |
| session-list/AC1 | 세션 열거 | ✅ 전용 파일 `session_list_ac1.py` |
| session-list/AC2 | 상태를 바꾸지 않는 조회 | ✅ 전용 파일 `session_list_ac2.py` |
| session-list/AC3 | 미설정 시 graceful 거부 | ✅ 전용 파일 `session_list_ac3.py` |
| session-read/AC1 | 오프셋 커서 읽기 | ⬜ 공백 — 케이스 없음 |
| session-read/AC2 | 상태 분기 노출 | ⬜ 공백 — 케이스 없음 |
| session-read/AC3 | 대상 부재·잘못된 커서 처리 | ⬜ 공백 — 케이스 없음 |
| session-read/AC4 | 미설정 시 graceful 거부 | ⬜ 공백 — 케이스 없음 |
| session-write/AC1 | 워크로드 입력 주입 | ⬜ 공백 — 케이스 없음 |
| session-write/AC2 | 상태 분기 처리와 노출 | ⬜ 공백 — 케이스 없음 |
| session-write/AC3 | 파괴적 작업 표기 | ⬜ 공백 — 케이스 없음 |
| session-write/AC4 | 거부 응답의 구분 전달 | ⬜ 공백 — 케이스 없음 |
| session-write/AC5 | 미설정 시 graceful 거부 | ⬜ 공백 — 케이스 없음 |
| workload-list/AC1 | 종류별 워크로드 조회 | ✅ 전용 파일 `workload_list_ac1.py` |
| workload-list/AC2 | 네임스페이스 스코프 | ✅ 전용 파일 `workload_list_ac2.py` |
| workload-logs/AC1 | 워크로드 기준 로그 조회 | ✅ 전용 파일 `workload_logs_ac1.py` |
| workload-logs/AC2 | tail 라인 제어 | ✅ 전용 파일 `workload_logs_ac2.py` |
| workload-logs/AC3 | 크래시 루프 후 직전 로그 | ✅ 전용 파일 `workload_logs_ac3.py` |
| workload-logs/AC4 | 컨테이너 선택과 필터 | ✅ 전용 파일 `workload_logs_ac4.py` |
| workload-restart/AC1 | 롤링 재시작 트리거 | ✅ 전용 파일 `workload_restart_ac1.py` |
| workload-restart/AC2 | 파괴적 작업 표기 | ✅ 전용 파일 `workload_restart_ac2.py` |
| workload-scale/AC1 | 레플리카 설정 | ✅ 전용 파일 `workload_scale_ac1.py` |
| workload-scale/AC2 | DaemonSet 거부 | ✅ 전용 파일 `workload_scale_ac2.py` |
| workload-scale/AC3 | 파괴적 작업 표기 | ✅ 전용 파일 `workload_scale_ac3.py` |

### ⬜ 공백 backlog (9) — 케이스 자체가 없는 AC, 전용 **파일** 신설 필요

> 새 통합 e2e는 kind 클러스터 실서버 배포로 실행되므로 앱 구동 검증이 필요 — 후속 task로 저작한다.

- **session-read/AC1~AC4 · session-write/AC1~AC5 (9)** → AC별 전용 파일 9개(신규, 파일 단위 규칙 2). 미설정 거부 2건(`session-read/AC4` · `session-write/AC5`)은 각자의 전용 파일 `session_{read_ac4,write_ac5}.py`가 되며, `실행 대상: auth-variant`(port 8088, 자격증명 미부착 변형)를 선언해 다른 "미설정 거부" 파일들과 같은 배포에서 돈다 — `session_list_ac3.py`가 그 자리의 선례다. **선행 조건이던 구현은 2026-09-04로 전부 해소됐다**: `session_read`(PR #51)에 이어 `session_write`도 착지해 자매 모델 `tbm_homelab-k3s-mcp-docs-impl`이 자기 몫을 닫았고(도구 표면 17종), 이제 남은 것은 **이 렌즈의 파일 저작뿐**이다 — 규칙 4가 "도구·기능 미구현은 예외 사유가 아니다"라고 못박으므로 그동안에도 계수에서 빠지지 않고 여기 공백으로 남아 있었다. e2e 픽스처도 이미 서 있다: `tests/k8s/kind/session-platform.yaml`이 실 제어면을 띄우고 `tests/integration/_session_platform.py`가 그 상태 저장소를 시드한다. 다만 read/write는 목록과 달리 **제어면이 파드를 실제로 프로비저닝하는 경로**(접근=active화, 스냅샷 복원)를 타므로, 저작 시 픽스처에 `DATA_PLANE_IMAGE` 배선이 필요한지를 먼저 판단할 것 — 지금은 세션 생성 경로를 쓰지 않아 일부러 비워 두었다.

> **platform 2건(AC2 디스커버리 · AC8 구성 유연성) → AC별 전용 파일 2개 — ✅ 완료(2026-09-04)**: backlog 11건 중 **선행이 구현이 아니라 저작뿐인 2건**을 전용 파일로 닫았다(`platform_auth_safety_ac{2,8}.py`). 매칭 파일 **52 → 54**, 공백 **11 → 9**, 규칙 2 위반 **0 유지**. 남은 9건은 전부 session-read 4 · session-write 5다. 그 선행이던 「도구 미구현」은 같은 날 자매 모델 `tbm_homelab-k3s-mcp-docs-impl`이 `session_read`·`session_write`를 착지시켜 닫았으므로(도구 표면 17종), 9건에 남은 것은 이 렌즈의 파일 저작뿐이다.
>
> **왜 지금인가**: 두 AC의 구현은 오래전부터 있었고(`internal/auth/auth.go`가 `MCP_OAUTH_*`를 처리하고 `internal/server/server.go`가 OAuth 구성 시에만 디스커버리 라우트를 건다) 빠진 것은 e2e 파일뿐이었다. 직전 슬라이스가 이 둘을 범위 밖으로 끊은 사유는 선행 미충족이 아니라 **작업 성격의 차이**였다 — 「한 PR에 섞으면 `ci.yml`을 두 방향으로 동시에 건드린다」. 그 슬라이스가 머지된 지금 그 사유는 소멸했다. 두 AC는 같은 난제(**발급자가 없으면 서버가 기동조차 하지 않는다** — `auth.FromEnv`가 `openid-configuration`을 가져와 `jwks_uri`가 없으면 실패한다)를 공유하므로 한 슬라이스로 묶었다.
>
> **발급자는 스텁이 아니라 실 IdP(dex)다 — 모킹 허용목록·상한 5는 불변.** 이것이 이 슬라이스의 핵심 판단이고, 근거는 `docs/e2e-mocking-policy.md`가 자기 카테고리로 스스로 배제한다는 것이다: `UPS`는 「실 상류를 CI에서 띄울 수 있으면 쓸 수 없다」, `IMG`는 「이미지가 공개되면 소멸한다」인데 `ghcr.io/dexidp/dex`는 **공개 이미지에 linux/arm64 매니페스트**를 갖는다. 즉 정적 JSON 스텁 발급자는 **어느 카테고리에도 걸리지 않는 등재 불가 모킹**이 된다. 정책의 「실 구현체를 띄운 것은 모킹이 아니다」(MinIO가 S3를, 단일노드 OpenSearch가 Serverless를 대신하는 자리)와 09-04 session-list 슬라이스가 실 제어면을 고른 선례가 같은 방향을 가리킨다. dex 구성은 `issuer`·`storage`·`web`(`cmd/dex/config.go::Validate`)에 더해 **커넥터 하나**가 필요하다 — `server.NewServer`가 커넥터 0개면 「server: no connectors specified」로 기동을 거부한다. 그래서 `enablePasswordDB: true`로 내장 local 커넥터 하나를 켠다(정적 비밀번호는 두지 않아 실제로 로그인할 수 있는 주체는 없다 — 이 AC들에 필요한 것은 디스커버리와 키 집합뿐이고 대화형 흐름은 아니다). 이미지는 다이제스트로 핀했다.
>
> **AC8은 (a)만이 아니라 4구성 전부를 덮는다.** AC 본문이 (a)~(d)를 명시하는데 파일이 4분의 1만 단정하면 이 레포가 08-07·08-13에 되돌아와 고쳤던 「반쪽 단정」이 하나 더 생긴다. 발급자 픽스처를 세우는 이 슬라이스에서 (b)(c)는 배포 변형 하나씩, (d)는 픽스처 없이 관측되므로 **지금이 4구성을 덮는 가장 싼 순간**이었다. 각 구성은 관측 가능한 표면까지 단정한다 — (a) 401 + 챌린지에 `resource_metadata` 없음 + 디스커버리 404, (b) 디스커버리 200 + Available, (c) API 키 인가 + 디스커버리 200 + 챌린지에 `resource_metadata` 있음, (d) `availableReplicas` 0 + 종료 코드 1 + 로그에 `no authentication configured`.
>
> **(d)가 AC2의 논증을 떠받친다**: 서버 쪽 「JWKS 동적 로드」는 배포가 Available 하다는 사실이 증거인데(실패하면 `main.go`가 `os.Exit(1)`), 그 추론은 그 경로가 **실제로 치명적일 때만** 공허하지 않다. 자격증명을 하나도 주지 않은 변형이 실제로 기동에 실패하는 것을 같은 PR의 AC8 (d)가 관측하므로, 두 파일은 서로의 근거가 된다.
>
> **AC2는 사슬을 실제로 걷는다**: 401 챌린지가 준 `resource_metadata` 주소 → 그 문서가 지목한 `authorization_servers[0]` → 그 발급자의 `openid-configuration`이 준 `jwks_uri` → 쓸 수 있는 RSA 키. 각 칸이 앞 칸이 준 값으로만 이어지므로 중간이 낡으면 그 자리에서 끊긴다. `MCP_OAUTH_RESOURCE`를 audience와 다른 값으로 배포해 `resource == "" → audience` 폴백과 구분되게 했다.
>
> **범위 밖(후속)**: dex에서 실 토큰을 받아 「유효 JWT → 인가」까지 단정하는 것. dex에 정적 클라이언트·password DB·bcrypt 해시를 붙여야 하고, AC2의 검증 방법은 「표준 클라이언트가 이 문서로 인증을 **자동 구성**할 수 있다」이지 「인증에 성공한다」가 아니다. 붙이면 AC1의 "유효 토큰 → 정상 처리" 절과 AC8 (c)의 OAuth 절을 함께 강화하는 것이 자연스럽다.
>
> **`ci.yml` 변경은 픽스처 배포·롤아웃 대기·그룹 실행·진단 덤프 4자리**다. 러너에 세 번째 그룹 `oauth-variant`(포트 8089)가 생겼고 — 디스커버리 라우트는 OAuth가 구성된 배포에만 걸리므로 기존 두 그룹 어디에서도 AC2를 관측할 수 없다 — `homelab-k3s-mcp-no-auth`에는 **일부러 롤아웃 대기를 걸지 않았다**(기동 실패가 의도라 걸면 CI가 거기서 멈춘다). 네 구성을 대조하는 것이 AC8 자체라 그 파일만은 자기 그룹의 배포 하나로 부족해, 필요한 순간에만 짧게 여는 `kubectl port-forward`를 `_oidc.py`에 두었다(apiserver 서비스 프록시는 `Authorization` 헤더를 덮어써 API 키를 실을 수 없다).

> **session-list 3건 → AC별 전용 파일 3개 — ✅ 완료(2026-09-04)**: backlog 14건 중 **선행 조건이 이번에 해소된 3건**(session-list/AC1·AC2·AC3)을 전용 파일로 저작했다(`session_list_ac{1,2,3}.py`). 매칭 파일 **49 → 52**, 공백 **14 → 11**, 규칙 2 위반 **0 유지**. 남은 11건은 platform 2(AC2 디스커버리·AC8 구성 유연성) + session-read/write 9다. 후자의 선행이던 「도구 미구현」은 같은 날 자매 모델이 `session_read`·`session_write`를 착지시켜 닫혔으므로, 9건에 남은 것은 이 렌즈의 파일 저작뿐이다.
>
> **왜 지금인가**: 원장이 적어 둔 두 선행(「도구 미구현」·「session-platform 배포가 클러스터에서 제거됨」)을 2026-09-03의 `session_list` 구현 PR이 각각 구현과 정정으로 닫았고, 같은 PR이 「제어면 스텁 또는 kind에 띄운 제어면 중 택일은 **그 렌즈의 몫이다**」라며 이 셋의 e2e 저작을 여기에 이름으로 배정했다.
>
> **택일의 답은 정책이 이미 정해 두었다 — 실 제어면이다.** `docs/e2e-mocking-policy.md`의 `IMG`는 "실 구성요소의 이미지·데이터를 CI에서 확보할 수 없어"를 요건으로 하고 "이미지가 공개되거나 CI에서 당길 수 있게 되면 이 카테고리는 소멸하고 실 구성요소로 대체해야 한다"고 명시한다. `ghcr.io/dlddu/session-platform`은 **공개 패키지**이고(session-platform 레포의 `k8s/deployment.yaml`이 "GHCR 패키지는 공개라 imagePullSecret 은 필요 없다"고 적고, 익명 pull이 실제로 성공한다) **linux/arm64 매니페스트**가 있어 `ubuntu-24.04-arm` 러너의 kind 노드가 그대로 당긴다. `UPS`(외부 SaaS·유료 계정)도 해당하지 않는다. 그래서 신규 픽스처 `tests/k8s/kind/session-platform.yaml`은 **모킹 지점이 아니고, 허용목록은 5건·상한 5로 불변**이다.
>
> **세션은 제어면의 실 상태 저장소에 시드한다**: 제어면의 `List`는 소유 라벨이 붙은 `session-<id>` ConfigMap만 읽고 **파드를 조회하지 않으므로**, ConfigMap을 만들면 실 제어면의 실 API·실 디코딩·실 정규화가 그대로 돈다(MinIO를 `minio-seed` Job으로 시드하는 이 하네스의 선례와 같은 자리다). 제품 API로 세션을 만드는 길은 쓰지 않았다 — 그 경로는 데이터 플레인 파드를 프로비저닝하고, `idle`/`snapshot` 상태는 60분 유휴 또는 CRIU 체크포인트로만 도달하는데 그 어느 것도 session-list가 검증하는 대상이 아니다. 시드 JSON의 필드는 손으로 짓지 않고 `session.Session`의 구조체 태그에서 확인했다.
>
> **실 제어면이 스텁보다 강한 지점이 AC2에서 드러난다**: AC2의 "새 파드가 기동되지 않음" 절은 스텁 앞에서는 vacuous하다(스텁은 애초에 파드를 만들지 않으므로 도구가 복원을 유발했더라도 파드 수는 그대로다). 실 제어면은 snapshot 세션을 복원하면 실제로 파드를 만들기 때문에 파드 집합 불변이 진짜 판별자가 된다. `session_list_ac2.py`는 그 집합을 첫 호출 **전에** 떠서 3회 호출 뒤와 대조한다.
>
> **각 파일이 자기 선행 조건을 스스로 성립시킨다**: AC1은 제어면의 두 가지 상태(세션 있음/없음)를 요구하는데, 파일 간 실행 순서로 만들지 않고 한 파일 안에서 재고를 비운 뒤 시드한다. 신규 3파일 중 어느 것도 `실행 순서:`를 선언하지 않는다 — 직전 슬라이스가 확립한 규칙 그대로다.
>
> **`ci.yml` 변경은 픽스처 배포·롤아웃 대기·진단 덤프 3자리뿐**이다. 러너가 파일을 자동 발견해 배차하므로 파일별 스텝은 없다(primary 41 → 43, auth-variant 9 → 10).
>
> **곁가지 하나를 함께 고쳤다 — `_helpers.EXPECTED_TOOLS` 14 → 15.** `internal/mcp/toolslist.go`의 도구 표면은 `session_list` 추가로 15개가 됐는데 상수는 14개로 남아 있었다. 두 소비자(`smoke.py`·`platform_auth_safety_ac5.py`)의 단정이 `EXPECTED_TOOLS - names`(**부분집합**)이라 CI는 통과했다 — **깨진 게 아니라 조용히 약해진** 상태이고 체커도 러너도 이 상수를 보지 않는다. 그 도구를 실제로 구동하는 이 슬라이스가 고칠 자리다.

> **`workload.py`(16 AC) → AC별 전용 파일 16개 — ✅ 완료(2026-09-03)**: 마지막 분할 대기 파일을 쪼개 **규칙 2 위반을 0으로** 만들었다(`namespace_list_ac1.py` · `platform_auth_safety_ac3.py` · `pod_describe_ac{1,2,3}.py` · `workload_list_ac{1,2}.py` · `workload_logs_ac{1,2,3,4}.py` · `workload_restart_ac{1,2}.py` · `workload_scale_ac{1,2,3}.py`). 매칭 파일 **33 → 49**, 규칙 2 위반 **1 → 0**, 공백 **30 → 14**(겸용 커버 16 → 0, 케이스 없음 14는 불변). 이로써 잔여 공백 14건은 전부 backlog(platform 2 · session 12)이고, 이 렌즈에서 **분할로 닫을 수 있는 gap은 남아 있지 않다**.
>
> **선결 판단(순서 의존)을 어떻게 끊었는가**: 겸용 시절 `run()` 은 케이스를 고정된 순서로 불렀다 — 읽기 전용 조회 → restart → scale(레플리카를 1로 되돌리고 기다린다) → 그 레플리카를 필요로 하는 logs·pod_describe. 러너가 파일별 프로세스로 돌리므로 그 순서를 파일 경계 너머로 옮길 수는 없고, `실행 순서:` 로 고정하는 것은 결합을 옮길 뿐이다. 그래서 **픽스처를 읽거나 변형하는 파일이 자기 선행 조건을 스스로 성립시킨다**: `_workload.py::ensure_workload_fixture_baseline()` 이 `deploy/workload-fixture` 를 매니페스트가 선언한 기준선으로 멱등하게 되돌린다(`.spec.replicas` 가 1이 아니면 `kubectl scale`, 그다음 `rollout status`, 그다음 **셀렉터에 매칭되는 파드가 정확히 하나이고 Ready** 가 될 때까지 폴링). 마지막 조건이 이 슬라이스의 **유일한 신규 로직**이고 필요한 이유는 `rollout status` 가 구 파드의 Terminating 을 기다려 주지 않는 반면 `pod_describe` 는 셀렉터로 파드 **하나**를 고른다는 것이다 — 죽어 가는 파드를 골라 `Running`·`ready` 단정이 flake 할 수 있다. 이 헬퍼는 `workload_scale` 도구가 아니라 kubectl 로 되돌린다(시험 대상 도구로 세운 선행 조건은 그 도구의 오동작을 감춘다).
>
> **어느 파일이 무엇을 선행 조건으로 갖는지**: ① 픽스처 기준선을 요구하는 **9개** — `workload_list_ac{1,2}` · `workload_restart_ac1` · `workload_scale_ac1` · `workload_logs_ac{2,4}` · `pod_describe_ac{1,2,3}`. ② 서버만 필요한 **6개** — `namespace_list_ac1`(네임스페이스 오브젝트는 레플리카와 무관) · `workload_scale_ac2`(DaemonSet 거부) · `workload_restart_ac2` · `workload_scale_ac3`(둘 다 `tools/list` 메타데이터만) · `workload_logs_ac{1,3}`(크래시루프 픽스처의 전제를 `wait_for_crashloop_*` 멱등 폴링으로 스스로 성립시킨다 — 이 결합은 처음부터 없었다). ③ 서버도 필요 없는 **1개** — `platform_auth_safety_ac3`(실제로 바인딩된 ClusterRole + apiserver SubjectAccessReview 만 읽어 세션을 열지 않는다). 선행 조건은 컨텍스트 매니저에 숨기지 않고 각 파일 `run()` 에 한 줄로 드러나 있어, 9라는 수를 grep 으로 셀 수 있다.
>
> **순수 이동임을 주장하지 않고 증명했다**: 케이스 함수·상수를 손으로 옮기지 않고 원본의 ast 소스 세그먼트(선행 주석 블록 포함)를 떠서 생성한 뒤, 원본(`HEAD`)과 신규 파일의 top-level 정의를 `ast.dump` 로 대조해 **정의 39개 중 36개가 정확히 한 파일에 AST 동일하게** 착지함을 확인했다. 나머지 셋은 각각 선언된 예외다 — (a) `run` 1개는 파일별 디스패처 16개로 재작성, (b) `test_workload_scale_ac1_replica_count` 의 docstring 한 문장(“the log and pod_describe cases later in run()”)은 분할 후 거짓이 되므로 고쳤고 **docstring 노드를 제거한 AST 는 동일**함을 따로 대조했다(단언 무변경), (c) 죽은 상수 `EXEC_NAMESPACE`(정의만 있고 참조 0건) 1개는 제거했다. 신규 정의는 `FIXTURE_REPLICAS` · `wait_for_single_ready_pod` · `ensure_workload_fixture_baseline` 셋뿐이다.
>
> **공유 표면은 `_workload.py` 로 내렸다**: 픽스처 이름·마커 계약(`NAMESPACE` · `WORKLOAD` · `STS_WORKLOAD` · `DS_WORKLOAD` · `CRASHLOOP_WORKLOAD` · `CRASHLOOP_MARKER` · `RECOVERED_MARKER` · `SERVER_NAMESPACE`)과 kubectl·롤아웃 관측 헬퍼 5개, 그리고 위 선행 조건 헬퍼. RBAC 경계 단위(`EXPECTED_GRANT` · `FORBIDDEN_PROBES` · `live_cluster_role_grant` · `can_i` + 신원 상수 2개)는 소비자가 하나라 `platform_auth_safety_ac3.py` 에 함께 두었고, 케이스 국소 상수(`RESTART_ANNOTATION_PATH` · `LOG_TIMESTAMP_RE`)도 각자의 파일에 남겼다.
>
> **신규 픽스처·신규 CI 스텝·`ci.yml` 변경 없음** — 러너가 파일을 자동 발견해 배차하므로 분할은 파일을 더하고 빼는 것으로 끝난다. primary 그룹 배차는 **26 → 41**, auth-variant 는 **9 불변**. 늘어나는 비용은 파일당 파이썬 기동 + 세션 개설 + (9개 파일에서) kubectl 조회 두 번이다.

> **`no_config.py`(7) · `auth.py`(2) · `smoke.py`(2) → AC별 전용 파일 11개 + 비-AC 파일 1개 — ✅ 완료(2026-08-31)**: 분할 대기 4개 파일 중 **공유 가변 상태가 없어 프로세스가 갈라져도 관측이 바뀌지 않는 셋**을 골라 쪼갰다(`platform_auth_safety_ac{1,5,6,7}.py` · `ping_ac1.py` · `aws_config_get_ac3.py` · `github_app_installation_token_ac3.py` · `grafana_token_ac3.py` · `opensearch_{search_ac4,document_put_ac5,document_delete_ac5}.py`). 매칭 파일 **22 → 33**, 규칙 2 위반 **4 → 1**, 규칙 1 위반(공백) **41 → 30**(겸용 커버 27 → 16, 케이스 없음 14는 불변). 비-AC 파일 **0 → 1**.
>
> **왜 이 셋인가**: 세 파일의 케이스는 전부 읽기 전용이다 — 인증 없는/잘못된 `POST /mcp`의 401, `/healthz`·`/readyz`, `tools/list`, 그리고 "미설정 거부 + 직후 `ping`이 여전히 `pong`". 어느 것도 클러스터나 서버 상태를 변형하지 않으므로 파일이 나뉘어 프로세스가 갈라져도 서로를 관측할 수 없다. 직전 슬라이스의 기준("선결 판단 없이 순수 이동")과 다른 기준을 쓴 것은 이 셋에는 선결 판단이 **있었기** 때문이고, 그 둘을 아래처럼 확정했다. 남은 `workload.py`(16)는 restart → scale → logs/pod_describe가 실제로 공유 픽스처를 변형하므로 섞지 않았다.
>
> **확정한 선결 판단 ①(`no_config.py`·`auth.py`) — `auth-variant` 배차 증가를 수용한다.** 그룹 배차가 2 → 9로 늘지만 `ci.yml`은 그룹당 스텝 하나로 러너를 부르고 포트포워드를 **재시도 루프로 그룹 내내 유지**하며 각 파일이 `wait_for_healthz`로 시작하므로, 프로세스가 늘어도 배선·픽스처·롤아웃 대기는 한 줄도 바뀌지 않는다. 늘어나는 비용은 파일당 파이썬 기동과 MCP 세션 개설뿐이고, 그 대가로 실패가 어느 AC의 것인지 파일 이름으로 즉시 드러난다.
>
> **확정한 선결 판단 ②(`smoke.py`) — 잔여 도구 표면 확인을 규칙 3의 비-AC 파일로 등재한다.** 규칙 3이 허용 유형으로 "도구 표면 존재 확인"을 문자 그대로 지목하고, 이 확인은 `실행 순서: 0`으로 primary 그룹 맨 앞에서 돌아 배포가 깨졌을 때 26개 파일이 차례로 모호하게 죽는 대신 한 번에 원인을 말한다. **등재 0건이던 규칙 3 경로가 이번에 처음 양성 사례를 갖는다** — 체커는 등재 누락(고아 파일)과 고아 등재를 양방향으로 검사하는데, 지금까지 그 코드가 참인 경우로 실행된 적이 없었다.
>
> **순수 이동임을 주장하지 않고 증명했다**: 케이스 함수·상수를 손으로 옮기지 않고 원본의 ast 소스 세그먼트를 떠서 생성한 뒤, 원본(`HEAD`)과 신규 파일의 top-level 정의를 `ast.dump`로 대조해 **19개 정의 전부가 정확히 한 파일에 동일하게 착지**함을 확인했다. 재작성이 허용된 것은 파일별 `run()` 디스패처 3개와 아래 도구 표면 상수 1개뿐이며, 단언은 한 줄도 바뀌지 않았다.
>
> **공유 표면은 `_` 접두 모듈로 내렸다**: `_auth_variant.py`(`API_KEY` + 거부 텍스트 4개 + `assert_unavailable_refusal`). `run_all.py::matching_unit_paths()`가 `_` 접두를 걸러내므로 매칭 단위가 아니고 `검증 AC:` 선언을 갖지 않는다. `API_KEY`는 분할 전 `auth.py`·`no_config.py`에 같은 리터럴로 중복돼 있던 것을 한 벌로 합친 것이다.
>
> **stale 상수 하나를 정정했다(유일한 단언 변경)**: `smoke.py`의 도구 표면 상수는 14개 중 **9개짜리 부분집합**이라, primary 그룹의 케이스들이 실제로 구동하는 `github_app_installation_token`·`aws_config_get`·`opensearch_*`가 빠져 있었다. 도구 표면은 `internal/mcp/toolslist.go`가 구성과 무관하게 정적으로 선언하므로 두 배포에서 같아야 한다 — `_helpers.EXPECTED_TOOLS`(14개, `no_config.py` 원본과 AST 동일) 한 벌로 합쳐 `platform_auth_safety_ac5.py`(변형)와 `smoke.py`(주 배포)가 함께 읽는다.
>
> **체커에 하네스 검사 하나를 더했다(유일한 게이트 확장)**: 파일 하나에 케이스 하나인 구조가 되면 디스패처가 케이스를 부르는 줄을 빠뜨려도 그 파일은 여전히 exit 0 이라 CI가 초록으로 통과하고, 그 AC는 레지스트리에서 ✅ 로 세지면서 실제로는 아무것도 단언하지 않는다. `check_ac_mapping.py::check_cases_are_run()` 이 각 파일의 `run()` 이 그 파일의 `test_*` 를 전부 호출하는지 AST로 확인한다(표준 라이브러리만, 클러스터 불필요). 분할이 늘어날수록 값이 커지는 검사다.
>
> **신규 픽스처·신규 CI 스텝·`ci.yml` 변경 없음** — 러너가 파일을 자동 발견해 배차하므로 분할은 파일을 더하고 빼는 것으로 끝난다(배차 누락은 체커가 막는다). 이번 PR은 `ci.yml`을 한 글자도 건드리지 않는다.

> **`opensearch.py`(11) · `aws_config.py`(2) → AC별 전용 파일 13개 — ✅ 완료(2026-08-30)**: 분할 대기 6개 파일 중 **선결 판단 없이 순수 이동으로 끝나는 둘**을 골라 13개 전용 파일로 쪼갰다(`opensearch_{search,document_put,document_delete}_ac*.py` 11 · `aws_config_get_ac{1,2}.py` 2). 매칭 파일 **9 → 22**, 규칙 2 위반 **6 → 4**, 규칙 1 위반(공백) **54 → 41**(겸용 커버 40 → 27, 케이스 없음 14는 불변).
>
> **왜 이 둘인가**: 두 파일의 케이스는 이미 서로를 관측하지 못한다 — 2026-08-13 슬라이스가 opensearch 쪽 공유 상태(put→search→delete 파이프라인)를 **케이스별 전용 인덱스 `ci-<case>-<RUN_ID>` + 케이스별 유일 질의 토큰**으로 없애 뒀고, `run()` 주석이 "순서는 자유"라고 명시한다. 파일이 나뉘어 프로세스가 갈라지면 `RUN_ID`도 파일별로 새로 뽑히므로 격리는 오히려 강해진다. 남은 4개는 각각 선결 판단이 있어 이번 슬라이스에 섞지 않았다(위 「공백 내역」 참조).
>
> **신규 픽스처·신규 CI 스텝·`ci.yml` 워크플로 로직 변경 없음** — 2026-08-14가 넣은 러너가 파일을 자동 발견해 배차하므로, 분할은 파일을 더하고 빼는 것으로 끝난다(체커가 배차 누락을 막는다). 이번 PR의 `ci.yml` 변경은 삭제된 파일명을 가리키던 **주석 2줄뿐**이다.
>
> **순수 이동임을 주장하지 않고 증명했다**: 케이스 함수·상수를 손으로 옮기지 않고 원본의 ast 소스 세그먼트를 떠서 생성한 뒤, 원본(`HEAD`)과 신규 파일의 top-level 정의를 ast로 대조해 **32개 정의 전부가 정확히 한 파일에 동일하게 착지**함을 확인했다(도메인별로 스코프해 대조 — `REGION`처럼 두 원본에 같은 값이 있던 상수도 각자의 도메인 모듈에 하나씩 남는다). 재작성한 것은 파일별 `run()` 디스패처와 모듈 docstring뿐이다. 단언은 한 줄도 바뀌지 않았다.
>
> **공유 표면은 `_` 접두 모듈로 내렸다**: `_opensearch.py`(픽스처 상수 + `index_for`·`token_for`·`put_doc`·`search_until` 등 문서 헬퍼) · `_aws_config.py`(버킷·키·role ARN·ETag 모양). 둘 다 `run_all.py`의 `matching_unit_paths()`가 `_` 접두를 걸러내므로 매칭 단위가 아니고, 따라서 `검증 AC:` 선언을 갖지 않는다 — 체커가 이 제외를 러너와 **같은 함수로** 판정하므로 두 곳이 어긋날 수 없다.
>
> **`추가 인자: trace` 선언이 정밀해졌다**: 겸용 시절에는 파일 하나가 11개 케이스를 담아 trace를 쓰지 않는 케이스까지 프록시 URL을 요구했다. 이제 http-trace 기록을 실제로 읽는 4개(`opensearch_{search_ac3,document_put_ac4,document_delete_ac4}.py` · `aws_config_get_ac2.py`)만 신고한다. 프록시가 **AssumeRole 레코드만은 evict 하지 않기** 때문에(`tests/k8s/kind/http-trace.yaml`) 서버가 자격증명을 캐시해 한 번만 발급해도 파일이 나뉜 뒤의 각 프로세스가 그 앵커를 계속 관측할 수 있다 — 이 성질이 없었다면 분할이 접근 경로 단정을 깨뜨렸을 것이다.

> **AssumeRole·SigV4 4건 — 관측 수단 신설 후 ✅ 승격(2026-08-14)**: 직전 슬라이스가 `⬜ 보강 필요 (관측 수단 부재)`로 정정했던 4건(aws-config-get/AC2 · opensearch-search/AC3 · opensearch-document-put/AC4 · opensearch-document-delete/AC4)을, **관측 수단을 먼저 만든 뒤** per-AC 전용 케이스로 승격했다. 정정 자체는 옳았다 — 그때는 어떤 e2e 단언도 "역할을 한 번도 assume 하지 않는 서버"에서 그대로 통과했다. 바뀐 것은 픽스처다.

신규 픽스처 `tests/k8s/kind/http-trace.yaml`은 MinIO(S3+STS)와 OpenSearch **양쪽 앞단에 서는 기록 리버스 프록시** 하나다(기존 mock과 같은 ConfigMap + `python:3.12-alpine` 방식, 신규 이미지 빌드 없음). `AWS_CONFIG_S3_ENDPOINT`·`OPENSEARCH_STS_ENDPOINT`가 `:9000`(→MinIO), `OPENSEARCH_ENDPOINT`가 `:9200`(→OpenSearch)을 가리키고, 트레이스는 `:8081`로 읽는다 (CI 스텝 2개에 포트포워드 8090·8091 추가, 신규 검증 스텝은 없음).

**왜 MinIO 단독 trace가 아니라 프록시인가**: backlog가 든 후보 (a) `MINIO_HTTP_TRACE`는 MinIO에 닿는 트래픽만 본다. opensearch 3건이 요구하는 "AssumeRole **→ SigV4 서명 요청**"의 뒷부분, 즉 데이터 플레인 서명은 OpenSearch로 가지 MinIO로 가지 않으므로 (a)만으로는 "AssumeRole은 했다"까지만 관측되고 서명 절반은 여전히 공백으로 남는다. 한 프록시가 양쪽을 기록하면 STS가 발급한 AccessKeyId와 데이터 플레인 서명 키를 **같은 트레이스 안에서 대조**할 수 있어, 캐시 타이밍 의존(그 run의 첫 호출에서만 관측 가능)과 파드 로그 스크래핑도 함께 사라진다.

**단정의 구조**(`_helpers.py::assert_assumed_role_access`): (1) 설정된 role ARN의 AssumeRole이 성공했고 그 STS 호출 자체는 **베이스 자격증명으로 서명**됐다 — 베이스 키의 용도가 assume 뿐임을 고정, (2) 데이터 플레인 요청이 `AWS4-HMAC-SHA256`으로 해당 region의 `s3`/`aoss` 스코프에서 서명됐고 `host`(검색·색인은 payload 해시도)를 서명 대상에 포함한다, (3) 서명한 액세스 키가 **STS가 내준 키 집합에 속하고 베이스 키가 아니며** 세션 토큰을 동반한다. 셋은 각각 다른 위장 경로를 falsify한다 — (1)은 STS를 건너뛴 서버, (2)는 무서명 요청(security 꺼진 픽스처가 받아주는 것), (3)은 베이스 키 서명(MinIO가 받아주는 것).

**프록시의 전제와 그 검증**: SigV4는 `Host`를 서명하므로 프록시는 클라이언트가 서명한 `Host`를 그대로 전달해야 업스트림의 재계산이 일치한다. 이 투명성은 SigV4를 실제로 재계산해 불일치를 403으로 거부하는 가짜 업스트림에 대해 검증했고, `Host`를 재작성하면 403이 나오는 음성 대조로 그 검증이 vacuous하지 않음까지 확인했다. 기록은 식별자만 남긴다 — 서명값·세션 토큰·STS 시크릿 키·요청/응답 본문은 저장하지 않는다(STS 응답에서 꺼내는 것은 AccessKeyId 하나뿐이며, 이는 서명된 요청마다 평문으로 오가는 식별자다). 링버퍼가 넘칠 때 AssumeRole 레코드는 evict하지 않는다 — 자격증명이 캐시돼 프로세스당 한 번만 발급되므로 밀려나면 재관측이 불가능하다.

**잔여**: `workload-logs/AC4`의 부분 단정과 backlog 14건(platform 2 · session 12)은 그대로다.

> **부분 단정 잔여(계수 밖, ✅ 유지)**: `workload-logs/AC4`의 "파드에 컨테이너가 둘 이상이면 `container`가 필요하다" 절은 아직 단정되지 않는다 — 이 배포의 픽스처 파드가 전부 단일 컨테이너라 거부를 관측할 대상이 없다. 러닝 멀티컨테이너 워크로드가 필요해 후속 슬라이스로 남긴다. **2026-09-03 분할로 blocker 절반이 사라졌다**: 롤아웃 대기가 `ci.yml` 스텝이 아니라 파일 안의 선행 조건 헬퍼(`_workload.py::wait_for_single_ready_pod`)로 처리되므로 워크플로 변경은 필요 없고, 남은 것은 `test-deployment.yaml` 에 **별도 멀티컨테이너 워크로드를 신설**하는 것뿐이다 — 기존 `workload-fixture` 에 컨테이너를 더하는 경로는 `workload_logs_ac2.py` 가 단정하는 「`container` 없이 성공」을 깨뜨리므로 막혀 있다. 케이스 docstring에 "단언하지 않는 것"으로 명시돼 있다(2026-08-07).

> **잔여 파일 수준 커버 13건 정리 — ✅ 완료(2026-08-13)**: `opensearch.py`(9) · `aws_config.py`(2) · `dear_baby.py`(2)를 마지막으로 `✅ 통합` 행이 0이 됐다. **9건은 per-AC 전용 케이스로 승격**(opensearch-document-put/AC1·AC2 · opensearch-search/AC1·AC2 · opensearch-document-delete/AC1·AC2 · aws-config-get/AC1 · dear-baby-reset-user/AC1·AC2), **4건은 관측 불가를 근거로 ⬜로 정정**(위 backlog 참조). 신규 픽스처·신규 CI 스텝·신규 롤아웃 대기 없음 — 이미 도는 스텁(`dear_baby.py` 8082 · `aws_config.py` 8084 · `opensearch.py` 8086) 안에서 평면 `run()`을 케이스 디스패처로 재구성했다. opensearch 쪽 공유 상태(put→search→delete 한 파이프라인)는 **케이스별 전용 인덱스 `ci-<case>-<RUN_ID>` + 케이스별 유일 질의 토큰**으로 없앴다(put이 인덱스를 자동 생성하므로 픽스처 추가가 필요 없고, 컬렉션 전체 검색도 다른 케이스의 문서와 겹치지 않는다). 분리하며 **AC 문언 대비 비어 있던 단정 4건을 채웠다**: (1) opensearch-search/AC2의 "기본값 10"은 한 번도 관측된 적이 없었다(문서 3건뿐이라 기본값과 무제한이 구분되지 않음) → 12건 시드 후 `len(hits)==10` · `total==12`; (2) opensearch-document-put/AC2의 "자동 생성"은 색인 전 상태를 보지 않아 성립하지 않았다 → 색인 전 해당 인덱스 검색이 404 `index_not_found_exception`으로 거부되는 것을 선행 관측; (3) dear-baby-reset-user/AC2의 "`email` 누락 거부"는 **단언이 0개**였다 → `McpError: email is required` 단언 신설, `container` 재정의도 관측(무시되면 성공해버리므로 실패 자체가 판별자); (4) aws-config-get/AC1의 메타데이터는 size만 봤다 → contentType·ETag(따옴표 제거된 다이제스트 모양)·lastModified(RFC3339 파싱, 미래 아님)까지 단정. tests/ 변경이라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변).

> **`workload.py` 통합 커버 11건 → per-AC 전용 케이스 — ✅ 완료(2026-08-07)**: 잔여 `✅ 통합` 24건 중 `workload.py`가 소유한 **11건 전부**(namespace-list/AC1 · workload-list/AC1·AC2 · workload-logs/AC1·AC2·AC3·AC4 · workload-restart/AC1 · workload-scale/AC1·AC2 · platform-auth-safety/AC3)를 per-AC 전용 케이스로 분리했다. **신규 CI 스텝·신규 네임스페이스·신규 롤아웃 대기 없음** — 이미 도는 `workload.py`(port 8081) 스텝 안에서 평면 `run()`을 케이스 디스패처로 재구성했고, 픽스처는 목록 조회 전용 오브젝트 2개(`workload-fixture-sts` StatefulSet · `workload-fixture-ds` DaemonSet)만 `test-deployment.yaml`에 더했다(러닝 파드를 기다리는 케이스가 없어 `ci.yml` 무변경). 분리하며 **AC 본문과 대조해 비어 있거나 반쪽이던 단정 6건을 실단정으로 채웠다**: (1) platform/AC3(RBAC 경계)은 `workload.py`에 단언이 **0개**였다 → `kubectl auth can-i`로 배포된 ServiceAccount 신원(그룹 3개 포함)을 임퍼소네이트해 허용 15동사·금지 11동사를 apiserver SubjectAccessReview로 관측(매니페스트 재독이 아니라 실제로 바인딩된 RBAC를 본다). (2) workload-list/AC1은 AC가 요구하는 "각 enum 종류"를 Deployment 하나만 보고 "레플리카 요약"을 아예 안 봤다 → 3 enum 전부 호출 + kind별 요약 필드를 단정하고 **다른 kind의 필드가 섞여 나오지 않음**까지 확인. (3) namespace-list/AC1은 AC 문언의 **생성 시각**을 빼먹었다 → 전 항목에 phase·creation_timestamp가 있고 파싱 가능한 실제 instant임을 단정. (4) workload-logs/AC1은 로그를 내지 않는 `pause` 픽스처에서 `logs == ""`만 봐 "최근 로그가 반환된다"를 관측하지 못했다 → 실제로 출력을 내는 `crashloop-fixture`의 현재 인스턴스 마커를 단정(이전 인스턴스 마커와 문자열이 달라 AC3와 서로를 대신할 수 없다). (5) workload-scale/AC1은 AC가 명시한 **replicas=0**을 한 번도 시도하지 않았다 → 3 → 0 → 1 경로로 확장(0은 `.status.replicas` 드레인으로 확인). (6) workload-restart/AC1은 "delete가 아닌 patch"를 단언하지 않았다 → 재시작 전후 `metadata.uid`·`creationTimestamp` 불변 + `metadata.generation` 증가를 단정. workload-logs/AC4도 에코 필드 확인에서 **출력 반영**(timestamps → RFC3339 접두, since_seconds → 시작 마커 탈락) + 잘못된 컨테이너 거부로 강화했다. 잔여 통합 13건(`aws_config.py` 2 · `dear_baby.py` 2 · `opensearch.py` 9)은 후속 슬라이스.

> **통합 커버 8건 → per-AC 전용 케이스 — ✅ 완료(2026-08-07)**: 규칙 1·2를 미충족하던 파일 수준 `✅ 통합` 32건 중 **8건**(ping/AC1 · platform-auth-safety/AC5·AC6 · github-app-installation-token/AC1·AC2·AC4 · grafana-token/AC1·AC2)을 per-AC 전용 케이스로 분리했다. **신규 픽스처·신규 CI 스텝 없음** — 이미 도는 `smoke.py`(8080) · `github_app.py`(8083) · `grafana.py`(8085) · `no_config.py`(8088) 스텝 안에서 평면 `run()` 본문을 케이스 함수로 재구성했을 뿐이다. 분리 과정에서 **단정이 비어 있던 세 곳을 실제 단언으로 채웠다**: ping/AC1은 `smoke.py`가 `ping`을 호출조차 하지 않고 tools/list 존재만 확인했고, github/AC4(개인키 비노출)는 어떤 단언도 없었으며, grafana/AC1은 `# token expires` 주석의 **존재**만 봤다(이제 RFC3339를 파싱해 TTL이 50~70분임을 단언 — mock이 서버가 보낸 `expiresAt`을 되돌려주므로 1시간 TTL이 실제로 관측된다). **platform/AC5는 파일을 옮겼다**: AC 문언이 "자격증명 env를 비운 채 기동"을 전제하는데 `smoke.py`가 도는 주 배포는 모든 자격증명이 배선돼 있어 그 전제가 성립하지 않는다 — 전제가 성립하는 유일한 배포인 자격증명 미부착 변형(`no_config.py`, port 8088)으로 옮겨 `/healthz` + 전체 도구 표면 유지를 단언한다. 잔여 통합 24건(`aws_config.py` 2 · `dear_baby.py` 2 · `opensearch.py` 9 · `workload.py` 11)은 후속 슬라이스.

> **platform 인증 게이트(2) — ✅ 완료(2026-08-07)**: platform-auth-safety/AC1(인증 게이트)·AC7(API 키 인증)을, 인증을 켠 최소 배포 변형(`tests/k8s/kind/auth-fixture.yaml` — 같은 `:ci` 이미지에 `MCP_API_KEYS` 세팅·`MCP_AUTH_DISABLED` 미설정, 자격증명 시크릿 미부착 graceful degrade)을 별 네임스페이스에 띄워 검증하는 per-AC 전용 e2e 케이스로 승격했다(신규 CI 스텝 port 8087, 기존 배포·kustomize 불변). 케이스: `auth.py::test_platform_auth_safety_ac1_gate`(무Authorization → 401 `missing_token`), `::test_platform_auth_safety_ac7_api_key`(무효 키 → 401 `invalid_token`, 유효 키 → tools/list 인가). 잔여 platform 2건(AC2 디스커버리·AC8 구성 유연성)은 OIDC 발급자 mock·다중 구성 전환이 필요한 후속 슬라이스.

> **pod-describe(3) — ✅ 완료(2026-07-30)**: pod-describe/AC1·AC2·AC3을 기존 CI 스텝(`workload.py`, port 8081)과 `workload-fixture` 러닝 파드를 재사용하는 per-AC 전용 케이스로 승격했다(신규 파일·픽스처·`ci.yml` 변경 없음). 케이스: `workload.py::test_pod_describe_ac1_snapshot`(스냅샷 필드), `::test_pod_describe_ac2_target_resolution`(name/selector/workload 해석 + 상호배타 거부), `::test_pod_describe_ac3_events_best_effort`(events 필드 best-effort present).

> **미설정 graceful 거부(6) — ✅ 완료(2026-08-07)**: 별도 no-config 픽스처를 만들지 않고, 인증 변형 `tests/k8s/kind/auth-fixture.yaml`이 이미 **자격증명 시크릿을 하나도 붙이지 않은** 배포(= `GITHUB_APP_CLIENT_ID`·`AWS_CONFIG_S3_BUCKET`·`GRAFANA_ISSUER_TOKEN`·`OPENSEARCH_ENDPOINT` 전부 미설정 → `main.go`의 `build*Service`가 모두 `NewUnavailable("")`로 degrade)라는 점을 이용해 그 파드에 6개 per-AC 전용 케이스를 신설했다(`tests/integration/no_config.py`, CI 스텝 port 8088 하나 추가, 신규 픽스처·신규 롤아웃 대기 없음). 각 케이스는 (1) 호출이 `isError=true` + `<도메인> unavailable: <미구성 사유>` 텍스트로 돌아오고 (2) 직후 `ping`이 여전히 `pong`을 반환함을 단언해 AC 문언의 "서버 기동·다른 도구에 영향 없음"까지 관측한다. 그 대가로 이 변형에는 자격증명을 붙이면 안 된다(픽스처 헤더 주석에 명시).

> **파괴적 작업 표기(5) — ✅ 완료(2026-07-21)**: 파괴 동작을 실제로 실행하지 않고 배포 서버 `tools/list`의 `annotations.destructiveHint == true`(및 `readOnlyHint == false`)를 e2e로 단언하는 per-AC 전용 케이스를 신설해 위 레지스트리에서 ✅로 승격했다(`internal/server/mcp_test.go`의 in-process 단언을 배포 서버 통합 e2e로 승격). 케이스: `dear_baby.py::test_dear_baby_reset_user_ac3_destructive_hint`, `opensearch.py::test_opensearch_document_{put,delete}_ac3_destructive_hint`, `workload.py::test_workload_{restart_ac2,scale_ac3}_destructive_hint`. 남은 backlog 14건은 no-config 배포 변형·신규 픽스처가 필요한 후속 슬라이스.

### 비-AC 파일 (스모크·인프라) (1)

> AC 대신 스모크/인프라 확인(서버 기동·`/healthz`·도구 표면 존재)을 주검증한다고 선언한 매칭 단위 파일의 등재 자리다(규칙 3). 이 목록에 없는 비-AC 파일은 고아이고, 여기 등재됐는데 실재하지 않거나 AC를 선언하는 파일도 고아 등재다 — 체커가 양방향으로 검사한다.

- **`smoke.py`** — primary 배포의 **도구 표면 존재 확인**(`_helpers.EXPECTED_TOOLS` 14개가 `tools/list`에 광고되는지). `실행 순서: 0`으로 primary 그룹 맨 앞에서 돌아, 배포가 깨졌을 때 뒤따르는 AC 파일들이 차례로 모호하게 죽는 대신 한 번에 원인을 말하는 **공유 선행 조건**이다(파일 수는 분할이 진행될수록 늘어나므로 여기 적지 않는다 — 세는 것은 러너와 체커의 몫이다). 2026-08-31 분할 전에는 이 확인이 ping/AC1·platform-auth-safety/AC6과 한 파일에 섞여 있었고(= 분할 대기), 두 AC를 각자의 전용 파일(위 레지스트리 참조)로 떼어낸 뒤 남은 것이 이 파일이다.
  - **이것은 platform-auth-safety/AC5가 아니다**: 주 배포는 모든 통합이 구성돼 있어 정상적인 `tools/list`가 degradation에 대해 아무것도 말해 주지 않는다. AC5는 자격증명이 없는 배포에서만 관측되므로 그 전용 파일이 `auth-variant`에서 같은 상수를 단언한다.

> 이 절의 백틱 파일명은 체커가 **등재 목록으로 읽는다**(`FILE_REF_RE`). 다른 파일을 예로 들 때는 백틱을 쓰지 말 것 — 고아 등재로 잡힌다.

### 🚫 e2e 예외 (1) — 규칙 4 등재 (1:1 계수에서 제외)

> e2e로 커버하기 비현실적이고 정적 검토로 대체하는 AC. 모델 정의의 **규칙 4**가 "AC별 사유와 대체 검증 수단을 적어 이 문서에 등재"하도록 정하고, 정의의 현황 메모도 예외 등재 후보로 아래 1건만 지목한다 — 그에 따라 **등재**했고 체커가 1:1 계수에서 제외한다(`예외 등재: 1`). 정의 문서 자체는 고치지 않는다(등재의 SSOT는 이 문서다). **도구·기능 미구현은 예외 사유가 아니다**(규칙 4) — session-\* 12건이 공백으로 남아 있는 이유다.
>
> **정의 예외 개정 제안(1건, e2e 비대상)**: e2e 1:1 계수에서 빠져야 하는 AC는 아래 platform/AC4 1건뿐이다. 파괴적 표기 5건은 `tools/list` 메타데이터를 e2e로 단언해 커버하므로(✅ 전용 케이스, 2026-07-21 완료) e2e 예외가 아니다. 정의의 예외 목록에는 이 1건만 등재하도록 제안한다.

- **platform-auth-safety/AC4** 하드닝된 런타임 — [정적 매니페스트] `k8s/deployment.yaml` securityContext(비루트·읽기전용 루트FS·capability drop 등) 정적 검증 — definition이 든 e2e 예외 예시. 대체: 정적 매니페스트 리뷰 + (선택) 런타임 securityContext 단언 단위.

## 변경 이력

| 시점 | 변경 내용 | 이전 상태 | 이후 상태 |
|------|-----------|-----------|-----------|
| 2026-09-04 | **주석 비중복성 정책 문서 신설 + 첫 판정 패스 2개 패키지** — 정합성 모델 `tbm_homelab-k3s-mcp-comment-redundancy`의 to-be가 `absent`(정책 문서 부재)였다. `docs/comment-policy.md`를 세워 **복원 경로 넷**(코드 · 저장소 문서 · PR · 커밋 메시지), **유지 대상 셋**(기계 판독 주석 · doc 주석 최소치 · 복원 불가능한 지식), **5단계 판정 절차**, 「애매하면 남긴다」의 비용 비대칭 근거, 그리고 as-is 지문의 사각지대(Python docstring 본문 · Go 블록 주석 · 줄 끝 주석)를 고정했다. 이어 그 정책을 `internal/opensearch/` · `internal/sessionplatform/`(주석 161줄 / 4파일, 범위 내 848줄의 19%)에 **완전 적용**해 주석 8줄을 제거했다 — 전부 ① 선언 재진술 ② `docs/prd-opensearch-*.md` 재진술 ③ 자기 파일 안의 중복이며, exported 식별자의 1줄 doc 주석과 패키지 주석은 하나도 건드리지 않았다. 판정 표본이 정정한 것: 감지가 지목한 「kind 픽스처 YAML의 매니페스트 재진술」은 실측에서 **성립하지 않았다** — `tests/k8s/kind/*.yaml`의 주석 240줄은 대부분 상류 함정·정책 판단 근거라 유지 대상이다. **범위 밖(후속)**: 나머지 주석 679줄(`main.go` · 그 외 `internal/` · `tests/` · `scripts/`), 지문 패턴 확장, 이 축의 CI 게이트 신설(현재 ungated). docs/ 신설 + `internal/` 주석 변경이라 as-is 해시 변경 + 문서 인벤토리·허브 계수 갱신(PRD·AC·테스트 문서 불변). | 가치 5 / PRD 18 / AC 64 / 테스트 18 · 정책 1 · 허브 39 | 가치 5 / PRD 18 / AC 64 / 테스트 18 (불변) · 정책 2 · 허브 40 |
| 2026-09-04 | **`session_write` 구현 착지 — 이 모델의 마지막 미구현 도구가 닫혔다** — `internal/sessionplatform`에 `WriteSession`(제어면 `POST /api/v1/sessions/{id}/write` 클라이언트)을, `internal/mcp`에 `session_write` 도구 등록·디스패치를 더해 session-write/AC1~AC5의 **도구 계층**을 구현으로 닫았다(도구 표면 16 → 17). 새 설계 판단은 **거부 응답 매핑 하나**였고, 원장이 적어 둔 「재시도 가능/불가 **두** 종류를 더한다」 대신 **세 종류**(`kindBusy` 429 · `kindTooLarge` 413 · `kindQuotaExhausted` 507)를 더했다 — 두 종류면 413과 507이 한 kind로 접혀 둘의 구별이 제어면이 보내는 산문에만 의존하는데, AC4가 요구하는 것은 네 거부가 서로 구별되는 것이라 그 의존을 구조로 바꿔야 했다. 그 차이를 지키는 것이 `TestWriteRefusalsAreDistinctWithoutTheControlPlanesProse`다: 네 상태코드에 **같은 오류 본문**을 물려 놓고도 메시지가 쌍쌍이 달라야 통과한다(본문을 다르게 두면 두 kind로 접어도 통과해 버려 단정이 공허해진다 — 실제로 첫 판에서 이 구멍이 뮤테이션으로 드러나 테스트를 고쳤다). 페이로드 1 MiB 상한은 **클라이언트에서 선제 거부하지 않는다**: `claude-code`에만 걸리는 워크로드 타입별 규칙이라 조회 없이는 판정할 수 없고, 판정은 제어면에 남기고 413만 구별해 전달하는 것이 맞다. 빈 id는 HTTP 이전에 거부해 「거부된 호출은 세션을 건드리지 못한다」를 요청 0건으로 증명한다 — write는 도착만으로 스냅샷을 복원시키므로 read보다 이 보장이 더 필요하다. 함께 **원장의 살아 있는 어긋남 3건**을 교정했다: 「session 3종의 통합 e2e — 미작성」 항목의 세 단정(「`tests/integration/`에 아직 케이스가 없다」·「e2e 렌즈에서 공백으로 계수된다」·「스텁이냐 실 제어면이냐의 택일은 그 렌즈의 몫이다」)이 모두 거짓이었고 반례가 **같은 파일 안**에 있었다(session-list 3파일 실재 · 레지스트리 ✅ 3행 · 실 제어면 확정 기록). 문서 매트릭스의 `session_write` 행에 남아 있던 `(구현 선행)`과, e2e 렌즈 절 두 곳의 「도구 미구현」 서술도 이 PR이 거짓으로 만드는 자리라 함께 고쳤다. 곁가지로 `_helpers.EXPECTED_TOOLS`를 15 → **17**로 정정했다(`session_read` 누락 + 신규 `session_write` — 단정이 부분집합이라 **깨진 게 아니라 조용히 약해져** 있었고, 이번이 두 번째 재발이라 주석에 「도구를 더하는 PR이 같은 커밋에서 갱신한다」를 규칙으로 명시했다). **AC·PRD·테스트 문서의 신설·삭제·개정 0건**이고 e2e 렌즈 레지스트리 표·집계도 불변이다 — session_write의 통합 e2e는 그 렌즈 소관으로 남는다(같은 날 platform 2건을 닫은 형제 슬라이스 뒤 **공백 9 유지**). 그 형제 슬라이스가 새로 쓴 「남은 9건은 도구 자체가 미구현」이라는 문장도 이 PR이 거짓으로 만드는 자리라 함께 고쳤다. | 가치 5 / PRD 18 / AC 64 / 테스트 18 · 도구 표면 16 | 가치 5 / PRD 18 / AC 64 / 테스트 18 (불변) · 도구 표면 17 |
| 2026-09-04 | **platform 2건(AC2 인증 디스커버리 · AC8 인증 방식 구성 유연성)의 통합 e2e 저작** — backlog 11건 중 **선행이 저작뿐인 2건**을 전용 파일 2개(`platform_auth_safety_ac{2,8}.py`)로 닫았다. 두 AC는 같은 난제(발급자가 없으면 `auth.FromEnv`가 기동을 막는다)를 공유하므로 한 슬라이스다. 핵심 판단은 「스텁 발급자 vs 실 IdP」였고 **모킹 정책이 스텁을 배제한다**: `UPS`는 실 상류를 CI에서 띄울 수 있으면 못 쓰고 `IMG`는 이미지가 공개되면 소멸하는데 `ghcr.io/dexidp/dex`는 공개·arm64라 스텁은 **등재 불가 모킹**이 된다 → 신규 픽스처 `tests/k8s/kind/oidc-fixture.yaml`은 **실 OIDC 발급자(dex, 다이제스트 핀)**이고 **모킹 허용목록은 5건·상한 5로 불변**이다(MinIO·OpenSearch·session-platform과 같은 자리). AC8은 (a)만으로 끊지 않고 **4구성 전부**를 덮었다 — (a) auth-variant에서 401 + 챌린지에 `resource_metadata` 없음 + 디스커버리 404, (b) OAuth만 세팅한 변형에서 디스커버리 200 + Available, (c) 둘 다 세팅한 변형에서 API 키 인가 + 디스커버리 200, (d) 아무것도 미설정인 변형이 종료 코드 1 + 로그 `no authentication configured`로 **기동 실패**. (d)가 AC2의 「Available ⇒ 기동 시 OIDC 디스커버리·JWKS 로드 성공」 추론을 공허하지 않게 만든다. AC2는 401 챌린지 → 보호 리소스 메타데이터 → 발급자 `openid-configuration` → JWKS의 사슬을 **앞 칸이 준 값으로만** 걷는다. `ci.yml`은 픽스처 배포·롤아웃 대기·신규 그룹 실행(`oauth-variant`, 8089)·진단 덤프 4자리가 늘었고, 기동 실패가 의도인 변형에는 롤아웃 대기를 걸지 않았다. 네 구성 대조가 AC8 자체라 그 파일만 `_oidc.py`의 짧은 `kubectl port-forward`로 다른 배포에 닿는다. **범위 밖(후속)**: dex에서 실 토큰을 받아 「유효 JWT → 인가」까지 단정하는 것. tests/·docs/·ci.yml 변경이라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변). | ✅ 전용 파일 52 · ⬜ 분할 대기 0 · ⬜ 공백(케이스 없음) 11 · 🚫 예외 1 · 비-AC 1 | ✅ 전용 파일 54 · ⬜ 분할 대기 0 · ⬜ 공백(케이스 없음) 9 · 🚫 예외 1 · 비-AC 1 |
| 2026-09-04 | **`session_read` 구현 착지 + 원장 어긋남 2곳 교정** — `internal/sessionplatform`에 `ReadSession`(제어면 `POST /api/v1/sessions/{id}/read`, 커서 규약·`path` 분기 노출·404/400 구분)을, `internal/mcp`에 `session_read` 도구 등록·디스패치를 더해 session-read/AC1~AC4의 **도구 계층**을 구현으로 닫았다(도구 표면 15 → 16). 음수 커서·빈 id는 HTTP 이전에 거부해 "상태 불변"을 요청 0건으로 증명한다. 함께 **원장의 살아 있는 어긋남 2곳**을 교정했다 — 「허브 도달 가능 문서」가 실측 39와 어긋난 **38 / 38**로 남아 있었고(분해 괄호에 정책 문서 범주가 없었다), 「문서 인벤토리」 표에 `e2e-mocking-policy.md` 행이 없어 그 파일명이 이 문서에 **0회** 등장했다(#47이 문서를 신설하며 허브 링크만 넣었다). 두 곳 모두 레포 체커의 파싱 범위 밖이라 게이트가 영원히 초록이었다. **AC·PRD·테스트 문서의 신설·삭제·개정 0건**이고 e2e 렌즈 레지스트리·집계도 불변이다 — session_read의 통합 e2e는 그 렌즈 소관으로 남는다. | 가치 5 / PRD 18 / AC 64 / 테스트 18 | 가치 5 / PRD 18 / AC 64 / 테스트 18 (불변) |
| 2026-09-04 | **session-list 3건의 통합 e2e 저작** — backlog 14건 중 선행 조건이 해소된 `session-list/AC1·AC2·AC3`을 전용 파일 3개(`session_list_ac{1,2,3}.py`)로 닫았다. 핵심 판단은 「제어면 스텁 vs 실 제어면」이었고 **모킹 정책이 답을 이미 정해 두었다**: `IMG` 예외는 실 구성요소의 이미지를 CI에서 확보할 수 없을 때만 쓸 수 있는데 `ghcr.io/dlddu/session-platform`은 공개 패키지에 arm64 매니페스트를 갖고 있어 러너의 kind가 그대로 당긴다 → 신규 픽스처 `tests/k8s/kind/session-platform.yaml`은 **실 제어면**이고 **모킹 허용목록은 5건·상한 5로 불변**이다. 세션은 제어면의 실 상태 저장소(소유 라벨이 붙은 `session-<id>` ConfigMap)에 시드한다 — `List`가 파드를 조회하지 않으므로 실 API·실 디코딩 경로가 그대로 돌고, MinIO를 `minio-seed`로 시드하는 선례와 같은 자리다. 시드 JSON 필드는 `session.Session`의 구조체 태그에서 확인했다. AC1은 한 파일 안에서 재고를 비워 빈 목록을 관측한 뒤 세션 둘(active·snapshot)을 시드해 열거를 관측하고(파일 간 순서 의존 0, `실행 순서:` 선언 0건), AC2는 파드 집합을 첫 호출 전에 떠서 3회 호출 뒤와 대조한다 — **실 제어면이라 이 단정이 vacuous하지 않다**(스텁이면 만들 파드가 없다). AC3는 `auth-fixture.yaml` 변형에서 도는 7번째 「미설정 거부」 파일이다. homelab 쪽 배선은 무변경 — base `k8s/deployment.yaml`이 이미 `SESSION_PLATFORM_ENDPOINT`를 클러스터 내부 주소로 박아 두었고 픽스처가 프로덕션과 같은 이름(ns `session-platform` · Service `control-plane`)을 쓴다. `ci.yml`은 배포·롤아웃 대기·진단 덤프 3자리만 늘었다(러너 배차 primary 41→43 · auth-variant 9→10). 곁가지로 `_helpers.EXPECTED_TOOLS`를 14 → 15로 정정했다(`session_list` 누락 — 단정이 부분집합이라 **깨진 게 아니라 조용히 약해져** 있었다). tests/·docs/·ci.yml 변경이라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변). | ✅ 전용 파일 49 · ⬜ 분할 대기 0 · ⬜ 공백(케이스 없음) 14 · 🚫 예외 1 · 비-AC 1 | ✅ 전용 파일 52 · ⬜ 분할 대기 0 · ⬜ 공백(케이스 없음) 11 · 🚫 예외 1 · 비-AC 1 |
| 2026-09-03 | **`session_list` 구현 착지** — `internal/sessionplatform`(제어면 `GET /api/v1/sessions` 클라이언트 + `Unavailable` 대체)과 `internal/mcp` 도구 등록으로 session-list/AC1·AC2·AC3을 구현으로 닫고, `k8s/deployment.yaml`에 `SESSION_PLATFORM_ENDPOINT`를 배선했다. 문서 쪽 변경은 **상태 기술의 교정뿐**이다: 자동화 커버리지의 🔴 항목이 "session-platform 배포가 클러스터에서 제거돼 검증 불가"라는 **이미 사실이 아닌 전제**로 12 AC 전체를 묶어 두고 있었다(제어면은 재배포돼 `session-platform` 네임스페이스에서 `control-plane` 1/1로 동작 중). 남은 공백을 read/write 9건과 통합 e2e로 좁혔다. **AC·PRD·테스트 문서의 신설·삭제·개정 0건.** | 가치 5 / PRD 18 / AC 64 / 테스트 18 | 가치 5 / PRD 18 / AC 64 / 테스트 18 (불변 — e2e 렌즈 레지스트리·집계도 불변) |
| 2026-09-03 | 마지막 분할 대기 파일 `workload.py`(16 AC 겸용)를 **AC별 전용 파일 16개**로 분할해 **규칙 2 위반을 0**으로 만들었다. 선결 판단이었던 「케이스가 공유 픽스처 상태에 순서 의존적」은 원장이 지목한 방식대로 **각 파일이 자기 선행 조건을 스스로 성립시키는 것**으로 해소했다 — `_workload.py::ensure_workload_fixture_baseline()` 이 `deploy/workload-fixture` 를 매니페스트 기준선(Ready 파드 정확히 1개)으로 멱등하게 되돌리고, 픽스처를 읽거나 변형하는 9개 파일이 각자 `run()` 에서 그것을 부른다. `실행 순서:` 로 파일 간 순서를 고정하는 길은 결합을 옮길 뿐이라 채택하지 않았고, 그 증거로 신규 16개 파일 중 어느 것도 `실행 순서:` 를 선언하지 않는다. 유일한 신규 로직은 `wait_for_single_ready_pod()`(`rollout status` 는 구 파드의 Terminating 을 기다리지 않는데 `pod_describe` 는 셀렉터로 파드 하나를 고른다)이며, 선행 조건은 시험 대상 도구가 아니라 kubectl 로 세운다. 케이스 함수·상수는 ast 소스 세그먼트로 옮겨 **정의 39개 중 36개가 원본과 AST 동일**함을 대조로 확인했다(예외 셋은 선언됨 — `run` 1개는 파일별 디스패처 16개로 재작성, `workload-scale/AC1` 케이스는 분할 후 거짓이 되는 docstring 한 문장만 고쳐 body AST 동일을 따로 대조, 죽은 상수 `EXEC_NAMESPACE` 1개 제거 — 단언 무변경). 공유 표면은 매칭 단위 밖 `_workload.py` 로 내렸다. 러너가 파일을 자동 발견하므로 신규 픽스처·신규 CI 스텝 없음 — `ci.yml` 무변경(primary 배차 26 → 41, auth-variant 9 불변). `docs/test-*.md` 6종의 자동화 필드에서 `workload.py` 참조를 전용 파일로 재조준했고, 그중 platform AC3 필드의 「정적 검증 + delete/secret 부재 단언 자동화 추가 권장」은 2026-08-07 이후 실제로 e2e 단언이 존재하므로 함께 교정했다. tests/·docs/ 변경이라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변). | ✅ 전용 파일 33 · ⬜ 분할 대기 16 · ⬜ 공백(케이스 없음) 14 · 🚫 예외 1 · 비-AC 1 | ✅ 전용 파일 49 · ⬜ 분할 대기 0 · ⬜ 공백(케이스 없음) 14 · 🚫 예외 1 · 비-AC 1 |
| 2026-08-31 | 분할 대기 4개 파일 중 `no_config.py`(7 AC)·`auth.py`(2 AC)·`smoke.py`(2 AC)를 **AC별 전용 파일 11개 + 규칙 3 비-AC 파일 1개**로 분할했다(`platform_auth_safety_ac{1,5,6,7}.py` · `ping_ac1.py` · `aws_config_get_ac3.py` · `github_app_installation_token_ac3.py` · `grafana_token_ac3.py` · `opensearch_{search_ac4,document_put_ac5,document_delete_ac5}.py`). 이 셋을 고른 근거는 **케이스가 전부 읽기 전용이라 프로세스가 갈라져도 서로를 관측하지 못한다**는 것이다(401 응답 · `/healthz`·`/readyz` · `tools/list` · 미설정 거부 + 직후 `ping`). 세 파일이 안고 있던 선결 판단 둘을 확정했다 — ① `auth-variant` 배차 증가(2 → 9) 수용(포트포워드가 재시도 루프로 그룹 내내 유지되고 각 파일이 `wait_for_healthz`로 시작하므로 배선 무변경, 늘어나는 비용은 파일당 파이썬 기동 + 세션 개설뿐) ② `smoke.py`의 잔여 도구 표면 확인을 **비-AC 파일로 등재**(등재 0건이던 규칙 3 경로의 첫 양성 사례). 케이스 함수·상수는 ast 소스 세그먼트로 옮겨 **19개 정의 전부가 원본과 AST 동일**함을 대조로 확인했고(재작성은 파일별 `run()` 3개 + 도구 표면 상수 1개뿐), 공유 표면은 매칭 단위 밖 `_auth_variant.py`로 내렸다. 유일한 단언 변경은 `smoke.py`가 들고 있던 **9개짜리 stale 도구 표면 부분집합**을 `_helpers.EXPECTED_TOOLS`(14개)로 정정한 것이다 — 빠져 있던 `github_app_installation_token`·`aws_config_get`·`opensearch_*`는 primary 그룹 케이스가 실제로 구동하는 도구다. 체커에는 하네스 검사 하나를 더했다 — `check_cases_are_run()` 이 각 파일의 `run()` 이 그 파일의 `test_*` 를 전부 호출하는지 AST로 확인해, 파일 하나에 케이스 하나인 구조에서 디스패처가 케이스를 빠뜨려도 초록으로 통과하는 구멍을 막는다. 러너가 파일을 자동 발견하므로 신규 CI 스텝·픽스처·롤아웃 대기 없음 — `ci.yml` 무변경. tests/·docs/ 변경이라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변). | ✅ 전용 파일 22 · ⬜ 분할 대기 27 · ⬜ 공백(케이스 없음) 14 · 🚫 예외 1 · 비-AC 0 | ✅ 전용 파일 33 · ⬜ 분할 대기 16 · ⬜ 공백(케이스 없음) 14 · 🚫 예외 1 · 비-AC 1 |
| 2026-08-31 | **문서 허브 신설** — `docs/`의 마크다운 38개에 진입점이 없어 레포를 클론하지 않으면 읽을 수 없던 상태를 해소했다. 배포 골격 3개(`index.html` 허브 · `reader.html` 뷰어 · `.nojekyll`)를 추가하고, 허브는 도구 목록이 아니라 **가치별로 묶은 목차**로 구성해 각 도구 행에 PRD·테스트 두 링크와 달성 가치 식별자를 나란히 뒀다. 뷰어는 외부 CDN 의존 없는 단일 파일이며, 제목 앵커를 `AC1`·`V3` 같은 이 레포의 식별자로 만들어 특정 AC를 URL로 가리킬 수 있게 했다(`?doc=` 경로 가드 + `.md` 상대 링크 재작성 포함). 문서 38개 전부에 대해 렌더링과 링크 도달을 대조 확인했다. 문서 내용·PRD·AC는 불변이고, Pages 활성화(Settings → Pages)만 사용자 작업으로 남는다. | 문서 38개 · 진입점 없음(클론 또는 GitHub 파일 뷰로만 열람) | 문서 38개 · 허브에서 38/38 도달 · Pages 설정 대기 |
| 2026-08-30 | 분할 대기 6개 파일 중 `opensearch.py`(11 AC)·`aws_config.py`(2 AC)를 **AC별 전용 파일 13개**로 분할했다(`opensearch_{search,document_put,document_delete}_ac*.py` · `aws_config_get_ac{1,2}.py`). 이 둘을 고른 근거는 **선결 판단이 없는 유일한 후보**라는 것이다 — 케이스가 이미 케이스별 전용 인덱스·유일 질의 토큰으로 서로 격리돼 있어 순서 의존이 없다(남은 4개는 순서 의존·`auth-variant` 배차 증가·규칙 3 재분류라는 미결 판단을 각각 안고 있다). 공유 표면은 매칭 단위에서 제외되는 `_opensearch.py`·`_aws_config.py`로 내렸고, 케이스 함수·상수는 ast 소스 세그먼트로 옮겨 **32개 정의 전부가 원본과 AST 동일**함을 대조로 확인했다(재작성은 파일별 `run()`과 모듈 docstring뿐, 단언 무변경). 러너가 파일을 자동 발견하므로 신규 CI 스텝·픽스처·롤아웃 대기 없음 — `ci.yml` 변경은 삭제된 파일명을 가리키던 주석 2줄뿐이다. `추가 인자: trace` 신고는 http-trace를 실제로 읽는 4개 파일로 좁혀졌다. tests/·docs/ 변경이라 as-is 해시 변경 + doc-tracker 레지스트리 갱신(prd 불변). | ✅ 전용 파일 9 · ⬜ 분할 대기 40 · ⬜ 공백(케이스 없음) 14 · 🚫 예외 1 | ✅ 전용 파일 22 · ⬜ 분할 대기 27 · ⬜ 공백(케이스 없음) 14 · 🚫 예외 1 |
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
