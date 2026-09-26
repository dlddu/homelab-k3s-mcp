# 2026-09-26 — 리소스 도구 계층 다섯 컴포넌트의 복원 경로 ①② 판정 (슬라이스 55)

`rct_20260926-0025`. 판정 축이 `③④` 에 멈춰 있던 38행 1,759줄 가운데 **`resource_proxy`·`resource_exec`·
프리컨디션 읽기·제네릭 리소스 도구와 그 읽기 스모크로 이루어진 다섯 컴포넌트(줄 주석 5행 + docstring 1행 = 6행,
11파일, 399줄)** 를 ①② 축에서 전건 판정했다. **주석 103줄 제거**(줄 주석 98 · docstring 5), 코드 0줄,
Go 쪽 비주석 diff 0줄.

## 왜 이 다섯 컴포넌트인가 — 예산은 산술로 닫았다

정의 「판정 슬라이스」 절은 「판정 축이 `①②③④` 가 아닌 행을 **파일 공유로 묶은 덩어리 단위로 주석 400줄까지**
채워 가져간다 · 예산보다 작게 끝내려면 사유를 계획에 적는다」를 요구한다. tip 의 ③④ 38행(L 26 · D 12)을 파일
공유 union-find 로 묶으면 **27 컴포넌트**이고, 그중 리소스 도구 계층 다섯을 채우면

```
internal/k8s/proxy.go 외 3파일 124 · internal/k8s/precondition.go 외 1파일 103 ·
internal/mcp/exec.go 외 2파일 98 · internal/mcp/resource.go 53 ·
tests/integration/resource_read_smoke.py 21(L 2 + D 19) = 399
```

이고, 잔여 전체에서 **가장 작은 컴포넌트가 10줄**(`D:tests/integration/smoke.py`)이라 하나만 더해도
**409 > 400** 이다.

크기순 1위(`internal/gatekeeper` 125)를 받지 않은 것은 슬라이스 50·52·54 가 세운 판별식 때문이다 — 묶는 축은
크기가 아니라 **② 를 소유하는 문서가 하나인가**이고, 이 다섯은 `docs/prd-resource-generic.md`(AC3·AC4·AC5·
AC6·AC8·AC10·AC12·AC13·AC15) 와 그 짝인 `docs/test-resource-generic.md` 시나리오 15 자동화 칸,
그리고 `docs/prd-approval-gate.md`(AC3·AC6·AC11) 로 **전부 닫힌다**. 감지가 넘긴 크기순 1순위는
`internal/gatekeeper` 였고 그 행의 ② 는 다른 문서 쌍이 소유한다.

## 판정 전 원장 전수 grep — 「닫힌 행이 내 파일을 ① 원본으로 드는가」

슬라이스 50 의 규칙대로 판정 **전에** 보존 목록을 고정했다. ⚠️ **경로 전문이 아니라 basename 으로 세야 한다** —
`tests/integration/resource_read_smoke.py` 전체 경로로 세면 히트가 9행인데, 원장 본문은 등재 docstring 을
`resource_read_smoke.py` 로만 적는 자리가 있어 **basename 으로는 19행**이 나온다(2.1배). 실제 의존은 셋이었다.

1. 🔴 **원장 `#24`**(`internal/k8s/resource.go`, `①②③④` 로 닫힘)가 `tableAccept` 본문 6줄을 접으며 ① 소유자로
   「같은 행의 `tableNegotiatingServer` doc 과 **등재 docstring `resource_read_smoke.py`**」를 명시 지목한다
   ⇒ `resource_read_smoke.py` **모듈 ¶1(Table 협상 어긋남)은 유지**.
2. 🔴 같은 `#24` 가 `DeleteResource` 본문 4줄을 접으며 ① 소유자로 `mcp/resource.go::deletionText` doc 을
   지목한다 ⇒ **`deletionText` doc 유지**.
3. 🔴 **원장 `#111`** 이 자기 함수 docstring 요약 줄 다섯을 유지하며 「같은 날 원장 `#9` 가
   `resource_read_smoke.py` 의 같은 형태를 **관례 최소 요약**으로 유지했다」를 선례로 든다 ⇒ **그 요약 줄 유지**.

