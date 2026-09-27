# 2026-09-27 — 슬라이스 59: 주석 사본 사슬의 정본 지정 + github-commit-status 축 ①② 판정

`rct_20260927-0003` (모델 `tbm_homelab-k3s-mcp-comment-redundancy`). base `f39d5a1`.
원장 행의 결과 칸이 판정 그 자체를 담고, 이 파일은 **패스 국소 서술**(선택 근거·예산 산술·
프로브·인계)만 담는다.

## 이 슬라이스가 가져간 것

두 몫이다.

- **⒜ 만료 표지 3행의 재판정** — `rct_20260927-0002`(슬라이스 58)가 결과 칸에 「제거 여부의
  재판정은 이 행을 집는 다음 슬라이스의 몫이다」라고 적어 인계한 자리. 표지가 붙은 행은
  `판정 축` 이 `①②③④` 라 **예산 모집단(축이 미완인 행) 밖**이고 게이트의 `③④ N행` 목록에도
  없어 스스로 깨어나지 못한다(0 → 0). 「안 하면 영영 안 되는 쪽」이 이 몫이다.
- **⒝ ①② 판정** — `github-commit-status` 축 세 컴포넌트 **137줄**
  (`internal/github/commitstatus.go` 가족 32 · `github_commit_status_ac1` 가족 L 11 + D 46 ·
  `github_commit_status_ac3` 가족 L 17 + D 31).

## ⒜ — 번복의 범위를 어디까지로 잡았는가

정책은 「번복은 만료된 **그 논거 하나**에만 미친다」로 범위를 좁힌다. 표지 세 행의 논거를
문면 그대로 읽으면 대상이 이렇게 갈린다.

| 표지가 붙은 행 | 만료된 논거가 이름으로 드는 자리 | 이 PR 이 건드린 행 |
| --- | --- | --- |
| `internal/k8s/port_forward.go` 가족 | `parseExecTarget` → `parseAttachTarget` → `parsePortForwardTarget` **세 doc** | `port_forward`(−2) · `attach`(−3) · `exec`(−2) |
| `internal/mcp/resource_test.go` | `internal/mcp/resource.go::parseDeleteTarget` doc | `resource_test`(−3) |
| `tests/integration/github_app_installation_token_ac5.py` | 자매 `github_commit_status_ac3.py` docstring | `token_ac5`(−2) |

첫 행의 논거가 **세 doc 을 이름으로 들기 때문에** 그 논거의 번복은 세 자리에 미친다. 그래서
표지가 붙지 않은 두 행(`internal/k8s/attach.go` 가족 · `internal/mcp/exec.go` 가족)도 함께
갱신했다 — 표지를 세운 슬라이스 43 이 **port_forward 행과 attach 행을 한 슬라이스로** 물었고,
그 패스의 문면이 사슬 셋을 한 명제로 묶는다. 하나만 접으면 같은 문장이 한 표면 안에서 갈린다.

`internal/mcp/gate.go` 행(−3)은 다른 이유로 들어왔다. 정본이 그 파일의 `updatePairs` doc 인데
**같은 파일의 `deletePairs` doc 이 같은 명제를 한 번 더 되풀이**한다 — 슬라이스 58 이 이 행의
표지를 `gateTargetKind` 자리로 해소할 때 남은 사본이다. 정본과 같은 파일이라 접는 비용이 0 이고,
접지 않으면 「사이트 하나」라는 완료 기준이 서지 않는다.

**완료 기준(기계로 세어진다)**: `grep -rc "authorize runs before" --include=*.go .` 가
**5 → 1**(정본 `gate.go::updatePairs` 하나). 규칙 ⑵(N벌이면 N−1벌)가 그 하나를 지킨다.

### 만료 표지 전수성 — 다섯 패턴으로 다시 셌다

슬라이스 58 이 「#257 의 스윕이 명제를 두 문면으로만 세어 세 행을 흘렸다」를 잡았으므로 이번에도
문면이 아니라 **명제**로 다시 셌다. 원장 행 전수에 대해 다섯 정규식을 돌렸다 —
`주석\s*→\s*주석` · `주석\s*↔\s*주석` · `(복원\s*경로)\s*(①~④|넷)?\s*(밖|어디에도\s*없)` ·
`다른\s*주석이?\s*(이미)?\s*말` · 표지 문면 자체. 히트 19행을 한 건씩 읽어 갈랐다.

