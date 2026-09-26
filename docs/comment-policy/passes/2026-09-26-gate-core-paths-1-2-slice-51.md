# 슬라이스 51 — 승인 게이트 코어 2파일, ①② 축 전건 판정

`rct_20260926-0021` · base `2eb05c4` · 대상 원장 행 하나(줄 주석 `internal/mcp/gate.go` ·
`internal/mcp/gate_test.go`). 제거 **35줄**(gate.go 20 · gate_test.go 15). 코드 **0줄**.
416 → 381, 지문 `fcb288a609d3` → `9515ea3f1de0`.

## 왜 이 행인가 — 직전 패스가 유예한 1순위이고, 그 유예 사유는 정책이 아니다

슬라이스 50 의 패스 파일이 이 행을 감지의 1순위로 받고도 유예했다고 스스로 적는다:
「크기 대 직전 패스가 적은 예산(400줄) — 416, 단일 행이라 쪼갤 수 없이 **초과**」.

그 유예 사유를 이번에 전제부터 다시 물었고, 셋 다 유예를 지탱하지 못한다.

1. **400줄은 정책 문면에 없다.** `README.md`·`ledger.md` 어디에도 슬라이스 크기 상한이 없다.
   한 패스가 자기 사정으로 적은 수치이고, 그것을 다음 패스가 임계로 물려받으면 문서에 없는
   기준이 게이트처럼 작동하게 된다.
2. **이 행은 쪼갤 수 없다.** 원장은 행 단위로 판정하고 이 행의 범위는 2파일이다. 「초과라서
   미룬다」를 한 번 더 적용하면 이 행은 **영구히 제외된다** — 잔여 70행 중 가장 큰 단일 덩어리가
   구조적으로 닿지 않는 자리가 된다. 같은 자리를 반복 제외하는 사유는 정당성이 아니라 점검 신호다.
3. **크기의 방향이 반대다.** 416줄은 잔여 L 2,158줄의 **19.3%**, 잔여 전체 3,364줄의 **12.4%** 다.
   한 행을 닫아 판정 완료가 L 1492 → 1873줄(40.9% → 51.8%)으로 움직이는 자리는 이 행뿐이다.

## 무엇을 물었는가

이 행의 `판정 축` 은 현재 지문 `fcb288a609d3` 에 대해 `③④` 였다 — ③④ 는 2026-09-20·09-21 에
물어 **전건 유지**로 닫혔고, ①② 는 이 지문에서 **한 번도 묻지 않았다**. 게이트의 `미판정(—) 0행`
은 「③④ 까지는 마쳤다」는 뜻일 뿐이라(`README.md:190`), 이 416줄은 초록인 채로 ①② 가 비어 있었다.

### 판정 규칙(이 패스가 적용한 것)

한 명제는 **① 코드**(같은 파일의 시그니처·타입·상수·바로 아래 줄·함수 본문) 또는
**② 저장소 문서**(`docs/prd-*.md`·`docs/test-*.md`, 파일:줄로 인용 가능할 것)에서 축자 또는
그에 준하게 복원될 때만 제거한다. 「그 AC 가 왜 이 형태로만 관측 가능한가」·서 있는 불변식·
실패 모드·픽스처와 실환경이 갈리는 지점은 유지한다(`README.md` 유지 대상 3).

🔴 **주석→주석 사본은 제거 근거로 쓰지 않았다.** 복원 경로는 넷(코드·문서·PR·커밋)이고 「다른
주석」은 그중 어디도 아니다. `README.md` 의 제거 유형 ③(자기 파일 안의 중복)이 가리키는 자리가
실제로는 주석↔주석인 경우가 있는데, 그 축을 열려면 README 개정이 선결이라 이 패스는 건드리지
않았다(아래 「인계」).

## 제거 — gate.go 20줄

