# 슬라이스 52 — 리소스 도구 표면 7행 18파일, ①② 축 전건 판정

`rct_20260926-0022` · base `0744553` · 대상 원장 행 일곱(줄 주석 표면). 제거 **72줄**
(`internal/k8s/resource.go` 15 · `resource_test.go` 19 · `internal/mcp/mcp.go` 23 ·
`internal/mcp/delete_collection.go` 10 · `delete_collection_test.go` 5). 코드 **0줄**.
L 판정 완료 39행 1,873줄(51.8%) → **46행 2,197줄(62.0%)**.

## 왜 이 일곱 행인가 — 직전 패스의 1순위를 예산 안에서 묶었다

슬라이스 51 의 인계 2 가 다음 단일 최대를 `internal/k8s/resource.go`+`resource_test.go`
**177줄**로 지목하고, 이어 「`internal/k8s/` 는 자매 컴포넌트가 같은 패키지를 덮으므로 한
슬라이스로 묶을 후보 — 공유 파일은 없어 union-find 로는 갈리지만 **① 원본이 패키지를
가로지른다**」고 적었다.

그 인계를 받되 묶는 축을 **패키지가 아니라 `① 원본이 실제로 건너가는 방향`** 으로 다시 재서
잡았다. 이 행들의 주석이 서로를 가리키는 자리는 `internal/k8s` ↔ `internal/mcp` 다 —
`resource.go` 의 `UpdateRef`·`ScaleSubresource`·`PatchTypeNames` doc 이 셋 다 「the tool
layer refuses …」로 **`internal/mcp` 를 ① 원본으로 든다.** 그래서 도구 계층
(`mcp.go`·`toolslist.go`)과, 두 패키지에 걸친 세 행(`delete_collection`·`create`·
`precondition` 계열)을 함께 가져왔다.

**크기**: 396줄 — 예산 400줄 안에서 가능한 최대다. 잔여 컴포넌트 중 가장 작은 것이 10줄
(`tests/integration/smoke.py` D 행)이라 무엇을 하나 더 얹어도 406줄로 넘친다.

| 행 | 범위 | 판정 전 | 판정 후 |
| --- | --- | ---: | ---: |
| 1 | `internal/k8s/resource.go` · `resource_test.go` | 177 | 143 |
| 2 | `internal/mcp/mcp.go` · `toolslist.go` | 126 | 103 |
| 3 | `internal/k8s/delete_collection.go` 외 2파일 | 85 | 70 |
| 4 | `internal/k8s/collection_precondition.go` 외 1 | 6 | 6 |
| 5 | `internal/k8s/create.go` 외 4 | 2 | 2 |
| 6 | `internal/k8s/patch_precondition.go` 외 1 | 0 | 0 |
| 7 | `internal/k8s/update_precondition_test.go` 외 1 | 0 | 0 |

## 판정 규칙 — 슬라이스 51 의 것을 그대로 썼다

한 명제는 **① 코드**(같은 파일의 시그니처·타입·상수·맵 리터럴·바로 아래 줄, 그리고 교차
패키지의 실제 호출부) 또는 **② 저장소 문서**(`docs/prd-*.md`·`docs/test-*.md`, 절로 인용
가능할 것)에서 축자 또는 그에 준하게 복원될 때만 제거한다. 「그 AC 가 왜 이 형태로만 관측
가능한가」·서 있는 불변식·실패 모드·픽스처와 실환경이 갈리는 지점·상류의 문서화되지 않은
거동은 유지한다(`README.md` 유지 대상 3).

🔴 **주석→주석 사본은 제거 근거로 쓰지 않았다**(슬라이스 51 인계 1 그대로 — README 개정이
선결이다). 이 슬라이스에서 그 모양을 **세 자리** 관측했고 셋 다 유지로 닫았다:
`resource_test.go` 의 AC8 404 논거 ↔ `resource.go::requireSubresource` ·
`mcp.go:390` 의 「핸들러는 게이트를 잊을 수 있다」 ↔ 같은 파일 `toolsCall` doc ·
`delete_collection_test.go` 의 namespace 논거 ↔ `parseDeleteCollectionTarget` doc.