- **표지 7행**(#257 이 4 · 슬라이스 58 이 3) — 4행은 슬라이스 58 이 해소, **3행이 이 패스의 몫**.
- 세 번째 패턴의 히트 12행은 **전부 정상 유지**다 — 「부재를 말하는 주석」(`github.go`
  `mintInstallationToken` · `gate-refusal-variant.yaml` 게이트 시크릿 · `exec.go` 컨테이너 개수)과
  구조적 이유라 어느 복원 경로에도 없다는 뜻이고, 「다른 주석이 말한다」 모양이 아니다.
- `main.go` 행만 같은 문면을 쓰는데 그것은 **제거 근거**로 쓴 자리다(근거를 ②·① 로 옮겨 전건
  다시 세웠다) — 번복될 결론이 없어 대상이 아니다.
- ⇒ **8번째 행 없음.** 세는 법을 남기는 것이 이 절의 목적이다.

## ⒝ — ①② 몫을 137줄로 끝낸 사유(예산 400, 263 미달)

definition 「판정 슬라이스」는 덩어리를 **400줄까지 채워** 가져가라고 하고, 좁히는 사유로
「남은 대상이 없음」과 「파일이 겹치는 열린 PR」을 든다. 열린 PR 은 **0건**이고 잔여도 0 이
아니므로 그 둘은 아니다. 사유는 셋이다.

1. **가역성 경계.** 이 PR 은 앞선 유지 판정을 **번복**하는 재판정 여섯 행을 싣는다. 번복의
   *모양*에 대한 거부(정본 지정이 틀렸다 · 만료 범위를 넓게 잡았다)는 같은 PR 의 신규 판정까지
   되돌린다 — 슬라이스 57 이 「사본 스윕과 집행을 한 PR 에 섞으면 규칙 모양에 대한 거부가 제거까지
   되돌린다」로 그은 경계와 같은 성질이다.
2. **결합된 덩어리만 가져갔다.** 이 슬라이스가 지정한 정본 하나가
   `github_commit_status_ac3.py` 모듈 docstring 이고, 그 행(docstring 표면)은 `판정 축` 이
   `③④` 였다. 지금 묻지 않으면 다음 ①② 슬라이스가 **내 정본을 지울 수 있다**. 같은 ② 소유자
   (`docs/test-github-commit-status.md`)를 공유하는 `github_commit_status_ac1` 가족(57)과 구현
   쪽 `commitstatus.go` 가족(32)을 함께 가져가 축을 쪼개지 않았다 — 쪼개면 남는 쪽이 다음
   슬라이스의 1순위로 재등장한다.
3. **잔여는 예산이 끌고 간다.** ①② 잔여 **13행 825줄**은 예산 모집단 **안**이라 다음 감지가
   그대로 연다. ⒜ 의 표지 집합과 대칭이 아니다.

산술: 남은 최소 컴포넌트는 `tests/k8s/kind/minio.yaml` 가족 16 이고, 137 에 더할 수 있는 조합은
많다(예: `internal/gatekeeper` 125 를 더하면 262). 즉 **채울 수 없어서가 아니라 위 ⑴⑵ 때문에
안 채웠다** — 그 판단을 검토가 거부하면 ⒝ 를 빼고 ⒜ 만 착지시키는 것이 복구 경로다.

## 판정에서 갈린 것 · 남긴 것

- **정본 판별식은 「그 명제를 어길 사람이 읽는 자리」 하나로 다 갈렸다.** 사슬의 명제(승인 요청은
  핸들러보다 먼저 쓰인다)를 어기는 편집은 게이트의 `authorize` 호출 순서를 바꾸는 것이고,
  `parseUpdateTarget` doc 자신이 「`updatePairs` says why it must」로 그쪽을 세워 둔다.
  `parseDeleteTarget` doc 은 떠돌이 `subresource` 거부를 빼는 사람이 읽는 자리다.
  `github_commit_status_ac3.py` 머리말은 **두 파일을 합치려는 통합자**가 읽는 자리다.
- **사본이 정본을 이미 이름으로 지목한 자리는 포인터로 접었다**(사슬 넷). 지목하지 않은 자리는
  전량 걷었다(`resource_test.go` 케이스 표 · `token_ac5.py` 머리말).
- **`token_ac5.py` 의 남은 2줄은 다섯 벌이다.** `EXPECTED_INSTALLATION_ID` 의 CI 스텝 결합 주석이
  `github_app_installation_token_ac1`·`_ac2`·`_ac4`·`_ac5` 와 `github_commit_status_ac2.py` 에
  **같은 명제로 다섯 벌** 있다(`grep -rc "Create test GitHub App secret"` = 5). 정본이 될 자리가
  이 다섯 행 밖(`_helpers.py`)일 수 있어 **이 슬라이스의 범위로 잡지 않았다** — 한 벌만 걷으면
  넷이 남아 진척이 없고, 정본을 새로 만드는 것은 비주석 코드 변경이다.
- **`ac1` 가족 L 11줄은 전건 유지**다. 포트 8093 과 `ci.yml` 포트포워드 스텝의 짝, 다른 레인이
  같은 시각에 여는 포트 집합, CI 시크릿 스텝과의 결합은 어느 문서도 대신 말하지 않는다.
- **테스트 함수 doc 은 Go 관례 최소치의 대상이 아니다.** `TestXxx` 는 대문자로 시작하지만
  `go doc` 이 테스트 파일을 내지 않으므로 「exported 1줄 doc」 규칙이 걸리지 않는다 — 슬라이스 58 이
  `internal/k8s/resource_test.go` 의 3줄을 대체 없이 걷은 선례를 따랐다. 같은 이유로 e2e 의
  `AC: <도메인>/ACn` 첫 줄 열은 걷었다(`resource_generic_ac13.py` 등 이미 ①② 를 닫은 파일에 그
  형태가 남아 있지 않다).

## 무영향 · 프로브

- **비주석 diff 0줄**: `git diff -U0` 의 모든 `+`/`-` 줄을 훑어 주석·docstring·구분자 밖의 줄이
  없음을 확인했다.
- **docstring 걷은 AST 동등**(py 7파일) · **줄머리 주석 걷은 바이트 동등**(go 7파일) ·
  **부분 수열**(편집 후가 편집 전의 부분 수열 — casefold + 비영숫자 제거, 14파일 전건 OK).
  ⚠️ 부분 수열은 한글을 정규화에서 버리므로 **한글 개작 3줄**(`ac6.py` 줄 주석 2 · `ac3.py`
  머리말 1)은 그 프로브가 보지 못한다 — 원장 결과 칸에 `개작` 으로 적었다.
- **`run_all.py` 선언 파싱 drift 0** · **`check_ac_mapping.py` 출력 바이트 동일** ·
  **`check_mock_policy.py` 출력 바이트 동일**.
- `gofmt -l` 공백 · `go vet ./...` 무출력 · `go test ./...` 전건 ok.
- **음성 프로브**(고친 트리 rc=0 ↔ 되돌린 트리 rc=1): 상세는 PR 본문.

## 범위 밖 (후속)

- **①② 잔여 13행 825줄 · union-find 13 컴포넌트.** 다음 덩어리(내림차순):
  `internal/gatekeeper` **125** · `internal/server` **97** · `approval_gate_ac6` 가족 **92** ·
  `oidc-fixture.yaml` **84** · `internal/auth` **79** · `session-platform.yaml` 61 ·
  `resource-generic-fixture` + `resource_generic_ac1` 61 · `gatekeeper-fixture.yaml` 57 ·
  `auth-fixture.yaml` 47 · `event_log_ac3` 가족 43 · `http-trace.yaml` 32 ·
  `check_doc_inventory.py` 31 · `minio.yaml` 가족 16. **125 + 97 + 92 + 84 = 398** 이 예산을
  채운다(다음 최소 16 을 더하면 414 초과). ⚠️ `oidc-fixture.yaml` 의 ② 소유자는
  `prd-approval-gate.md` 가 아니라 `docs/test-platform-auth-safety.md` 다.
- **③ 관측 자리 — `EXPECTED_INSTALLATION_ID` 다섯 벌**(위). 정본 후보가 행 밖일 수 있어 다섯 행을
  한 슬라이스로 묶는 설계가 선행한다.
- **만료 표지 잔여 0.** 표지 7행 전부가 해소 기록을 얻었다
  (`grep -c '이 유지 근거는 만료됐다'` 7 · 그중 이 패스가 3 + 슬라이스 58 이 4).
