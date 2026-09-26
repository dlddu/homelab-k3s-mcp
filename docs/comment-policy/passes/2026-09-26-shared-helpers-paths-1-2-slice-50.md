# 슬라이스 50 — 공유 헬퍼 가족 7파일, 두 표면의 ①② 축 전건 재판정

`rct_20260926-0020` · base `5605451` · 대상 원장 행 둘(줄 주석 `tests/integration/_helpers.py` 외 6 ·
docstring 같은 7파일). 제거 **19줄**(L 4 · D 15). 코드 **0줄**.

## 왜 이 두 행이 한 슬라이스인가

두 행의 **범위 파일 집합이 동일하다**(실측 7/7). 따로 착지시키면 뒤에 오는 패스가 앞 패스의 ① 근거를
지운다 — 이 가족에서 ① 원본은 거의 전부 **같은 파일의 모듈 docstring 또는 상수 doc** 이고, 그 둘이
서로 다른 표면에 등재돼 있기 때문이다(모듈 docstring = D 행 · 상수 `#` doc = L 행). 실제로 이 패스의
제거 근거 다섯 중 셋이 표면을 가로지른다: `_opensearch.py` 의 `RUN_ID` **줄 주석**(L)을 걷은 근거가
같은 파일 **모듈 docstring**(D)이고, `_helpers.py` 의 사용 지점 **줄 주석**(L)을 걷은 근거가
`BASE_ACCESS_KEY_ID` 의 **줄 주석**(L)이지만 그 판단은 `assert_assumed_role_access` **docstring**(D)이
같은 명제를 반증 조건으로 들고 있다는 사실에 의존한다.

## 감지의 1순위를 받지 않은 이유

감지는 크기순으로 `internal/mcp/gate.go`+`gate_test.go`(L 416줄, 최대 단일 행)를 1순위로 넘겼다.
판별식을 후보에 직접 돌려 갈랐다.

| 축 | gate 행 | 이 가족 |
| --- | --- | --- |
| 크기 대 직전 패스가 적은 예산(400줄) | 416 — 단일 행이라 쪼갤 수 없이 **초과** | 315 — 예산 안 |
| 닫힌 행에 대한 하중 | 없음 | **있다** — 슬라이스 34 의 75줄 제거가 이 가족의 함수 doc 여섯을 ① 원본으로 이름을 들었다 |
| 읽는 면 | 2,393줄 | 1,262줄 |

## 무엇을 다시 물었는가 — 두 선행 ①② 판정이 지문에 묶이지 않은 전건 단정이었다

| 행 | 선행 ①② | 그 뒤 지문 이동 | 그래서 |
| --- | --- | --- | --- |
| L | 2026-09-08(그 전 1차는 「제거 0줄 — 전부 「왜 이 형태인가」다」) | 슬라이스 20 이 132 -> 111 | ③④ 만 재앵커 · ①② 는 안 물었다 |
| D | 2026-09-06(「이름으로 든 여덟 자리 중 **어느 것도 복원 경로 넷에 없다**」) | 슬라이스 28 이 220 -> 204 | 같은 문장의 다섯 자리를 슬라이스 28 이 ③ 로 이미 번복 |

🔴 **D 행의 전건 단정에는 범위 수식어가 빠져 있었다.** 2026-09-06 이 이름으로 든 자리는 전부 **산문
문단**이고, **시그니처를 그대로 옮긴 한 줄 함수 docstring** 은 세지 않았다. 이 패스가 세는 반대 증거는
그 부류 **11자리 / 15줄**이다. 슬라이스 28 이 ③ 축에서 센 다섯과 합치면 그 한 문장에 대한 반대 증거는
두 축에서 각각 나왔다 — 「낡았다」가 아니라 **처음부터 전수를 세지 않은 단정**이다.

## 판정 — 제거 (L 4줄)

| 자리 | 복원처 | 판정 |
| --- | --- | --- |
| `_opensearch.py` `RUN_ID` doc 2줄 중 첫 절 | ① 같은 파일 모듈 docstring 둘째 문단(자기 프로세스의 `RUN_ID` · 인덱스 이름 형태 · 실행 순서 비의존까지 **더 자세하다**) | 첫 절만 제거 · 고유분(「따뜻한 픽스처 재실행」)은 남겨 2줄 -> 1줄 |
| `_helpers.py` `assert_assumed_role_access` 안 3줄 | ① 같은 파일 `BASE_ACCESS_KEY_ID` doc 이 거의 축자(「legitimate for the AssumeRole call itself, and exactly what must NOT appear on a data-plane request」) | 제거 — **상수 자신의 doc 을 정본**으로 둔다(명제를 어길 사람이 읽는 자리) |

## 판정 — 제거 (D 15줄, 전부 README 제거 대상 ① 「선언 재진술」)

| 파일 | 자리 | ① 원본 |
| --- | --- | --- |
| `_helpers.py` | `base_url` | 본문 `argv[1]` / `os.environ.get("MCP_BASE_URL", …)` 축자 |
| `_helpers.py` | `trace_url` | 본문 축자 + 픽스처 포인터는 **같은 파일 http-trace 절**이 더 자세히 담는다 |
| `_helpers.py` | `wait_for_healthz` | 본문 축자(`GET /healthz` 200 폴링) |
| `_helpers.py` | `parse_env_resource` | 시그니처 `-> tuple[str, str]` + 본문 |
| `_helpers.py` | `open_session` | 「skips initialize」는 모듈 docstring 둘째 문단이 **기제까지** 담고, `headers` 해설은 시그니처 + `streamablehttp_client(mcp_url, headers=headers)` |
| `_oidc.py` | `kubectl` · `available_replicas` · `protected_resource_metadata` · `unauthenticated_challenge` | 넷 모두 본문 축자(`available_replicas` 는 jsonpath + `int(raw) if raw else 0`) |
| `_opensearch.py` | `index_for` | 모듈 docstring 둘째 문단이 소유 불변식을 더 자세히 담는다 |
| `_opensearch.py` | `search_until` | 시그니처 `predicate` + 모듈 docstring 넷째 문단 |

