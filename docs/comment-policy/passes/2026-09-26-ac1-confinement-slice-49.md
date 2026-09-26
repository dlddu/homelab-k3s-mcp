# 슬라이스 49 — AC1 호출 제한 가족을 세 표면에서 네 축 전건 판정

- **task**: `rct_20260926-0019` (`tbm_homelab-k3s-mcp-comment-redundancy`)
- **base**: `18f43ec809c5a4ede8d5402d66650d41d063d124`
- **범위**: 원장의 미판정(`—`) 13행 전건 — 줄머리 주석(L) 5행 184줄 · Python docstring(D) 1행 48줄 ·
  줄 끝 주석(E) 7행 9줄, 합계 **241줄**
- **결과**: 주석 **48줄 제거**, 비주석 diff **0줄**. 세 표면의 미판정 행이 **0** 이 된다

## 왜 이 13행이 한 슬라이스인가

직전 패스(슬라이스 48 · PR #248)가 잔여 484줄을 CI `changes` 잡 경계로 갈라 `docs/`·`scripts/` 쪽
243줄을 가져가고 **코드 레인 241줄을 다음 몫으로 남겼다.** 이 패스가 그 241줄이다 — 남은 13행이
모두 `internal/`·`tests/` 이라 `go`·`app`·`image` 세 필터가 함께 켜지고, 쪼개도 같은 CI 를 두 번
돌릴 뿐 얻는 것이 없다.

13행은 주제로도 한 덩어리다. `prd-approval-gate` AC1(호출 시 선언 밖 행사 차단)이 낳은 파일
넷(`confine.go` · `confine_test.go` · `undeclared_tool_probe.go` · `approval_gate_ac1.py` ·
`gate-refusal-variant.yaml`)이 L·D 여섯 행을 차지하고, E 일곱 행 중 셋이 같은 PR(#241)에서 왔다.

## 판정 요약

| 표면 | 행 | 범위 | 줄 수 | 판정 |
| --- | ---: | --- | --- | --- |
| L | 1 | `internal/mcp/confine.go` | 60 → 44 | 제거 16 (② PRD AC1 축자 8 · ① 자기 선언 8) |
| L | 2 | `internal/mcp/confine_test.go` | 33 → 20 | 제거 13 (전부 ② — AC1 설명·검증 방법 축자) |
| L | 3 | `internal/mcp/undeclared_tool_probe.go` | 26 → 21 | 제거 5 (① 빌드 태그 1 · ② 시나리오 1 (c) 4) |
| L | 4 | `tests/integration/approval_gate_ac1.py` | 16 → 16 | **전건 유지** (수치 근거 · 관측된 실패 모드) |
| L | 5 | `tests/k8s/kind/gate-refusal-variant.yaml` | 49 → 49 | **전건 유지** (부재의 의도 · 순서 의존 · 편집 구속) |
| D | 1 | `tests/integration/approval_gate_ac1.py` | 48 → 39 | 제거 9 (전부 ② — 시나리오 1 실행 단계·자동화 칸 축자) |
| E | 1 | `internal/auth/auth_test.go` | 3 → 1 | 제거 2 (① 테스트 이름·바로 위 doc) |
| E | 2 | `internal/mcp/confine_test.go` | 1 → 0 | 제거 1 (① `touchesNothing` 분기) |
| E | 3 | `internal/mcp/eventlog_test.go` | 1 → 1 | 유지 (필드 이름이 대립항을 말하지 않는다) |
| E | 4 | `internal/mcp/gate_test.go` | 1 → 1 | 유지 (헬퍼 기본 상태 ↔ 케이스 명제의 유일한 연결) |
| E | 5 | `internal/mcp/mcp.go` | 1 → 0 | 제거 1 (① 다음 줄) |
| E | 6 | `internal/mcp/resource_test.go` | 1 → 1 | 유지 — **선행 판정 승계** (아래) |
| E | 7 | `tests/k8s/kind/http-trace.yaml` | 1 → 0 | 제거 1 (① 다음 세 줄) |

행별 근거의 전문은 `ledger.md` 의 결과 칸에 있다. 이 문서는 표 밖의 세 가지만 적는다.

## ⑴ 선행 판정을 뒤집지 않았다 — `// "super-secret-bytes"`

E 행 `internal/mcp/resource_test.go` 의 한 줄은 **E 표면이 생기기 전에 이미 판정된 자리**다.
`rct_20260914-0012`(PR #93)가 「지문 사각지대 1건을 함께 판정했다」로 이 줄을 이름 들어 **유지**로
닫고 `passes/2026-09-14-resource-patch-unit.md` 에 기록했다. 2026-09-26 표면 개정이 E 원장을
세우며 이 줄에 행을 주었고, 그 행은 「처음 세는 값」이라 축이 `—` 였다 — 즉 **판정이 없는 것이
아니라 새 축으로 다시 묻지 않은 것**이다.

그래서 걷기 전에 그 논거의 **전제**를 다시 쟀다. 논거는 「`(masked, 18B)` 단언의 `18` 이 어디서
오는지 그 줄만이 준다」인데, 그 단언이 지금도 같은 파일에 살아 있다. 전제가 유효하므로 결론도
유효하고, 이번 ①② 판정은 그 결론을 **승계**한다.

③ 축에서는 이 평문이 PR 42개 본문에 **한 번 걸린다.** 그러나 그 히트는 저작 PR 이 아니라
**이 정책의 판정 패스 PR(#186)** 이 「유지」를 설명하려 인용한 것이다. 판정 패스의 인용을 ③ 히트로
세면 「주석을 유지한 근거가 그 주석의 제거 근거가 되는」 자기 무효화 고리가 생긴다 — 모집단은
저작 PR 아홉이고, 거기에는 없다.

## ⑵ 쌍둥이가 둘 있었고, 정본을 먼저 세운 뒤 사본을 걷었다

같은 명제가 두 파일에 있는 자리가 둘이다. 둘 다 **그 명제를 어길 사람이 읽는 자리**를 정본으로
남기고 반대쪽을 걷었다.

- 「등록 표와 광고면이 컴파일 시점 상수라 env·매니페스트로는 그 변형을 만들 수 없다」 —
  `undeclared_tool_probe.go` 머리말과 `gate-refusal-variant.yaml` 머리말. 이 명제를 어기는 것은
  **매니페스트로 변형을 만들려는 사람**이므로 정본은 yaml 쪽이다. Go 파일의 사본을 걷었다.
- 「게이트 시크릿에 `GATEKEEPER_USER_ID` 가 없어 자동 응답이 걸리지 않는다」 —
  `approval_gate_ac1.py` 모듈 docstring과 `gate-refusal-variant.yaml` 머리말. 그 부재를 깨는 것은
  **시크릿을 만드는 사람**(ci.yml·yaml)이므로 정본은 yaml 쪽이다. docstring 의 (c) 문단을 걷으며
  이 사본도 함께 사라졌다.

**부재를 말하는 주석은 복원 경로 넷 어디에도 없다** — 그래서 둘 중 한 벌은 반드시 남는다.

## ⑶ 두 파일은 전건 유지다

`approval_gate_ac1.py` 의 L 16줄과 `gate-refusal-variant.yaml` 의 49줄은 **한 줄도 걷지 않았다.**
전자는 `#:` 모듈 속성 doc 이 임계값의 *근거*만 담고(값 자체는 리터럴에 있다), 후자는 넷을 말한다 —
수치 근거(5초 대 300초) · 부재의 의도 · `auth.FromEnv` 가 `mcp.Validate()` 보다 먼저 돈다는 순서
의존 · ci.yml 이 이 배포의 롤아웃을 **일부러 기다리지 않는다**는 편집 구속. 넷 다 어기면 조용히
깨지고, 어느 문서도 그것을 담지 않는다.

`docs/test-approval-gate.md#시나리오 1` 은 「자동 응답을 끄고 판정하지 않는다」라는 **요구**를 적고,
그 요구를 만드는 **구성**은 적지 않는다. 요구와 구성 사이의 이 간격이 이 두 파일의 주석이 사는
자리다.

## 범위 밖 (후속)

- **게이트 D 필터의 앵커 부재.** `docstring_lines()` 가 `DIRECTIVE_RE.search(f"{rel}:{line}")` 로
  지시자를 판정해, 지시자 이름을 *인용한* 산문 docstring 까지 D 인구조사에서 뺀다. base `18f43ec`
  에서 그렇게 빠지는 줄은 **실측 0** 이라 지금은 잠재적 결함이고, 이 슬라이스가 그 수를 움직이지
  않는다(판정 전후 모두 0). 고치면 `scripts/check_comment_policy.py` 가 슬라이스 48 에서 막 판정한
  두 행의 파일을 다시 건드리게 되므로, **다음 슬라이스의 몫으로 남긴다.**
- **①② 축이 빈 행 72개**(L 47 · D 25). 게이트가 `③④` 로 적는 행들이고, 이 패스의 범위가 아니다.
