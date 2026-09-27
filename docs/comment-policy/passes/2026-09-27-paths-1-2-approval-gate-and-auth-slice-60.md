# 복원 경로 ①② 판정 — 승인 게이트·인증 축 (슬라이스 60)

- 판정일: 2026-09-27
- 판정 근거: `rct_20260927-0004` (`tbm_homelab-k3s-mcp-comment-redundancy`)
- 범위: 판정 축이 `③④` 였던 6행 **400줄** — L 5행 327줄(`internal/gatekeeper` 125 ·
  `internal/auth` 79 · `gatekeeper-fixture.yaml`+variant 57 · `auth-fixture.yaml`+`test-deployment.yaml` 47 ·
  `approval_gate_ac6.py` 가족 19) + D 1행 73줄(`approval_gate_ac6.py` 가족)
- 결과: **제거 120줄**(L 91 · D 29) · **개작 15줄** · 비주석 diff **0줄** · 코드 0줄

## 왜 이 여섯 행인가

파일 공유로 묶은 잔여 13컴포넌트(825줄) 중 **승인 게이트 도메인**(`internal/gatekeeper` 125 +
`gatekeeper-fixture.yaml` 가족 57 + `approval_gate_ac6.py` 가족 92)과 **인증 도메인**
(`internal/auth` 79 + `auth-fixture.yaml` 가족 47)을 합쳐 **정확히 400줄**로 예산을 채웠다.
두 도메인은 ② 소유자가 각각 `docs/prd-approval-gate.md`·`docs/test-approval-gate.md` 와
`docs/prd-platform-auth-safety.md`·`docs/test-platform-auth-safety.md` 로 갈려 있고, 그 두 쌍을
한 번 읽으면 여섯 행이 모두 판정된다 — 감지가 1순위로 든 묶음
(`internal/gatekeeper` + `internal/server` + `approval_gate_ac6` + `oidc-fixture.yaml` = 398)은
② 소유자가 셋으로 갈려 같은 예산에 문서를 하나 더 읽어야 했고 2줄이 남았다.

## 이 축의 판정이 남아 있던 이유 — 「일부 축만」의 실제 모양