🔴 **부재를 말하는 주석은 유지가 기본이다.** 다만 **그 부재를 PRD 가 스스로 적은 자리**는
예외이고, 이 패스에서 딱 하나 걸렸다 — `DeleteRef` 의 「셀렉터 필드가 없다」 3줄은
`prd-resource-generic` AC10 이 「셀렉터로 여러 개를 지우는 것은 … `resource_delete_collection`
(AC11)의 일이다 — verb 가 다르므로 도구도 다르다」와 검증 방법 「`name` 없이 셀렉터만으로
호출하는 경로가 이 도구에 **존재하지 않는다**」로 **결론과 이유를 함께** 적는다. 나머지
부재 주석 넷(`WatchQuery` 의 paging 없음 · `WatchResult.Truncated` · `scaleObject` 의
resourceVersion 없음 · `DeleteResource` 의 Preconditions 없음)은 PRD 에 대응 절이 없어 유지다.

## 제거 — 자리와 복원처

### 행 1 · `internal/k8s/resource.go` 15줄

| 자리 | 줄 | 복원처 |
| --- | ---: | --- |
| `WatchDefaultSeconds` 본문 | 2 | ② AC3 「**초과 값은 클램프하지 않고 거부한다** — AC5 가 `tailLines` 에 대해 정한 것과 같은 처리」 — 크로스 AC 대응까지 PRD 가 스스로 그었다 |
| `patchTypes` 첫 문장 | 1 | ① 바로 아래 맵 리터럴 넷 · ② AC9 「`patchType` 으로 `merge`·`strategic`·`json`·`apply` 를 받는다」 |
| `DeleteRef` 셀렉터 부재 | 3 | ② AC10 본문 + 검증 방법(위 참조) |
| `resolve` 첫 절 | 1 | ② AC20 「클러스터에 없는 종류는 추측해 호출하지 않고 거부한다. 거부 메시지에는 비슷한 이름의 실존 종류를 함께 제시한다」 축자 |
| Table 아닌 본문 판별 | 3 | ② AC2 ¶2 「파라미터가 어긋나면 apiserver 는 **오류로 답하지 않는다** … **모든 좌표에 빈 표**로 나타난다. 그래서 본문이 Table 이 아니면 **거부한다**」 |
| `requireSubresource` AC8·AC20 절 | 3 | ② AC8 「사유가 권한이나 존재 여부가 아니라 **그 종류에 레플리카가 없다는 사실**임을 밝힌다」 · AC20 「리소스 권한을 행사하지 않으므로 쌍 표에 없다」 |
| `apiCallError` | 1 | ② AC18 「403 을 주면 그대로 흘리지 않고, 권한 밖임을 밝히는 에러로 변환한다」 축자 |
| `stripNoise` | 1 | ① 바로 아래 두 필드 삭제 · ② AC4 가 두 필드를 이름으로 든다 |

### 행 1 · `resource_test.go` 19줄

전부 **테스트 doc 의 AC 재진술**이다(`README.md` 제거 대상 ②가 「테스트 파일 상단의 AC 목록,
테스트 이름을 산문으로 옮긴 doc 주석」을 명시적으로 든다). 「왜 이 어서션 형태인가」를 담은
절은 한 줄도 건드리지 않았다 — 지운 것은 그 앞에 붙은 AC 문장뿐이다.

| 테스트/헬퍼 | 줄 | 복원처 |
| --- | ---: | --- |
| `TestStripNoiseRemovesOnlyTheNoise` | 1 | ② AC4 + 검증 방법 「나머지 `spec`/`status` 는 온전하다」 |
| `TestForbiddenBecomesAGrantStatement` | 2 | ② AC18 본문 + 검증 방법 「재시도를 유도하지 않는 명시적 에러」 |
| `tableNegotiatingServer` 폴백 절 | 3 | ② AC2 ¶2(폴백 `application/json` → 빈 표) |
| `TestListResourcesGetsTheTableFrom…` | 5 | ② AC2 ¶2 **전문 축자** — 「손으로 적지 않는다」·「`v=1`(어떤 apiserver 도 서빙하지 않는 값)을 들고 머지 직전까지」·「모양이 아니라 **값으로** 단언」·「자기가 만든 Table 을 먹이는 테스트는 이 계열의 어긋남을 원리상 볼 수 없다」 |
| `TestListResourcesRefusesABodyThatIsNotATable` | 3 | ② 같은 절(빈 표로 디코드 → 거부한다) |
| `TestUnknownKindSuggestsCandidates` | 1 | ② AC20 「후보를 함께 제시한다」 |
| `TestPatchTypesMapToApiserverMediaTypes` | 2 | ② AC9 네 이름 + AC2 의 「값으로 단언」 원칙 |
| `discoveryServer` | 1 | ② AC8 DaemonSet 규칙 |
| `TestUpdateScaleRejectsReplicalessKind` | 1 | ② AC8 축자 |

