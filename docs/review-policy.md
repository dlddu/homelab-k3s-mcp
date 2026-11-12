# PR 수동 승인 정책

> 수동 승인 케이스 · 위험 표면 · 의도적 제외 · 미탐 원장의 SSOT. 모델 `tbm_homelab-k3s-mcp-pr-manual-approval`, 템플릿 reconciler `templates/pr-manual-approval.tbm.md`.
> 판정기(`scripts/review-policy/judge.py`)는 이 문서의 케이스 ID 를 판정 근거 라벨로 쓰고 케이스 본문을 되풀이하지 않는다.

이 레포의 PR 수동 승인은 **하나의 프로세스**다. 판정기 하나가 PR 이 아래 케이스 어디에도 닿지 않았다고 확인될 때만 head 커밋에 `review/manual-approval` = success 를 붙이고, 기본 브랜치 ruleset 이 그 status 를 required 로 요구한다. 닿았을 가능성이 있으면 판정기는 아무 status 도 붙이지 않는다 — status 의 **부재**가 곧 「사람이 본다」이고, 판정기가 죽거나 판정이 불가능해도(정책 파싱 실패 · 정책과 판정기의 케이스 ID 불일치 · 빈 변경 목록 · files API 상한) 같은 결과가 된다. 사람의 승인은 PR 을 검토한 사람이 **같은 context 로 직접 success 를 붙이는 것**(수동 status) 하나뿐이다.

케이스는 저장소 소유자가 2026-11-04 정했다. 2026-11-04 오전 등재(MA1·MA2) 뒤 같은 날 승인 게이트·인증과 RBAC 케이스(MA3·MA4)를 더했다. 배포 매니페스트, GitHub 토큰 발급·commit status 쓰기 코드, CI 워크플로 전반, 외부 백엔드(AWS·OpenSearch·Grafana 등)는 대상이 아니다 — 단 commit status 접두사 허용목록은 아래 「의도적 제외」의 트립와이어로 지킨다. 케이스를 더하거나 줄이는 것은 사람의 결정이고, 바꾸면 아래 표·surface·anchors 와 판정기를 같은 PR 에서 함께 고친다.

## 프로세스

| 항목 | 값 |
|---|---|
| status context | `review/manual-approval` |
| 판정기 | `scripts/review-policy/judge.py` (`--policy docs/review-policy.md`, 표준 입력 = 변경 경로 목록 · 개명은 옛 경로와 새 경로 둘 다; 종료 코드 0 = 케이스 미접촉, 1 = 접촉(닿은 케이스 ID 와 경로 출력), 2 = 판정 불가). 케이스마다 대상 경로와 제외 패턴(테스트 등)을 갖는다 |
| 워크플로 | `.github/workflows/review-policy.yml` — `pull_request_target`, **base 브랜치를 체크아웃**해 base 쪽 판정기로 돌고, PR 의 변경 경로는 `pulls/{n}/files` API 로 받아 판정기의 입력(데이터)으로만 쓴다. PR 코드는 체크아웃도 실행도 하지 않는다. 종료 코드 0 일 때만 status 를 붙인다 |
| 게이트 | 기본 브랜치 ruleset `default`(id 23666077, active, bypass 0)의 required status check — `ci passed`(integration 15368)와 `review/manual-approval`(integration 고정 없음) 둘. `review/manual-approval` 은 게이트 PR #309 착지 뒤 2026-11-04 에 저장소 소유자가 추가했다(먼저 추가하면 워크플로가 없는 동안 모든 PR 이 막힌다). 확인: `gh api "repos/dlddu/homelab-k3s-mcp/rules/branches/main" --jq '.[] \| select(.type=="required_status_checks") \| .parameters.required_status_checks[] \| "\(.context) integration=\(.integration_id // "any")"'` → `review/manual-approval integration=any` 가 있어야 하고 `review/` 계열 다른 context 는 없어야 한다 |
| 사람 승인 | 수동 status. 승인할 수 있는 사람: 저장소 소유자(`dlddu`). PR 을 검토한 뒤 head 커밋에 붙인다: `gh api -X POST "repos/dlddu/homelab-k3s-mcp/statuses/<head sha>" -f state=success -f context=review/manual-approval -f description="수동 승인 — <승인자>" -f target_url=<PR URL>`. 에이전트가 대신 요청하는 경로: homelab-k3s-mcp `github_commit_status_request` 가 요청만 PocketAide 에 남기고(요청할 수 있는 context 는 서버 env `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES` = `review/` 로 제한 — `k8s/deployment.yaml`), 사람이 PocketAide 앱 「커밋 상태」 탭(또는 그 알림)에서 승인할 때 PocketAide GitHub App 이 기록한다(2026-11-12 — 이전에는 `github_commit_status_create` 가 사람 확인 없이 바로 기록했다). 이 레포에서 그 네임스페이스의 status 는 `review/manual-approval` 하나다. ruleset bypass · 관리자 머지 · required check 해제는 승인 경로가 아니다. 승인 뒤 새 푸시로 head 가 바뀌면 승인은 무효가 되고 판정기가 다시 판정한다 |
| 마지막 재검토 | 2026-11-04 |
| 다음 재검토 | 2027-02-02 |