나머지 열여섯 히트는 전부 **코드 식별자**를 가리켰다 — `internal/k8s/exec.go::streamMaxOutputBytes`(원장
`#52`·`#60`·`#63`), `internal/k8s/proxy.go::ProxyVerbForMethod`(`#65`), `internal/mcp/proxy_test.go`·
`internal/k8s/proxy_test.go` 의 **테스트 이름**(`#108`), `internal/k8s/resource.go::scaleTargetRefFor`·
`ListDefaultLimit`(`#47`·`#69`·`#83` — 이쪽은 내 범위가 아닌 동명 파일), `gate.go::updatePairs →
resource.go::parseUpdateTarget` 호출 사슬(`#89`). 식별자와 테스트 이름은 이 패스가 건드리지 않으므로
그 닫힌 행들은 흔들리지 않는다.

원장 `#33` 은 반대 방향으로 쓸모가 있었다 — `internal/mcp/resource.go::parseDeleteTarget` 의 「엉뚱한 서브리소스
거부는 정돈이 아니다」 문단에 대해 **② 미성립**(prd-approval-gate AC6·AC1 도 test-resource-generic 시나리오
10 도 그 명제를 축자로 담지 않는다)을 2026-09-26 에 이미 판정해 두었다. 그 판정을 그대로 승계해 유지했다.

## 이 행들의 수확 형태 — 「AC 본문이 주석보다 길고 정확하다」

`internal/k8s/precondition.go` 가 극단이다(103 → 54, −49). `prd-approval-gate` AC11 은 PartialObjectMetadata
프리컨디션에 대해 **문단 다섯**을 쓴다 — 왜 민감 종류만 다른지, 왜 값을 손으로 적지 않는지, 파라미터가 어긋나면
apiserver 가 오류 대신 전체 객체로 답한다는 함정, 그래서 폴백하지 않고 거부한다는 결정까지. 코드 주석은 그
문단들을 영어로 옮긴 것이었고, `precondition_test.go` 의 `metadataNegotiatingServer` doc 은 아예 AC11 문장을
**따옴표째** 담고 있었다(블록 인용 6줄). ② 가 자기 자신인 자리다.

반대로 **한 문장도 못 걷은 자리**가 `internal/k8s/exec.go` 다(56 → 53). AC12 는 네 줄짜리 절이고, 이 파일의
주석 대부분은 SPDY 실행기가 0 종료에 대해 아무것도 말하지 않는다는 상류 거동, 바이트 상한이 컨텍스트를 취소해
스트림이 데드라인이 아니라 취소로 돌아온다는 자리, 분기 **순서**를 바꾸면 오늘 맞게 답하는 갈래가 조용히
깨진다는 가드다. 문서는 그중 어느 것도 말하지 않는다.

## 부재를 말하는 주석은 여전히 걷을 수 없다

`internal/mcp/exec.go` 의 「컨테이너 개수 검사가 **일부러 없다**」 문단과 `internal/k8s/proxy.go::resourceProxy`
의 「이 줄에 거부를 더하면 그것이 곧 뒷문 허용목록이다」는 시나리오 11·15 가 관련 문장을 갖고 있어도 유지했다.
전자는 **부재**를 말하고(복원 경로 넷 어디에도 부재의 자리가 없다), 후자는 **앞으로의 편집을 막는 금지**다.

## 주석↔주석 사본 — 관측 자리 누적 열하나

이 슬라이스에서 둘이 더 나왔다.

- `internal/k8s/proxy.go::splitProxyPath` doc ↔ `internal/k8s/proxy_test.go::TestSplitProxyPathKeepsTheQuery`
  doc (rest.Request 의 suffix 이스케이프)
- `internal/k8s/proxy.go::apiserverRefusal` doc ↔ `TestApiserverRefusalIsToldFromTheTargets` doc
  (apiserver 의 403 과 프록시 대상의 403 을 본문으로 가른다)

`README.md` 의 제거 유형 ③ 이 이 모양을 제거 대상으로 예시하지만 **복원 경로 넷에 자리가 없다.** 누적 아홉이던
관측 자리가 열하나가 됐다 — README 개정 슬라이스가 선결이고, 그 전까지 유지가 정답이다.

## 무영향 증명 — 세 축

1. **Go 열 파일: 비주석 diff 0줄.** `git diff -U0` 의 `+`/`-` 줄에서 `//` 줄과 빈 줄을 뺀 나머지가 0이다.
   `gofmt -l internal/` 빈 출력 · `go vet ./...` rc=0 · `go test ./internal/k8s/... ./internal/mcp/...` 통과.
2. **Python 한 파일: docstring 을 `n.body[1:]` 로 걷어낸 `ast.dump` 양 끝 대조 SAME.** 이 파일의 삭제 5줄은
   전부 docstring 본문이라 `^-\s*#` 감사가 구조적으로 못 보는 자리다.