### 행 2 · `internal/mcp/mcp.go` 23줄

`toolslist.go` 2줄은 **전건 유지**다(리터럴로 두는 이유는 ①②에 없다).

| 자리 | 줄 | 복원처 |
| --- | ---: | --- |
| `defaultSensitiveKind` 둘째 문장 | 1 | ② `prd-resource-generic` AC16 「민감 종류는 읽기도 쓰기도 승인을 거친다」 |
| `gate` 필드 이유절 | 1 | ② `prd-approval-gate` AC5 「미설정 … 쿠버네티스 API 를 호출하지 않고 에러를 반환한다」 |
| `gateReader` 분리 이유 | 1 | ② AC11 「이것들은 **게이트가 행사하는 쿠버네티스 권한**이며 도구의 것이 아니다」 |
| `gateCollectionReader` | 2 | ② AC11 표(`list` on ⟨kind⟩) + 「도구의 `Service` 나 단일 객체 `TargetReader` 에 합치지 않는다」 축자 |
| `WithGate` 둘째 문장 | 1 | ② AC5 |
| `afterVerdict` 첫 문장 후반 | 1 | ② AC7 「승인 하나는 단 한 번의 …」 · AC6 「실행 직전에 … 다르면 실행하지 않고 거부」 |
| `gateOf` 괄호 | 1 | ② `prd-event-log` AC2 「gatekeeper `request_id` 와 판정을 함께 싣는다 … 자동 승인은 필드로 표기된다」 |
| `gateWait` 마지막 절 | 1 | ② `prd-metrics` AC3 「사람의 승인을 기다린 시간이 도구 지연에 섞이면 분포가 사람의 응답 습관을 재는 것이 된다」 축자 |
| 배치 승인 주석 | 2 | ② `prd-resource-generic` AC7 · `prd-approval-gate` AC3 `create` 항목 「문서마다 승인 요청이 하나씩」 |
| `recordTarget` 후반 | 1 | ② `prd-event-log` AC1(대상 네 필드) · AC3 ⑶⑷(페이로드·본문 일반은 싣지 않는다) |
| `extractArguments` 둘째 문장 | 1 | ① 바로 아래 `var p struct` 와 반환 |
| `sessionList` | 1 | ② `prd-session-list` AC1 「세션이 없으면 빈 목록을 반환하고 에러로 취급하지 않는다」 축자 |
| `sessionRead` | 4 | ② `prd-session-read` AC2 「도구는 어느 분기로 처리됐는지(`path`)와 처리 후 세션 상태를 결과에 포함해, **읽기 한 번이 스냅샷 세션의 파드를 되살렸다는 사실**이 …」 **전문 축자** |
| `offset` | 1 | ② AC1 「`offset=0` 또는 미지정은 전체 출력을 반환하고」 · ① 바로 아래 기본값 |
| `sessionWrite` 후반 | 4 | ② `prd-session-write` 「않고 반환하며, 산출물은 `session_read` 의 누적 출력으로 관측된다」 · AC2 「`snapshot` 은 거부하지 않고 복원 후 적용한다」 |

### 행 3 · `delete_collection` 15줄

🔴 **`internal/k8s/delete_collection.go` 는 25줄 전건 유지다.** 세 블록 모두 ①②에 서지
않는다 — 「apiserver 가 finalizer·유예 전에 답하므로 삭제 수는 이 서버가 관측하지 않은
숫자다」는 상류 거동이고, 「Namespace 가 값 타입이라야 최광역 삭제가 표현 불가능하다」와
「셀렉터를 좁히거나 정규화하지 않는다」는 다음 편집을 구속하는 서 있는 불변식이다.

| 자리 | 줄 | 복원처 |
| --- | ---: | --- |
| `contextNameLimit` 첫 문장 | 1 | ② AC3 `deletecollection` 행 「많으면 앞 20개와 총 개수」 · ① 상수값 |
| `deleteCollectionSelection` | 2 | ① 바로 아래 세 필드 · ② AC11 「`labelSelector`·`fieldSelector` … `namespace` 는 필수」 |
| `parseDeleteCollectionTarget` 첫 문장 | 2 | ② AC11 + ① 함수 본문의 거절 목록 |
| `callDeleteCollection` | 1 | ① 이름과 본문(`entry.decl.gatedPairs`) |
| AC3 종결절 | 2 | ② AC3 「대상 좌표를 해석할 수 없거나 상세를 만들 수 없으면 승인 요청을 만들지 않고 거부한다」 축자 |
| `confirmCollectionUnchanged` 첫 문장 후반 | 1 | ② AC6 |
| `collectionApprovalContext` 둘째 절 | 1 | ② AC3 `deletecollection` 행 |
| 테스트 doc 머리 5자리 | 5 | ② AC11 0건 절·AC3 행·AC3 20개 상한·AC11 검증 방법·AC11 namespace — **네 자리는 PRD 문장을 따옴표로 인용**하고 있었다 |

