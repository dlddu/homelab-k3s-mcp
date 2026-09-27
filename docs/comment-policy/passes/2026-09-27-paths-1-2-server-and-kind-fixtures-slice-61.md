# 슬라이스 61 — 복원 경로 ①② 판정: MCP 표면 · kind 픽스처 · 이벤트 기록 e2e

`rct_20260927-0005` (2026-09-27). 원장 9행 394줄을 복원 경로 ①②에 대고 판정해 **158줄을 걷었다**
(L 132 · D 26). 코드 0줄, 비주석 diff 0줄.

판정 결과와 행별 근거는 [`../ledger.md`](../ledger.md)의 해당 행에 있다. 이 파일은 그 판정을
**어떻게 골랐고 무엇으로 검산했는지**를 남긴다.

## 슬라이스를 고른 방법

잔여 ①② 미판정은 11행 425줄이었고 예산은 400줄이다. 파일 공유로 묶으면 8덩어리다 —
`internal/server` 97 · `oidc-fixture.yaml` 84 · `session-platform.yaml` 61 ·
`resource-generic-fixture.yaml` 가족 61(L45 + D16) · `event_log_ac3` 가족 43(L3 + D40) ·
`http-trace.yaml` 32 · `check_doc_inventory.py` 31(L7 + D24) · `minio.yaml` 가족 16.

25줄 이상을 빼야 예산에 든다. 뺄 수 있는 후보는 `check_doc_inventory.py` 31 · `http-trace.yaml` 32 ·
`event_log_ac3` 43 셋이고, **`check_doc_inventory.py`를 뺀 394가 예산 안에서 가장 큰 값**이다
(`http-trace.yaml`를 빼면 393).

크기만으로 고르지 않았다. 슬라이스 60이 세운 규칙 — **② 소유자로 묶어라** — 로 다시 재니 같은
답이 나왔다. 각 덩어리의 ② 소유자는 이렇다.

| 덩어리 | ② 소유자 |
| --- | --- |
| `internal/server` | `test-session-{read,write,list}.md` · `test-github-commit-status.md` · `test-opensearch-search.md` · `test-platform-auth-safety.md` · `prd-metrics.md` |
| `oidc-fixture.yaml` | `test-platform-auth-safety.md` · `e2e-mocking-policy.md` |
| `session-platform.yaml` | `test-session-list.md` · `e2e-mocking-policy.md` |
| `resource-generic-fixture.yaml` 가족 | `test-resource-generic.md` |
| `event_log_ac3` 가족 | `test-event-log.md` · `test-resource-generic.md` |
| `http-trace.yaml` | `e2e-mocking-policy.md` · `doc-tracker/2026-09.md` |
| `minio.yaml` 가족 | `e2e-mocking-policy.md` |
| `check_doc_inventory.py` | `doc-tracker/2026-09.md` 「문서 공개」 표 |

`http-trace.yaml`을 빼면 `e2e-mocking-policy.md`를 이번에 읽고 다음 슬라이스에서 **또** 읽어야 하고
(`minio.yaml` 가족·`session-platform.yaml`·`oidc-fixture.yaml`이 같은 소유자다),
`test-event-log.md`도 `event_log_ac3` 가족과 갈린다. `check_doc_inventory.py`의 소유자는
doc-tracker의 「문서 공개」 표 하나이고 그 표를 읽는 다른 덩어리는 없다 — **뺐을 때 ② 소유자가
갈리지 않는 유일한 덩어리다.** 두 기준이 같은 답을 줬다.

## 판별식

**③④는 이미 판정된 9행이다.** 정의의 「일부 축만 끝난 기존 행은 남은 축만 채우면 된다」대로
①②만 물었다. ③④ 앵커는 새 지문에 다시 못박았고(제거·정정뿐이라 남은 주장 집합이 판정 당시
집합의 진부분집합) 옛 앵커는 지우지 않았다.

**② 는 「그 문서가 이 명제를 축자로 담는가」이고, 그 앞에 실재성 검사가 있다.** 슬라이스 60이
`test-approval-gate.md`에서 밟은 함정 — 자동화 칸이 드는 Go 단위 이름 일곱이 전부 **계획**이고
실재하지 않았다 — 을 이번에는 `grep -n '^func Test' internal/server/*_test.go`로 먼저 걸렀다.
`internal/server/mcp_test.go`를 드는 문서 열둘의 함수 이름은 **전부 실재한다**. 그래서 이 파일의
AC 재진술은 ② 커버리지가 맞고, 라벨을 걷었다.