재검토 때는 날짜만 옮기지 않는다: 아래 `surface` 블록이 여전히 위험을 다 담는지(위험이 새 위치·새 기술로 나타나지 않았는지), ruleset 의 bypass 목록이 비어 있는지(admin 권한 필요)를 사람이 보고 결과를 이 절에 한 줄로 남긴다.

## 수동 승인 케이스

| ID | 대상 | 이유 |
|---|---|---|
| MA1 | 제품 문서 — 가치·PRD·테스트: `docs/values.md` · `docs/prd-*.md` · `docs/test-*.md` 의 **모든** 변경(개명은 옛 경로와 새 경로 둘 다 판정 입력이 된다) | 제품이 무엇을 왜 하는가의 결정이고, 소유자는 사용자다(`docs/values.md` 「제품 소유자」). 정합성 루프(reconciler docs-impl·scenario-e2e 등)가 어긋남을 「문서를 구현에 맞추는」 방향으로 닫으면 제품 결정이 사람 눈 없이 바뀐다. 테스트 문서는 AC 의 검증 기준을 정하므로 함께 건다. 집계(doc-tracker)·목업은 결정을 옮겨 적을 뿐이라 대상이 아니다 |
| MA2 | 판정기·워크플로·정책 자신 — `scripts/review-policy/` · `.github/workflows/review-policy.yml` · `docs/review-policy.md` | 워크플로가 `pull_request_target` 이라 PR 은 **base 쪽 판정기**로 판정된다 — PR 이 규칙을 고쳐 자기를 통과시킬 수는 없지만, 그 변경 자체(케이스를 줄이거나 자기 경로를 빼는 편집)는 옛 판정기가 보지 못한 채 다음 PR 부터 효력을 가진다. 그래서 이 경로의 변경은 사람 눈에 올린다 |
| MA3 | 승인 게이트·인증 로직 — 게이트 분류와 면제(`internal/mcp/gate.go`, `create_gate.go`, `delete_collection.go`, `confine.go`, `declaration.go`, `provenance.go`) · 승인 게이트 클라이언트(`internal/approvalgate/`) · 인증과 호출자 범위(`internal/auth/`, `internal/toolscope/`, `internal/server/server.go`) · 그 배선(`main.go`). 테스트(`*_test.go`)는 대상이 아니다 | 어떤 도구 호출에 사람 승인이 필요한지를 이 코드가 정한다 — `gatedVerbs`·도구 등록부의 `outsideGate`/`touchesNothing` 선언 한 줄이 cluster-admin 쓰기에서 사람 승인을 빼고, `RESOURCE_GATED_KINDS` 기본값이 Secret 접근을 게이트에 묶는다. `MCP_AUTH_DISABLED`·`RequireBearer` 범위·OIDC 규칙의 도구 허용과 토큰 상한·`toolscope` 필터가 누가 무엇을 부를 수 있는지를 정한다 |
| MA4 | RBAC·ServiceAccount — `k8s/rbac.yaml` · `k8s/serviceaccount.yaml` 의 모든 변경. 배포 매니페스트(`deployment.yaml` · `service.yaml` · `kustomization.yaml` · `namespace.yaml`)는 대상이 아니다 | 서버 ServiceAccount 의 `cluster-admin` 바인딩이 여기에 있다. 이 서버의 모든 클러스터 조작 권한의 상한이므로, 바뀌면 승인 게이트가 막는 범위와 무관하게 서버가 할 수 있는 일이 바뀐다 |

