# 미등재 창의 첫 등재 — 슬라이스 39: 시나리오 5 e2e 신설 4파일 (양 표면)

- **task**: `rct_20260926-0003` (`tbm_homelab-k3s-mcp-comment-redundancy`)
- **기준 커밋**: `3636aaf` (자매 PR #225 착지 tip)
- **대상**: 자매 렌즈 `rct_20260926-0001`(PR #226, `fea3747`)이 신설하고 **판정하지 않은** 4파일 —
  `tests/k8s/kind/gatekeeper-injector.yaml` · `tests/k8s/kind/gate-broken-variant.yaml` ·
  `tests/integration/approval_gate_ac5.py` · `tests/integration/_injector.py`.
  줄 주석 **88줄** · docstring **83줄**, 합 **171줄**, **원장 행 0개**
- **결과**: **제거 5줄 · 추가 3줄**(추가는 지시 대상 복구 reflow — 새 주장 0줄).
  줄 주석 원장 **2행 신설**(등재 67 → **69**) · docstring 원장 **1행 신설**(32 → **33**).
  `판정-합계` 3300 → **3386**(100.0%) · `판정-잔량` 88 → **0** ·
  `docstring-합계` 1884 → **1967**(100.0%) · `docstring-잔량` 83 → **0**.
  복원 경로 ①② 판정 6/67 → **8/69**(잔량 **61 불변**) · docstring ①② 3/32 → **4/33**(잔량 **29 불변**)

## 왜 이 표면인가 — 「행이 없다」는 것 자체가 산출이다

이 모델에서 `미판정 잔량`이 0 이 아닌 것은 **이번이 처음**이다(직전 지점 `ed0ff06` 는 두 표면 모두
100.0% / 잔량 0). 자매 렌즈는 자기 경계(「산출물은 E2E 와 등재 문서뿐」) 안에 있었고 내 축의 판정을
**회피하지 않고 원장에 선언해 넘겼다** — 이 모델의 to-be 가 정책 디렉터리 *전체* tree 인 이유가
바로 그 인계를 재감지로 깨우는 것이다. 절차 위반은 없다.

문제는 **그 표면에 게이트가 없다는 것**이다. R4/R8 은 `판정 완료 + 잔량 == 실측 전체` 라는
**항등식**이라, rc=0 이 뜻하는 것은 「171줄이 미판정이라는 선언이 정확하다」뿐이다. 감지 단계가
변이 탐침으로 못박았듯 `e2e-mocking-policy.md` 의 C3·C4 를 **축자 복사한 주석**을 심고 잔량만 맞추면
게이트는 **여전히 rc=0** 이다 — 초록이 커버리지를 부정하고 공백을 서명한다. ①②/③④ 잔량 카운터도
**행 단위**라 미등재 줄을 원리적으로 세지 않는다.

그래서 이 슬라이스는 제거량이 작다(5줄). 산출은 제거가 아니라 **행 세우기**다 — 행이 서면
그 171줄이 비로소 R2(재측)·R9/R11(재앵커)의 하중을 받는다.

## 제거 5줄

### ⑴ `gate-broken-variant.yaml` — 복원 경로 ① (3중 복원)

머리말의 두 불릿을 걷었다:

```
#  * no-base-url  -- GATEKEEPER_API_KEY set, GATEKEEPER_BASE_URL absent.
#  * no-api-key   -- GATEKEEPER_BASE_URL set, GATEKEEPER_API_KEY absent.
```

같은 사실이 **세 경로로** 복원된다:

| 경로 | 자리 | 무엇이 복원되나 |
| --- | --- | --- |
| ㈀ 같은 파일의 선언 | `no-base-url` 변형은 `GATEKEEPER_API_KEY` 만, `no-api-key` 변형은 `GATEKEEPER_BASE_URL` 만 env 에 둔다 | 「어느 쪽이 빠졌나」 |
| ㈁ 같은 파일의 인라인 주석 | 각 변형의 env 바로 위 줄이 같은 사실을 또 적는다 | 같음 |
| ㈂ 제품 코드 | `internal/gatekeeper/gatekeeper.go` `FromEnv()` 의 두 에러 문자열 축자 | 문면 그대로 |

등록 메모가 이름으로 적은 중복 유형 ③(픽스처 YAML 주석이 바로 아래 선언을 되풀이)에 정확히 걸린다.

> **지시 대상 복구 — 이 제거가 문단 하나를 낡게 만든다.** 이어지는 문단이
> 「**The last two** are the halves of `gatekeeper.FromEnv()`'s two error branches」로 시작해
> 걷은 두 불릿을 가리킨다. 불릿만 지우면 지시어가 공중에 뜨므로, 그 지시어를
> **두 변형 이름**으로 바꿨다(`The no-base-url and no-api-key variants`). 이름은 같은 파일의
> Deployment·Service 명(`homelab-k3s-mcp-gate-no-base-url` 등)이고 `approval_gate_ac5.py` 의
> `TARGET_NO_BASE_URL`·`TARGET_NO_API_KEY` 가 그 이름으로 닿으므로 **식별자 참조이지
> env 사실의 재진술이 아니다**. 문단 폭을 맞추느라 6줄 → 7줄이 되어 순 −1 줄이다.

### ⑵ `gatekeeper-injector.yaml` — 복원 경로 ②(+③)

「WHY A SEPARATE DEPLOYMENT」 블록에서 C3 **내용을 되풀이하는 절만** 걷었다:

- **걷은 것**: `C3 says the product's code, image, build flags and deployment env do not change`
  ↔ `docs/e2e-mocking-policy.md` 「**C3 제품 무변경**: 제품 코드·이미지·빌드 플래그·배포 env 를
  바꾸지 않는다」의 축자.
- **남긴 것**: 「가장 작은 방법은 `gatekeeper-fixture.yaml` 을 한 줄도 건드리지 않는 것」이라는
  **설계 판단**과 C3 참조 자체.

② 의 최상급 형태다 — **같은 주석 블록 8~10행이 그 문서를 이름째 자기 원본으로 지목한 뒤**
(「the whole basis of the verdict in `docs/e2e-mocking-policy.md` 「실환경 주입 판정 …」, whose
conditions C1~C5 this file … must both stay inside」) 그 C3 의 내용을 되풀이한다. 주석이 스스로
원본을 가리켰으므로 되풀이는 잉여다.

## 유지 — 그리고 ③ 히트를 「무히트」로 적지 않은 이유

🔴 **PR #226 본문에 복원 경로 ③ 히트가 실재한다.** 그 본문의 「주입기를 사이드카가 아니라 별도
Deployment 로 세운 이유」 절은 ⑴ 위 C3 판단과 ⑵ **단일 노드 kind · ReadWriteOnce PVC 동일노드
마운트 · uid·fsGroup 1001** 을 실제로 옮겨 적는다. ⑴ 은 위에서 걷었다. ⑵ 는 걷지 않았고,
그 근거는 정책이 ③ 에 달아 둔 단서 — **서사는 닫고 가드는 남긴다** — 이다.

| 자리 | ③ | 판정 | 근거 |
| --- | --- | --- | --- |
| uid 1001 · fsGroup 1001 | 히트 | **유지** | 「픽스처의 `securityContext` 와 **같아야 한다**」는 결합. 원장 `#29`·`#30`·`#32` 의 `CLIENT_ID`·`MCP_API_KEYS`·`GRAFANA_ISSUER_TOKEN` 줄과 같은 형이고, 그 선례가 전건 유지로 닫혔다 |
| 단일 노드 kind · RWO PVC 동일노드 | 히트 | **유지** | 클러스터가 다중 노드가 되는 날 **조용히 깨지는** 서 있는 불변식. 「순서를 바꾸면 무엇이 조용히 깨지는가」에 해당한다 |
| SQLite `busy_timeout=5000` 롤백 저널 | 무히트 | **유지** | 상류 `gatekeeper`(sha-762fafe)의 **문서화되지 않은 동작**. 저장소 문서로 복원되지 않는다 |
| discard 포트 결정성 | 무히트 | **유지** | 「Service with no endpoints 도 거부하지만 그건 지켜야 할 객체가 하나 더 늘어난다」는 기각 논거 — 복원 불가 |
| `approval_gate_ac5.py` 머리 관행 | — | **승계** | 「시나리오 제목 축자 + `검증 시나리오:` + `실행 대상:`」 은 형제 8파일이 전건 같은 모양이고 기계 판독 선언은 게이트가 이미 제외한다. 이탈이 아니라 승계이며 선례 행 다수 보유 |
| Python 줄 주석 16줄 | 무히트 | **유지** | 전부 상수 위 `#:` 형이고 매니페스트의 Deployment 명·포트와 **같아야 한다**는 결합 |

「애매하면 남긴다」가 적용된 자리가 다수이고, 그것이 이 슬라이스의 제거량이 5줄인 이유다.

## 검증 (전부 로컬 · 클러스터 불필요)

```
python3 tests/integration/check_ac_mapping.py     # rc=0 (무이동)
python3 scripts/check_mock_policy.py              # rc=0 (무이동)
python3 scripts/check_comment_policy.py           # rc=0
python3 scripts/check_doc_inventory.py            # rc=0
```

- 줄 주석 `3386줄/150파일 · 100.0% · 등재 69 · 잔량 0 · ③④ 69/69 · ①② 8/69(잔량 61)`
- docstring `1967줄/111파일 · 100.0% · 등재 33 · 잔량 0 · ③④ 33/33 · ①② 4/33(잔량 29)`
- 문서 `96 / 96 · 정책 56 · 끊긴 링크 0`
- **비주석 diff 0줄** — `.go` 접촉 없고 YAML 편집은 주석 줄뿐이라 `kubectl apply` 결과가 바이트 동일

**음성 프로브 4종**(행이 공전하지 않음을 확인 — 프로브 뒤 전부 원상 복구):

| 프로브 | 기대 | 실측 |
| --- | --- | --- |
| `판정-잔량` 0 → 1 | R4 FAIL | rc=1 (FAIL 1건) |
| 새 행의 ①② 앵커 제거 | R11 FAIL | rc=1 (FAIL 2건) |
| 새 행 지문 1글자 변조 | R2 FAIL | rc=1 (FAIL 5건) |
| ⑴ 제거 되돌림 | 줄 수·지문 FAIL | rc=1 (FAIL 3건, 합계 3387) |

## 범위 밖 (후속)

- **등재 행의 복원 경로 ①② 잔여** — 줄 주석 **61행** · docstring **29행**(잔량 마커 불변이 그 증거).
  계획 시점 `rct_20260926-0002` 가 그 축의 슬라이스 38(원장 `#8`·`#10`)을 집고 있었다.
- **원장 행 `#61`**(eventlog) — 같은 축의 별건.
- **유지한 ③ 히트 둘의 만료 조건** — ⑴ `ci.yml` 이 kind 를 다중 노드로 바꾸면 단일노드 전제는
  주석이 아니라 코드가 거짓이 되는 자리다. ⑵ 픽스처 `securityContext` 의 uid 가 1001 에서
  바뀌면 그 결합 주석이 먼저 거짓이 된다. 둘 중 하나가 일어나면 이 행은 **재판정 대상**이다.
- **e2e 실행** — kind 가 필요해 CI 가 판정한다. 비주석 diff 0줄이라 무영향이다.