**유지의 판별식은 「어겼을 때 조용히 깨지는가」 하나다.** 통과하면서 의미를 잃는 자리만 남겼다 —
핸들러가 셀렉터를 로컬에서 걸러도 밖에서 똑같이 보인다 · clamp는 「호출이 통했다」를 통과시킨다 ·
`idempotentHint`를 조용히 뺀 후계자도 통과한다 · 시크릿을 한쪽만 고치면 바늘이 영영 안 걸려
네거티브가 공허해진다 · `병렬 레인:`을 선언하면 다른 파일의 전제가 조용히 깨진다.

**부재를 말하는 주석은 복원 경로 넷 어디에도 없다.** 원장이 여러 번 고정한 처리를 그대로 따랐다 —
AC10·AC11이 광고되지 **않음**을 재는 이유, `oidc-fixture.yaml`이 자격 시크릿을 붙이지 않는 이유,
`session-platform.yaml`이 두 ClusterRoleBinding을 일부러 만들지 않아 권한을 **좁힌다**는 것,
`resource-generic-fixture.yaml`의 Service에 셀렉터도 엔드포인트도 두지 않는 이유.

**같은 관용구의 처분은 이미 닫힌 자매 파일이 정한다.** `// The negative half …` 세 자리 중 둘이
`internal/k8s/resource_test.go`(①②③④ 완료)에 **남아 있다**. 그래서 `mcp_test.go`의 같은 자리도
남겼다 — 슬라이스 58이 `internal/mcp/resource_test.go`에서 같은 관용구를 걷은 것은 그 파일의
자매가 없었기 때문이고, 관용구 자체에 대한 판정이 아니었다.

## 표면 경계 둘

**`http-trace.yaml`의 임베드 스크립트 docstring은 이 표면 밖이다.** YAML 블록 스칼라 안의
문자열이라 `ast`가 보지 않고 D 표면에 들지 않는다. 머리 주석과 그 docstring이 같은 명제를 두 벌
적는 자리(투명성·기록 범위)는 **주석↔주석 축(③)의 관측 자리**로 남긴다 — 이 슬라이스는 ①②만
물었으므로 여기서 처분하지 않는다.

**`# mock-exception:` 지시자는 판정·지문 양쪽에서 제외된다.** 그 바로 위의
`등재: docs/e2e-mocking-policy.md …` 표기 줄만 걷었고, R4가 요구하는 「ID 토큰 줄 바로 앞」 위치는
지시자가 한 줄 올라와도 유지된다(`scripts/check_mock_policy.py` rc=0 재확인).

## 무영향 검산 넷

1. **비주석 diff 0줄** — `git diff -U0`에서 `//`·`#`로 시작하지 않는 ±줄이 Python docstring 본문
   말고는 없다.
2. **YAML 파싱 결과 동일** — 픽스처 8파일 중 7파일이 `safe_load_all` 정렬 덤프 md5 동일.
   `http-trace.yaml`만 다르고, 그 차이는 임베드 스크립트 문자열 안의 주석 5줄이다: 임베드
   `trace_proxy.py`의 **AST 덤프가 동일**(`90d69b9d79aa`)하고 그 문자열 diff 5줄이 전부 주석이며
   `data` 밖은 바이트 동일이다.
3. **Python 본문 동일** — docstring을 걷은 AST 덤프가 3/3 SAME.
4. **Go 전건** — `gofmt -l` 공백 · `go vet ./...` rc=0 · `go test ./...` 12패키지 ok / FAIL 0.

## 음성 프로브

게이트가 이 편집면을 실제로 보는지를 커밋 뒤에 확인했다. 결과는 PR 본문에 있다.

## 범위 밖 (후속)

- **`check_doc_inventory.py` 2행 31줄**(L7 + D24) — 예산 때문에 뺐다. 잔여 ①②는 이 한 덩어리뿐이고,
  다음 슬라이스가 가져가면 **①② 축이 닫힌다**. ② 소유자는 `doc-tracker/2026-09.md`의 「문서 공개」 표
  하나다.
- **`http-trace.yaml`의 ③ 관측 자리** — 머리 주석 ↔ 임베드 `trace_proxy.py` 모듈 docstring이 투명성과
  기록 범위를 두 벌 적는다. 정본은 명제를 어기는 편집을 하는 자리(프록시 구현)라 docstring 쪽이고,
  머리 주석이 사본이다. D 표면에 들지 않는 문자열이라 **행이 없는 정본**이라는 것이 이 자리의 특이점이다.