### 행 4~7 — 제거 0줄, 유지로 닫는다

- **행 4**(6줄)·**행 5**(2줄): 전부 exported 식별자의 **이름으로 시작하는 1줄 doc** 이다.
  판정 절차는 2(관례가 요구하는 최소 doc)에서 멈추고 3(복원 경로)까지 가지 않는다 — ① 히트가
  곧 제거 근거가 되는 부류가 아니라, ① 히트인 것이 **관례의 정의** 인 자리다.
- **행 6·7**(0줄): 판정 대상이 0줄이라 공허 판정으로 축을 닫는다.

## ③④ 재앵커 — 지문이 움직인 세 행

R11 은 축을 **그 행의 현재 지문에 묶인 근거**로만 인정한다. 제거가 지문을 움직이므로 세 행의
기존 ③④ 앵커(옛 지문)가 낡았고, 같은 PR 에서 다시 못박았다. 재앵커의 근거는 둘이다.

1. **이 패스는 줄을 걷고 남은 줄을 다시 감쌌을 뿐 새 명제를 한 줄도 들이지 않았다.** 살아남은
   문장은 전부 직전 지문에서 ③④ 로 물어 닫힌 것이고, 걷힌 줄은 ①② 로 닫혔다 — ③ 히트가 새로
   생길 자리가 없다.
2. **④ 는 이 head 에서 재실측했다.** 최근 120커밋을 `/commits/<sha>/pulls` 로 전수 조회해
   **연결 PR 이 없는 커밋 0건**을 얻었다(「제목에 `(#N)` 이 없다 = 직접 push」는 틀린 대리
   지표라 쓰지 않았다). 따라서 정책 범위 파일을 건드린 main 직접 push 도 0건이고 ④ 는 적용
   대상 부재다. 이 값은 직전 패스들의 「23건 전부 `chore(deploy):`」보다 더 강해졌다 — #133
   이후 이미지 핀이 `deploy` 브랜치로 가므로 main 에서 사라지는 중이다.

## 무접촉 증명

- **비주석 diff 0줄** — `git diff -U0` 의 `+`/`-` 중 주석 줄 ERE 에 맞지 않는 줄이 **0**
  (공백 줄 제외). 삽입 62 · 삭제 134 전량이 주석이다.
- `gofmt -l internal/` 공백 · `go vet ./internal/...` 무출력.
- `go test ./...` 전건 ok.
- 게이트 재실행: 세 행이 `177→143`/`9680cbbd07f2` · `126→103`/`be17ff0e9a72` ·
  `85→70`/`298eb8b08327`, 일곱 행 모두 판정 축 `③④` → `①②③④`.

## 인계

1. 🔴 **주석↔주석 사본의 축은 여전히 닫혀 있다.** 이 슬라이스에서 세 자리를 더 관측했다(위
   「판정 규칙」). 슬라이스 51 이 관측한 두 자리와 합쳐 **다섯 자리**이고, `README.md` 의 제거
   유형 ③ 이 이 모양을 제거 대상으로 예시하는데 복원 경로 넷에 그 자리가 없다. README 개정이
   선결이고, 다섯 자리가 모인 지금이 그 개정을 한 슬라이스로 세울 만한 크기다.
2. 잔여 ①② 는 **62행 / 2,552줄**(L 38행 1,346줄 · D 24행 1,206줄). 다음 단일 최대는
   `tests/integration/aws_config_get_ac1.py` 가족 **239줄**(L 29 + D 210, 17파일)이고,
   그 뒤로 `_gatekeeper.py` 가족 156 · `internal/gatekeeper/gatekeeper.go` 125 ·
   `internal/k8s/proxy.go` 외 3파일 124 · `run_all.py` 115 가 이어진다. **D 표면이 37.9% 로
   L 62.0% 에 크게 뒤처지므로**, 위 둘(395줄, 예산 안)을 함께 가져가면 D 가 한 번에
   37.9% → 56.0% 로 움직인다.