## 보존 제약 — 판정 전에 고정했다

닫힌 행이 ① 원본으로 **이름을 든** doc 은 전건 보존한다. 걷으면 그 닫힌 행의 제거 근거가 소급해
사라진다(선결 의존의 역방향).

| 보존 대상 | 어느 닫힌 행의 무엇을 지탱하는가 |
| --- | --- |
| `_session_platform.py` `live_shell_session` · `clear_sessions` · `pod_names` · `inject_through_control_plane` | docstring 행 `session_list_ac1.py` 외 9파일(슬라이스 34) 의 75줄 제거 근거 ⑵⑶⑷ |
| `_auth_variant.py` `assert_unavailable_refusal` + 모듈 docstring 첫 문단 | 같은 행의 auth-variant 공유 문단 3파일 12줄 제거 근거 ⑴ |
| `_helpers.py` `assert_destructive_annotation` | 같은 행의 `session_write_ac3.py` 승격 문장 제거 근거 ⑺ |

**제거 15자리와 이 목록의 교집합은 0**이다 — 원장 전수 `grep` 으로 심볼별 인용 건수를 세어 확정했고
제거 대상은 전원 0건이었다. ⚠️ `search_until` 만 인용 2건이 나왔는데, 그 둘은 **호출자 파일이 넘기는
사유 문자열**(다른 행)을 가리키고 이 파일의 docstring 이 아니다.

## ① 히트인데 유지한 하나 — 갈림

`_helpers.py` 포트포워드 절의 「`ci.yml` 은 그룹당 포워드 하나를 그룹 스텝이 사는 동안만 연다」가
`_oidc.py` 모듈 docstring 과 **같은 명제의 두 벌**이다(`_oidc` 가 `port_forward` 를 import 하므로 한 홉).
그런데 양쪽에서 각자 논증의 **전제**로 쓰인다 — `_helpers` 는 「그래서 이 헬퍼가 있다」, `_oidc` 는
「그래서 AC8 은 짧은 포워드를 쓴다(러너 그룹을 늘리는 대안은 대조 파일이 하나라는 사실과 어긋난다)」.
한쪽을 걷으면 그쪽 논증이 끊기므로 README 「애매하면 남긴다」로 보존하고 **정본 지정을 후속 ① 재판정
후보로** 남긴다.

## 가족 논거를 확장하지 않은 자리

`_oidc.py` 의 `NO_AUTH_STARTUP_ERROR` doc 은 `auth.FromEnv` 를 말하지만 슬라이스 40 이 5/5 소진한
**`FromEnv` 사족 가족**의 여섯째가 **아니다**. 그 가족의 명제는 Go `FromEnv` doc 의
「`It returns (nil, nil) when <VAR> is unset`」이고, 이 자리의 명제는 「오류의 고정 접두 + `main.go` 가
`invalid auth config` 로 로깅하고 `os.Exit(1)` 한다」다 — 계층이 다르다. 논거를 확장하지 않는다.

## 무영향 증명

| 축 | 방법 | 결과 |
| --- | --- | ---: |
| 코드 무접촉 | `ast` 로 모든 docstring 을 걷은 트리를 `ast.unparse` 로 직렬화해 base 와 대조 | **7 / 7 바이트 동일** |
| 비주석 diff | `git diff -U0 -- tests/integration` 전량 줄 감사 | 25줄 전부 주석·docstring · 비주석 **0줄** |
| E 표면 | `eol_lines(7파일)` | **0 -> 0**(이 가족에는 줄 끝 주석이 하나도 없다 — E 행 아홉은 원리적으로 무접촉) |

그래서 `unit tests`·`integration tests`(kind)·`image` 가 돌더라도 결과가 갈릴 수 없다.

## 범위 밖 (후속)

- **`scripts/check_doc_inventory.py` 두 행**(L 7 · D 24 = 31줄) — 슬라이스 48 이 이름으로 넘긴 ①② 잔여.
  내 7파일과 공유 파일이 **0** 이라 union-find 상 같은 슬라이스일 필요가 없고 착지 순서가 자유롭다.
- 🔴 **게이트 D 필터의 앵커 부재** — 슬라이스 49 가 「다음 슬라이스의 몫」으로 넘겼고, 그때의 유예 사유
  (「슬라이스 48 이 막 판정한 행을 다시 건드린다」)는 #248 착지로 **만료됐다**. 이번에 접지 않은 이유는
  별개다: 그것은 판정이 아니라 **게이트 확장**이고(표 신설·게이트 확장은 한 슬라이스라는 규약), 이 가족에
  대한 실측 영향이 **0줄**이라(`docstring_lines` 가 이 7파일에서 지시자 이름을 인용한 산문을 버리는 줄 = 0)
  내 래칫을 움직이지 못한다. **다음 패스가 주 안건으로** 집을 것 — 부록으로 붙이면 또 밀린다.
- **나머지 ①② 미판정 46행 / 3,364줄.** 공유 파일 union-find 로 컴포넌트 46개이고 큰 자리는
  `internal/mcp/gate.go`+`gate_test.go` 416 · `aws_config_get` 17파일 가족 239(L 29 + D 210) ·
  `_gatekeeper.py` 156 · `internal/k8s/resource.go` 177 · `internal/mcp/mcp.go`+`toolslist.go` 126 ·
  `internal/gatekeeper/gatekeeper.go` 125 · `internal/k8s/proxy.go` 124.
