# 복원 경로 ①②③④ 판정 — 슬라이스 48: `scripts/` 정책 도구 가족

- **task**: `rct_20260926-0018` (`tbm_homelab-k3s-mcp-comment-redundancy`)
- **기준 커밋**: `db2f0d5` (#247 착지 직후 · 대상 레포 열린 PR **0건**)
- **대상**: 세 표면에서 판정 축이 `—` 인 17행 484줄 중, `scripts/check_comment_policy.py` ·
  `scripts/check_mock_policy.py` · `scripts/test_check_mock_policy.py` 세 파일이 드는 **4행 243줄** —
  L 59 (`09cf6251a575`) · D 180 (`e7dc920db1dc`) · E 2 (`ac3e92b48f1c`) · E 2 (`1a7385497564`).
- **결과**: **제거 108줄 · 추가 0줄**(243 → **135**). L 완료 31행 1204줄 → **32행 1235줄**(32.4% → 33.5%) ·
  D 완료 7행 405줄 → **8행 509줄**(19.8% → 25.9%) · E 완료 0행 → **2행**(둘 다 0줄이 되어 비율은 0.0% 그대로) ·
  미판정 `—` L 6→**5**행 · D 2→**1**행 · E 9→**7**행.
- **게이트**: `check_comment_policy`(R1~R15) · `check_mock_policy` · `check_ac_mapping` ·
  `check_doc_inventory` · `python3 -m unittest discover -s scripts -p test_check_mock_policy.py`(16건) 전부 rc=0.

## 왜 이 넷인가 — 슬라이스를 가른 것은 주제가 아니라 CI 의 벽

잔여 484줄은 예산 400줄을 넘어 한 슬라이스에 들어가지 않는다. 쪼개는 축을 파일 공유 덩어리 안에서
다시 고를 때 이 패스가 쓴 판별식은 **`ci.yml` 의 `changes` 잡**이다. 그 잡은 `docs/*|scripts/*|*.md` 만
바뀌면 `app=false·go=false·image=false` 로 분류해 `lint`(fmt + vet) 한 잡만 돌리고, 그 밖의 파일이
하나라도 닿으면 kind 통합 스위트와 도커 빌드가 함께 깨어난다.

잔여를 그 축으로 가르면 정확히 둘로 갈린다.

| 레인 | 행 | 줄 |
| --- | --- | ---: |
| `docs/`·`scripts/` (이 슬라이스) | L 1 · D 1 · E 2 | 243 |
| 코드(`internal/`·`tests/`) | L 5 · D 1 · E 7 | 241 |

둘 다 예산 안이고 합이 잔여 전건이다. 주제로 묶으면(예: 「승인 게이트 e2e 가 들인 신설 파일」)
두 레인이 섞여 주석 한 줄을 걷는 PR 이 kind 클러스터를 20분 돌리게 된다.

## 판별식 — ②의 소유자를 먼저 세운다

걷은 108줄의 대부분은 **정책 문서가 같은 명제를 이미 들고 있는 자리**다. 다만 「두 벌이 있다」가
곧 「주석을 걷는다」는 아니다 — 명제를 어길 사람이 읽는 자리가 어디인지를 먼저 정했다.

- **범위·원장 형식·미판정 선언의 규약** → 정본은 `docs/comment-policy/README.md` 다. 그 문서를
  고치는 사람이 규약을 바꾸고, 게이트는 그 규약을 집행할 뿐이다. 그래서 게이트 쪽 사본을 걷었다.
- **모킹 규칙 R1~R7·B2** → 정본은 `docs/e2e-mocking-policy.md` 의 「집행 (체커)」 절이다. 그 절은
  495–517행에서 같은 표로 R1~R7·B2 를 들고, 머리말이 「python3 표준 라이브러리 전용, 클러스터
  불필요. CI의 `lint` 잡에서 돈다」와 `git ls-files -- tests .github/workflows` 범위까지 적는다.
  그래서 `check_mock_policy.py` 모듈 docstring 의 규칙 목록 전건을 걷었다.
- **`check_comment_policy.py` 의 R1~R15 규칙 문면** → 정본이 **그쪽에 없다.** README 는 R4·R8·R9·
  R10·R12·R15 를 지나가며 언급할 뿐 *집합*을 들지 않는다. 그래서 이쪽 규칙 목록은 **남겼다** —
  앞 패스가 ③ 축에서 「규칙 집합을 복원하는 PR 본문은 없다」로 내린 것과 같은 결론이 ①② 에서도 선다.
  **답이 파일마다 갈리는 것이 이 패스의 요점이다.**

## 남긴 다섯 자리 — 어기면 조용히 깨진다

1. **바이트 일치 검산법** — 같은 트리에서 게이트 인구조사와 모델 지문 스크립트의
   `lines=/files=/unclassified=` 가 어긋나면 그중 하나가 틀렸다. README 는 「함께 고칠 것」까지만
   적고 *어떻게 확인하는가*는 적지 않는다.
2. **`_INDENT` 의 `\s` 금지** — `\s` 로 바꾸면 유니코드 공백까지 걸려 모델 ERE 보다 넓어진다.
   넓은 쪽이 안전해 보이는 것이 함정이고, 갈려도 양쪽 다 초록이다.
3. **언어군 표의 순서** — `requirements.txt` 가 아래 NONE 군의 `\.txt` 에 먼저 걸리면 그 파일의
   주석이 통째로 사라진다. 군을 재배열하는 편집은 CI 를 붉히지 않는다.
4. **경계 주석 마커를 두지 않는다** — 표를 절 제목으로 찾는 이유. 「무엇을 넣지 말라」는 ③ 가
   집행하지 못한다(README 「복원 경로는 넷이다」의 3분 표 둘째 줄).
5. **`ID_TOKEN_RE` 의 `(?![A-Za-z0-9_])`** — 빼면 정책 문서 경로 자체가 `e2e-mock` 으로 잡혀 R2
   오탐이 나고, 하이픈을 배제 목록에 넣으면 `github-mock-script` 류 파생 이름을 놓친다.

## 선행 판정을 뒤집지 않았다 — `# Rn —` 앵커 14줄

이 행들은 두 체커의 `# R1 —`·`# R9 —` 류 규칙 앵커를 **「긴 `main()` 의 항해 표지」** 로 유지해 두었고,
그 판정은 ① 축에서 「모듈 docstring 의 재진술에 가깝지만 애매하면 남긴다」로 내려져 있었다.

이 패스는 그 논거의 **전제**를 다시 쟀다 — `main()` 은 origin `db2f0d5` 에서 **131줄**이다(판정 당시의
「100줄짜리」보다 길다). 전제가 서 있으므로 결론도 선다. 실제로 한 번 걷었다가 **되돌렸고**, 그
되돌림이 L 행의 최종 실측(18 → 31줄)을 만든다. 낡은 것은 논거가 아니라 「두 파일 모두 모듈
docstring 이 규칙 목록을 든다」는 **사실 쪽**인데(이 패스가 `check_mock_policy.py` 쪽을 걷었다),
그 변화는 앵커를 **더 필요하게** 만들지 덜 필요하게 만들지 않는다.

## ③④ — 실측으로 닫았다

- **③**: 세 파일을 건드린 커밋 **15개**의 PR 본문(#47·#57·#63·#66·#69·#74·#110·#126·#151·#156·
  #216·#239·#243·#244·#246)을 전수로 받아, 남긴 다섯 자리의 명제를 토큰으로 그렙했다 —
  `[[:space:]]`·`유니코드 공백`·`NONE 군`·`경계 주석`·`A-Za-z0-9_`·`github-mock-script`·`견본 접미사`
  전부 **0건**. 걸린 둘(`표준 라이브러리`)은 「체커는 표준 라이브러리 전용」이라는 별개 명제이고,
  그 명제는 이 패스가 ② 로 걷은 자리다.
- **④**: 같은 15개 커밋을 `/commits/<sha>/pulls` 로 전수 조회해 **15/15 가 PR 로 착지**했음을 확인했다.
  README 「④의 현재 실태」가 레포 전체에 대해 적어 둔 「적용 대상 0」이 이 범위에서도 성립한다 —
  제목의 `(#N)` 표기가 아니라 API 로 쟀다.

## 범위 밖(후속) — 코드 레인 13행 241줄

다음 주석 슬라이스가 그대로 가져가면 된다. 예산 400줄 안이다.

| 표면 | 범위 | 줄 |
| --- | --- | ---: |
| L | `internal/mcp/undeclared_tool_probe.go` | 26 |
| L | `tests/integration/approval_gate_ac1.py` | 16 |
| L | `tests/k8s/kind/gate-refusal-variant.yaml` | 49 |
| L | `internal/mcp/confine.go` | 60 |
| L | `internal/mcp/confine_test.go` | 33 |
| D | `tests/integration/approval_gate_ac1.py` | 48 |
| E | `internal/auth/auth_test.go` | 3 |
| E | `internal/mcp/confine_test.go` | 1 |
| E | `internal/mcp/eventlog_test.go` | 1 |
| E | `internal/mcp/gate_test.go` | 1 |
| E | `internal/mcp/mcp.go` | 1 |
| E | `internal/mcp/resource_test.go` | 1 |
| E | `tests/k8s/kind/http-trace.yaml` | 1 |

그 슬라이스는 `internal/`·`tests/` 를 건드리므로 `changes` 잡이 `app=true`(`internal/mcp/mcp.go` ·
`confine.go` 때문에 `image=true` 도)로 분류해 kind 통합 스위트와 도커 빌드가 함께 돈다. 주석만
걷는 PR 이라도 그 대기는 정상이다.

이와 별개로 남는 것: `scripts/check_doc_inventory.py` 의 두 행(L 7줄 · D 24줄)이 `③④` 만 닫아 둔
①② 잔여 · L 47행 / D 25행의 ①② 잔여 전반 · D 표면 262줄 격차(control plane 소관, README
「D 표면의 262줄」이 든다).