| 자리 | 줄 | 복원처 | 근거 |
| --- | ---: | --- | --- |
| `gatedVerbs` doc | 2 | ② `prd-approval-gate.md:103`·`:107` · ① 맵 리터럴 | 「상태를 바꾸는 verb … **예외 없이** 게이트 대상」 + 「판정은 **verb에만** 걸리고」 축자. 어느 verb 인지는 바로 아래 리터럴 다섯 줄이 말한다 |
| `toolDeclaration` doc | 2 | ② `prd-approval-gate.md:101` | 「각 도구는 자신이 행사하는 `(verb, resource[/subresource])` 쌍을 선언하고」 — 괄호 표기까지 축자 |
| `pairs` 필드 doc | 3 | ② `prd-approval-gate.md:118`–`:122` | 「「빈 선언」은 ⑵가 아니다 — 선언을 빠뜨린 도구와 아무것도 행사하지 않는 도구가 구조상 구별돼야」 + 「둘 다 없거나 둘 다 있는 도구가 하나라도 있으면 **기동이 실패한다**」 |
| `noResourcePermission` 필드 doc | 4 | ② `prd-approval-gate.md:119`–`:122` · ① `validateDeclarations` | 「⑵ 「쿠버네티스 자원 권한을 행사하지 않는다」는 **명시적 표식과 그 사유**」·「정확히 하나를 가진다」 축자. 거절 주체는 함수 본문이 말한다 |
| `GatePairs` 둘째 문단 | 6 | ② `prd-approval-gate.md` AC11 · ① `main.go:267`–`269` | AC11 이 「AC19가 결번이 되어 대조는 없어졌지만 **선언은 남긴다** — 게이트가 승인 전에 무엇을 읽는지는 … 리뷰어가 알아야 하는 사실이고」를 그대로 적는다. 「main 이 기동 시 출력한다」는 `gatePairsText` 가 실물 (인용한 호출부를 **실측 확인**했다 — 문면이 낡지 않았다) |
| `readIsSensitive` doc | 3 | ② `prd-approval-gate.md:34` | 읽기 게이트 표의 「대상이 민감 종류일 때만. **스트림은 객체 전문을 밀어 주므로 `get`과 같은 노출이다**」 — 결론과 근거가 한 칸에 축자로 있다 |

## 제거 — gate_test.go 15줄

모두 **테스트 doc 의 AC 재진술**이다(`README.md` 제거 대상 ②가 「테스트 파일 상단의 AC 목록,
테스트 이름을 산문으로 옮긴 doc 주석」을 명시적으로 든다). 「왜 이 어서션 형태인가」를 함께 담은
블록은 **건드리지 않았다** — 아래 「유지」가 그 목록이다.

| 테스트 | 줄 | 복원처 |
| --- | ---: | --- |
| `TestGatedCallIsRefusedBeforeKubernetes` | 1 | ② `prd-approval-gate.md:134` 「쿠버네티스 클라이언트가 단 한 번도 호출되지 않는다」 · ① 테스트 이름 |
| `TestSensitiveReadsAreGated` | 2 | ② `:31`–`:34` 읽기 게이트 표 · `prd-resource-generic.md:361` AC16 |
| `TestApprovalCannotBeSpentTwice` | 2 | ② `:243` 「승인 하나는 단 한 번의 쿠버네티스 API 호출만 인가한다」 · `:248` 검증 방법 축자 |
| `TestGatedCallsAreAudited` | 2 | ② `:255` 「승인 후 실행한 호출의 로그에 요청 id와 판정자가 남고, 거부된 호출은 거부 사유와 함께 남는다」 — **축자** |
| `TestAutoApprovalIsVisibleInTheToolResponse` | 2 | ② `:262` 「자동 승인으로 통과한 실행은 **도구 응답 본문에도** 자동 승인이었음을 표기한다」 |
| `TestOrdinaryReadsAndSecretListsStayUngated` | 3 | ② `prd-resource-generic.md:367` 「`list`는 예외다. Table 표현이 apiserver에서 만들어져 값이 전송되지 않으므로(AC17)」 — **축자** |
| `TestGateReadsSensitiveKindsAsMetadataOnly` | 3 | ② `prd-approval-gate.md` AC11 민감 종류 예외 절 |

⚠️ 마지막 행은 **쌍둥이가 있는 자리**다 — 같은 명제가 `gate.go` 의 `readGateTarget` 안에도 거의
축자로 있다. 둘 다 ②로 복원되지만 **둘 다 걷지는 않았다**: 이 명제를 어기는 사람은
`ref.MetadataOnly` 를 고치는 사람이고 그가 읽는 자리는 `gate.go` 쪽이다. 정본을 그쪽에 두고
테스트의 사본만 걷었다.

## 유지 — 무엇을 남겼고 왜인가