여섯 행 중 둘(`internal/auth`·`internal/gatekeeper`)은 2026-09-04·2026-09-13 에 ①② 를 이미
한 번 받았고, 그 뒤 **재판정이 줄을 더하며 축을 `—`→`③④` 로만 채웠다**(`internal/auth` +38줄 ·
`internal/gatekeeper` +57줄). 그 재판정 문면을 읽으면 ① 은 물었고("시그니처에서 복원되지
않는다", "코드는 고르는 법만 보인다") **② 는 두 자리에서만 물었다**
(`recordRefusal` — "어느 PRD 문면도 이 선택을 정하지 않는다" · `maxRecordedBody` — "1 MiB 의
근거는 본문에 없다"). 나머지 행은 ①② 를 한 번도 받지 않았다. 그래서 이 패스의 일은
**증분의 ② 를 절 단위로 묻는 것**이었고, 이미 ② 를 물어 유지된 두 자리는 그 근거가 된
`prd-event-log.md` 가 그 뒤 **바이트 부동**임을 확인해 승계했다.

## 🔴 「AC 포인터는 유일한 자리」 논거는 파일마다 참·거짓이 갈린다

2026-09-13 이 gatekeeper 행에 세운 유지 근거는 이것이었다 — "Go 단위 테스트에는 `검증 AC:`
같은 기계 판독 선언이 없어 그 포인터가 AC ↔ 테스트를 잇는 **유일한 자리**". 이 명제를 두 파일에
대고 다시 쟀다.

| 파일 | ② 가 Go 테스트 함수를 이름으로 드는가 | 판정 |
| --- | --- | --- |
| `internal/auth/auth_test.go` | **든다** — `test-platform-auth-safety.md` 시나리오 1·2·7·8 자동화 칸이 12건을 이름으로 열거하고, `test-event-log.md` 시나리오 6 이 `TestRequireBearerRecordsTheCallItRefuses` 를 든다 | 논거 **거짓** → AC 라벨 넷을 절 배너에서 걷었다(줄 수 불변) |
| `internal/gatekeeper/gatekeeper_test.go` | **들지 않는다** — `test-approval-gate.md` 가 드는 일곱 이름은 전부 "계획" 이고 실제 함수 12건과 **하나도 맞지 않는다**(`TestFailClosedPaths` vs `TestEveryFailurePathRefuses` 등) | 논거 **참** → AC 포인터를 남겼다 |

같은 문장을 유지 근거로 쓰는 행이 둘인데 한쪽에서만 만료됐다. 그래서 번복은 그 한 행에만
미치고, gatekeeper 행의 결과 칸은 그 논거를 그대로 들고 있다.

## 🔴 「같은 모양의 문단」이 어떤 파일에서는 유지고 어떤 파일에서는 제거인 판별식

다섯 e2e 모듈 docstring 은 모두 "Go 단위가 X 를 이미 단언한다 — 그 층이 못 보는 것이 이 파일의
이유다" 문단을 들고 있었다. 그런데 `resource_generic_ac17.py`·`resource_read_smoke.py` 는
**①②③④ 로 닫힌 채 같은 모양의 문단을 유지**하고 있다. 모양으로는 갈리지 않는다.

판별식은 **그 시나리오의 자동화 칸에 같은 문장이 있는가** 하나다. `test-resource-generic.md`
시나리오 6 은 세 Go 단위를 이름으로 열거하고 "**이 수준이 못 보는 것**은 … 그것이 통합 파일이
남아 있는 이유다" 를 축자로 담는다 → 제거. 시나리오 17 은 담지 않는다 → 유지. 다섯 파일 전건을
자기 시나리오에 대고 물었고 다섯 다 담고 있었다.

## 🔴 낡은 지목 다섯 — 픽스처 주석이 사라진 파일을 가리키고 있었다

`auth-fixture.yaml`·`test-deployment.yaml` 의 주석이 `docs/test-namespace-list.md` ·
`tests/integration/namespace_list_ac2.py` · `docs/test-workload-logs.md` · `workload_list` ·
`workload_scale` 를 가리켰다. **다섯 다 레포에 없다**(도구군이 `resource_*` 로 대체됐다). 더
나아가 "그 파일이 거부 문면을 축자로 단언한다" 도 거짓이다 — 그 문자열의 현재 소유자는
`tests/integration/_auth_variant.py::K8S_REFUSAL` 이고, **그 상수를 실제로 단언하는 케이스가
없다**(`grep -rn K8S_REFUSAL tests/` = 정의 1건). 가드 자체("이 줄을 지우면 깨진다")는 유효한
명제가 아니게 됐으므로 죽은 이름을 살아 있는 이름으로 바꾸고, 단언 부재는 범위 밖 후속으로
넘긴다. 정책이 정한 대로 어긋난 주석은 제거 근거를 강화한다.

## 정본 지정(제거 대상 ③) — 여섯 자리

| 명제 | 정본 | 걷은 사본 |
| --- | --- | --- |
| 구조상 유효한 JWT 를 API 키 전용 모드에서 거부하며 nil JWKS 를 건드리지 않는다 | `auth.go::verify` 안의 주석 | `auth_test.go` 케이스 머리말 2줄 |
| httptest OIDC 서버로 실 발급자 없이 discovery 를 끝낸다 | `auth_test.go::oidcServer` doc | 파일 머리말 2줄 |
| 세 판정(unreachable·unconfigured·판정 이전)의 경계 | `gatekeeper.go` `Verdict`·`Refusal` doc | 테스트 머리말 2줄 |
| id 를 재사용하면 한 승인이 둘째 호출을 대신한다 | `randomExternalID` doc | 케이스 머리말 1줄 |
| 판정은 폴링해야 보인다 | `fakeBackend` doc | 케이스 머리말 1줄 |
| `AUTO_REJECT` 는 담당자가 있는 배포에서만 발화한다 | `resource_generic_ac16.py::_refuse` docstring | `resource_generic_ac6.py` 인라인 5줄 → 포인터 1줄 |
| gatekeeper-variant 의 `GATEKEEPER_BASE_URL` 이 기록 프록시를 가리킨다 | `gatekeeper-variant.yaml` 의 그 절 | `gatekeeper-fixture.yaml` Service 머리말 1줄 |

정본 전부가 **이 슬라이스 안에** 있어 이 PR 뒤에도 산다. `_auth_variant.py` 의 "Kubernetes 는
부재만으로 끌 수 없다" 는 사본으로 보이지만 그쪽은 **K8S_REFUSAL 문면이 나머지와 다른 이유**를
세우는 다른 논증의 전제라 정책의 "양쪽이 각자 다른 논증의 전제로 쓰면 둘 다 남긴다" 에 해당해
건드리지 않았다(그 행은 ①②③④ 로 닫혀 있다).

## 무영향 — 네 축

1. **Go**: `gofmt -l` 빈 출력 · `go build ./...` · `go vet ./...` · `go test ./internal/...`
   12패키지 전건 `ok`. 주석 줄을 걷은 뒤 비주석 바이트가 `HEAD` 와 동일(빈 줄 정규화 뒤 일치).
2. **Python**: 다섯 파일의 **docstring 을 전부 걷은 AST 덤프가 `HEAD` 와 바이트 동일**.
3. **기계 판독 선언**: `run_all.py::_field()` 로 여섯 필드
   (`검증 시나리오`·`실행 대상`·`병렬 레인`·`추가 인자`·`실행 순서`·`검증 AC`)를 다섯 파일에서
   다시 읽어 `HEAD` 와 값이 같음을 확인했다.
4. **문자 부분 수열**(한글 포함 정규화 `[^0-9a-z가-힣]`): Go 네 파일과 py 세 파일은 남은 주석이
   원본의 **부분 수열**이다(= 순수 삭제, 새 문장 0건). 부분 수열이 아닌 **개작 15줄**은 위
   "낡은 지목" 정정 5줄 · 사본 포인터화 2줄 · 절 축약 4줄 · 절 배너 AC 라벨 제거 4줄이고, 어느
   것도 새 명제를 들이지 않는다.

## 게이트 넷 (로컬 실측, 전건 rc=0)

- `scripts/check_comment_policy.py` — R1~R15 위반 0
- `scripts/check_doc_inventory.py` — D1~D4 위반 0 (허브 119 → **120**, 정책 79 → **80**)
- `scripts/check_mock_policy.py` — rc=0 (`mock-exception:` 줄 무접촉)
- `tests/integration/check_ac_mapping.py` — 일곱 값 불변

## 수치

| 표면 | 이전 | 이후 |
| --- | --- | --- |
| L 줄머리 | 3337줄 / 161파일 · 완료 71행 2665줄(79.9%) · `③④` 13행 | **3246줄** / 161파일 · 완료 **76행 2901줄(89.4%)** · `③④` **8행** |
| D docstring | 1447줄 / 112파일 · 완료 30행 1294줄(89.4%) · `③④` 4행 | **1418줄** / 112파일 · 완료 **31행 1338줄(94.4%)** · `③④` **3행** |
| E 줄 끝 | 4줄 / 4파일 · 9행 100% | **무접촉** |

행별 새 지문: `internal/auth` `edb3e14df9a8`(50) · `internal/gatekeeper` `1051a4089181`(106) ·
`approval_gate_ac6` 가족 L `d57e46ef22c4`(15) · `auth-fixture.yaml` 가족 `f9bde77f2d56`(18) ·
`gatekeeper-fixture.yaml` 가족 `f2d3423205af`(47) · `approval_gate_ac6` 가족 D
`eda9546b0538`(44).

## 범위 밖 (후속)

- **잔여 ①② 8컴포넌트 425줄** — L 8행 345줄(`internal/server` 97 · `oidc-fixture.yaml` 84 ·
  `session-platform.yaml` 61 · `resource-generic-fixture.yaml` 가족 45 · `http-trace.yaml` 32 ·
  `minio.yaml` 가족 16 · `event_log_ac3` 3 · `check_doc_inventory.py` 7) + D 3행 80줄
  (`event_log_ac3` 40 · `check_doc_inventory.py` 24 · `resource_generic_ac1.py` 16).
  다음 덩어리는 `http-trace.yaml`(32)만 빼면 **393줄**로 예산에 들어가고, 그 뒤 32줄 한 행이
  남는다.
- **`_auth_variant.py::K8S_REFUSAL` 을 단언하는 케이스가 없다** — 상수만 있고 소비자가 0이다.
  주석이 아니라 테스트의 공백이므로 이 축이 아니라 e2e 축의 몫이다.
- **`workload-fixture-sts` 를 쓰는 케이스가 없다** — `_workload.py::STS_WORKLOAD` 의 소비자가
  0이다(`DS_WORKLOAD` 는 `resource_generic_ac8.py` 가 쓴다). 같은 성질의 공백이다.
- **`docs/test-approval-gate.md` 의 Go 단위 "계획" 이름 일곱이 실제 함수명과 하나도 맞지
  않는다** — 문서↔구현 정합 축(`tbm_homelab-k3s-mcp-docs-impl`)의 몫이다. 고쳐지면 이 레포에서
  "AC 포인터는 유일한 자리" 논거가 `gatekeeper_test.go` 에서도 만료되므로, 그때 그 행을 다시
  집어야 한다.