## 위험 표면

케이스의 위험이 레포에 나타나는 모양. reconciler as-is 지문이 이 블록에 걸리는 (파일, 토큰) 집합을 잰다 — 위험 신호가 새 파일에 나타나면 지문이 움직여 이 정책이 다시 판정된다. 걸리는 파일은 전부 위 케이스에 덮이거나 아래 「의도적 제외」에 있어야 한다. 테스트·집계·목업은 운영 경로가 아니라 표면에서 뺀다.

```surface
scope: docs internal k8s main.go
exclude: ^docs/(doc-tracker|comment-policy)/|\.html$|_test\.go$|^internal/mcp/undeclared_tool_probe\.go$
ere: ^### ([A-Z]+-)?AC[-.0-9A-Z]*|^## 제품 가치|^## Acceptance Criteria|^### 시나리오|^## 테스트 시나리오|^## 검증 대상 AC
ere: outsideGate:|gatedVerbs|exemptAcceptedByOwner|Getenv\("RESOURCE_GATED_KINDS"\)|RequireBearer|Getenv\("MCP_AUTH_DISABLED"\)
ere: ^kind: (ClusterRole|ClusterRoleBinding|Role|RoleBinding|ServiceAccount)$|- name: [A-Z_]*_CONTEXT_PREFIXES
```

## 앵커

케이스가 이름으로 지목하는 경로. 하나라도 사라지면 그 케이스는 죽은 케이스다 — 판정기와 이 문서를 새 위치로 옮기거나, 위험 자체가 사라졌으면 케이스를 지운다.

```anchors
docs/values.md
docs/prd-*.md
docs/test-*.md
scripts/review-policy/*
.github/workflows/review-policy.yml
docs/review-policy.md
internal/mcp/gate.go
internal/mcp/create_gate.go
internal/mcp/delete_collection.go
internal/mcp/confine.go
internal/mcp/declaration.go
internal/mcp/provenance.go
internal/approvalgate/*
internal/auth/*
internal/toolscope/*
internal/server/server.go
main.go
k8s/rbac.yaml
k8s/serviceaccount.yaml
```

## 의도적 제외

판정기가 보지 않지만 위 케이스의 위험에 닿을 수 있는 변경. 재개 조건이 참이 되면 그 행을 지우고 케이스를 더한다. 제외를 연장하려면 새 재개 조건을 쓴다.

| 대상 | 사유 | 재개 조건 | 소관 |
|---|---|---|---|
| `k8s/deployment.yaml` 의 `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES` 값 | 배포 매니페스트는 소유자 결정(2026-11-04)으로 케이스 밖이다. 그러나 이 값은 에이전트가 붙일 수 있는 commit status 접두사 허용목록이라, 넓어지면 `review/manual-approval` 밖의 required check 까지 위조할 수 있다 | 값이 `review/` 하나가 아니게 된다 — `git grep -A1 'name: GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES' k8s/deployment.yaml \| grep -c 'value: "\?review/"\?$'` 가 1 이 아니다 | 사람 재검토 (O3 트립와이어) |

## 미탐 원장

`review/manual-approval` 이 자동으로 붙어 머지된 PR 에서, 사람 승인이 필요했던 변경을 뒤늦게 발견하면 한 행을 더한다. 보강 케이스 ID 는 케이스 표와 판정기에 실재해야 한다. 비어 있는 것이 정상이다.

| PR | 놓친 것 | 보강 케이스 ID | 기록일 |
|---|---|---|---|