| 자리 | 남긴 이유 |
| --- | --- |
| `resolve`·`target`·`collectionTarget`·`outsideGate`·`alwaysGated` 필드 doc | 「왜 한 필드로 합치면 안 되는가」(합치는 것이 곧 AC16 위반)·「왜 bool 이 아니라 문장인가」는 AC 가 *결론*만 적고 이 전이를 적지 않는다 |
| `genericPairs`·`updatePairs`·`deletePairs`·`deleteCollectionPairs` doc | 「인자 검사가 `authorize` 보다 먼저여야 하는 이유」 — 순서를 바꾸면 승인 요청 하나를 이미 치른다는 **조용한 파손**이고, 문서는 「승인 요청조차 만들지 말 것」이라는 요구만 적는다 |
| `validateRegistry`·`Exemptions` doc | 두 실패 방향의 비대칭(도달 불가 도구 vs 미선언 도구)과 기동 로그의 존재 이유 |
| `kubeletHighPowerPaths`·`kubeletHighPowerEndpoint`·`proxyPathDetail` | 셋 다 **하지 않는 일**을 적는다(거부 목록이 아니다 · 전집이 아니다 · 대소문자 무시의 근거) — 부재는 코드에도 문서에도 없다 |
| `maskCredentialTree`·`maskJSONPatchOps`·`maskedValue`·`withheld` | RFC 6902 가 값을 `path` 옆에 실어 트리 탐색이 못 보는 것 · base64 디코드 길이가 다른 질문에 답한다는 것 · 렌더 거부가 안전 방향이라는 것 |
| `countingK8s` 계열 대역 doc 12자리 | 「호출 카운트가 무엇을 구별하지 못하는가」(8080 vs 6379 · `nil` 유예 vs `0` 유예 · `/healthz` vs `/exec`) — 대역이 필드를 기록하는 이유이고, 지우면 다음 정리가 필드를 지워 단언이 대역 안에서 무효가 된다 |
| `TestEveryStateChangingToolIsGatedOrDocumented` doc | 「새 쓰기 도구가 둘 중 하나를 정하지 않고 추가될 때 실패하는 테스트」 — 서 있는 불변식 |
| `TestGatedReadWithNoDeclaredTargetIsDescribedFromItsArguments` 10줄 | 보호하는 모양(민감 종류 컬렉션 watch)과 「문서가 정의하지 않은 프리컨디션으로 거부하면 AC1 이 금지한 분류기 발명」 — 문서에 없는 경계 |

## 덤으로 고친 것 — `Validate` 의 doc 이 `ToolNames` 에 붙어 있었다 (0줄 델타)

판정 중 실측으로 드러났다. `// Validate reports …` 3줄과 `// ToolNames is …` 4줄 사이에 빈 줄이
없어 Go 가 **7줄 전부를 `ToolNames` 의 doc 으로** 붙이고, `func Validate()` 는 doc 이 **없었다**.

```
$ go doc ./internal/mcp Validate      # 이전: 본문 없음
$ go doc ./internal/mcp ToolNames     # 이전: "Validate reports whether …" 로 시작
```

원장이 기록한 `ToolNames` doc 추가(`rct_20260921-0008` · PR #177 · `gate.go` +4)가 기존 doc **위가 아니라
사이에** 들어가며 생긴 것이다. 판정 절차 2 는 exported 식별자에 **이름으로 시작하는** 1줄 doc 을
요구하므로 이 상태는 그 규칙이 실제로는 서 있지 않았다는 뜻이다. 3줄을 `func Validate()` 바로
위로 **옮겼다** — 제거도 추가도 아니라 자리 이동이라 줄 수 델타는 0 이고, 위 35줄에 포함되지 않는다.

## 무접촉 증명

- **비주석 diff 0줄** — `git diff -U0` 의 `+`/`-` 중 `//` 로 시작하지 않는 줄이 0.
- `gofmt -l internal/mcp/` 공백 · `go vet ./internal/mcp/` 무출력.
- `go test ./...` **13패키지 전건 ok**(base 와 동일).
- 게이트 재실행: L 행 `416`/`fcb288a609d3` → `381`/`9515ea3f1de0`, 판정 축 `③④` → `①②③④`.

## 인계

1. 🔴 **주석↔주석 사본의 축이 닫혀 있다.** 이 행에서만 두 자리를 관측했다(`gateTargetKind` ↔
   `genericPairs` doc · `toolDeclaration.resolve` 마지막 문장 ↔ `genericPairs` doc). `README.md`
   의 제거 유형 ③ 은 이 모양을 제거 대상으로 예시하는데 **복원 경로 넷에는 그 자리가 없어**
   원장 `판정 축` 에 적을 수 없다. README 개정이 선결이고, 그 전까지는 유지가 맞다.
2. 잔여 ①② 는 이 행을 닫은 뒤 **69행 / 2,948줄**(L 45행 1,742줄 · D 24행 1,206줄).
   다음 단일 최대는 `internal/k8s/resource.go`+`resource_test.go` **177줄**이고, 그 뒤로
   `internal/mcp/mcp.go`+`toolslist.go` 126 · `internal/gatekeeper/gatekeeper.go`+테스트 125 ·
   `internal/k8s/proxy.go` 외 3파일 124 가 이어진다. `internal/k8s/` 는 자매 컴포넌트 8개가
   같은 패키지를 덮으므로(497줄) 한 슬라이스로 묶을 후보다 — 공유 파일은 없어 union-find 로는
   갈리지만 ① 원본이 패키지를 가로지른다.