3. **주석 본문 문자 부분 수열 — 새 낱말 0건.** 게이트 추출기로 열한 파일의 줄머리 주석과 모든 docstring 을
   **문서 순서로** 이어 붙여 `casefold` + 비영숫자 제거로 정규화하면, 편집 후 문자열이 편집 전 문자열의
   부분 수열이다(20,551자 → 14,756자, 11/11 성립). 음성 프로브 둘이 발화한다 — 낱말 하나를 주입하면 False,
   역방향(편집 전 ⊆ 현재)도 False.

   ⚠️ **이 프로브는 정렬된 목록으로 돌리면 안 된다.** 게이트의 `comment_lines()` 는 줄을 정렬해 돌려주는데,
   이번 패스는 문장을 걷으면서 **남은 줄을 재배치(reflow)** 했으므로 정렬 위치가 움직여 거짓 FAIL 이 난다.
   문서 순서로 돌려야 명제가 선다.

   ⚠️ **게이트 추출기를 임시 트리에 쓰려면 `g.REPO_ROOT` 를 그 트리로 바꿔야 한다.** `comment_lines()` 는
   모듈 상수 `REPO_ROOT` 에서 파일을 읽으므로 `chdir` 만으로는 **양 끝이 같은 트리를 읽어 0 차이로 공전한다**
   (이번에 한 번 밟았고, 「편집 전 21,012자 → 현재 21,012자」라는 불가능한 값으로 드러났다).

   🔴 **프로브가 실제로 두 자리를 잡았다.** 첫 판에서 `TestProxyOutcomeReportsItsEncoding` 에 「bytes that are
   not UTF-8」, `parseExecTarget` 에 「AC12's arguments」라고 **원문에 없는 낱말**을 지어 넣었다. 전자는
   선행사(「/metrics is text and a container's gzip is not」)를 지우면서 대명사 「the second」가 뜬 자리라
   그 블록을 통째로 유지로 되돌렸고, 후자는 원문 낱말 「one shape」로 바꿨다.

4. **기계 판독 선언 생존**: `python3 tests/integration/check_ac_mapping.py` rc=0 — 시나리오 전집 99 · 예외 2 ·
   구현 대기 4 · 1:1 대상 93 · 매칭 파일 93 · 규칙 2 위반 0 · 공백 0. `run_all.py` 가 읽는 선언 필드
   다섯(`검증 시나리오`·`실행 대상`·`실행 순서`·`병렬 레인`·`추가 인자`)이 `resource_read_smoke.py` 에서
   그대로다.

## 실측값 (base `60fc067`)

- 줄 주석 **3,536 → 3,438**(−98) · 161파일 불변 · 판정 완료 58행2,323(65.7%) → **63행2,323(67.5%)**
  · 축별 `③④` **26행 → 21행**
- docstring **1,701 → 1,696**(−5) · 112파일 불변 · 완료 22행1,155(67.9%) → **23행1,155(68.1%)**
  · 축별 `③④` **12행 → 11행**
- 줄 끝 주석 4줄 100.0% **불변** — 이 열한 파일의 E 표면은 0줄이라 원리적으로 무접촉이다
- 허브 도달 **115 / 115 → 116 / 116**(정책 75 → 76)

## 범위 밖 (후속)

- ①② 미판정 잔여 **32행 1,360줄**(L 21행 1,115 · D 11행 245). 파일 공유 union-find 22 컴포넌트,
  내림차순으로 `internal/gatekeeper` 125 · `internal/mcp/exec.go` 를 뺀 나머지 Go 행들
  (`internal/server` 97 · `internal/auth` 79 · `internal/metrics` 76) · `run_all.py`+`check_ac_mapping.py` 115
  · `_metrics.py` 가족 111 · `approval_gate_ac6.py` 가족 92 · `tests/k8s/kind/oidc-fixture.yaml` 84 ·
  `session-platform.yaml` 61 · `gatekeeper-fixture.yaml` 57 · `github_commit_status_ac1` 가족 57.
- **주석↔주석 사본 축**: README 제거 유형 ③ 이 예시하는 모양에 복원 경로 넷의 자리가 없다(관측 자리 누적 열하나).
  README 개정이 선결이다.
- **D 표면 262줄 오프셋**: 게이트의 D 실측(1,696)과 모델 지문의 `docstring=`(1,958) 이 상수 262 만큼 갈린다.
  게이트 제외 목록발 상수이고 control plane 소관이다.
