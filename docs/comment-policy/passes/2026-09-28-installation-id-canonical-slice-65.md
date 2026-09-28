# 슬라이스 65 — `EXPECTED_INSTALLATION_ID` 가족의 정본을 세우고 사본 다섯 벌을 걷는다

소유자 결정 / `tbm_homelab-k3s-mcp-comment-redundancy`. 슬라이스 59·64 가 두 번 연속 「정본이 될 자리가
이 행들 밖(`_helpers.py`)이고, 그 자리를 세우는 것은 코드 변경이라 주석 슬라이스의 산출물이 아니다」로
유예한 가족이다. 소유자가 상수를 `tests/integration/_helpers.py` 로 모으기로 정해 그 벽이 무너졌다.
행의 판정과 근거는 [`../ledger.md`](../ledger.md) 의 결과 칸이 지고, 이 문서는 **패스 국소의 경위와
측정 방법**만 담는다.

## 무엇이 벽이었나

같은 명제 — 「`67890` 은 CI 「Create test GitHub App secret」 스텝의 `GITHUB_APP_INSTALLATION_ID` 와
맞아야 한다」 — 가 주석 **여섯 벌**로 서 있었다: `github_app_installation_token_ac1` · `_ac2` · `_ac4` ·
`_ac5` · `github_commit_status_ac2` 의 줄 주석 다섯과 `github_app_installation_token_ac5_checks` 의
모듈 docstring 하나. 복원 경로 ①~④ 는 전부 비어 있다(`.github/workflows` 는 모델 SSOT 범위 밖) —
그래서 README 「제거 대상 ③」의 처분인 **정본 지정**이 걸리는데, 여섯 벌이 각자 자기 파일의 선언 위에
서 있어 어느 하나를 정본으로 고르면 나머지 다섯 파일은 **자기 선언 위에 가드가 없는** 상태가 된다.
정본이 설 자리는 선언이 한 곳인 파일뿐이었고, 그런 파일은 없었다.

## 코드 변경 — 선언을 한 곳으로

- `tests/integration/_helpers.py` 에 `GITHUB_APP_INSTALLATION_ID = "67890"` 을 두고, 그 위에 결합 2줄을
  둔다. 이름은 CI 가 쓰는 env 이름 그대로다 — 가족이 쓰던 두 이름(`EXPECTED_INSTALLATION_ID` ·
  `INSTALLATION_ID`)을 하나로 합쳤다.
- 여섯 파일은 선언을 지우고 그 이름을 import 한다. 파생값(`MINT_PATH` · `INSTALLATION_PATH` ·
  `MINTED_TOKEN` · `ghs_mock_…`)은 각 파일에 그대로 둔다 — 파일마다 쓰는 파생이 달라 모으면 헬퍼가
  사용처 없는 이름을 떠안는다.
- `ci.yml` 은 건드리지 않는다. 값 `67890` 은 테스트 쪽 1곳 + CI 쪽 2곳(`:256` · `:266`)이 된다.

## 정본의 자리

README 「정본은 그 명제를 어길 사람이 읽는 자리다 … 값을 옮기면 깨지는 명제는 선언 지점의 doc이
정본이다」. 선언이 `_helpers.py` 한 곳이 됐으므로 정본도 거기다. 다섯 행의 결과 칸이 모두 이 자리를
**이름으로** 적는다(「적을 수 없으면 사본을 걷은 것이 아니다」).

## 내역

| 표면 | 행(첫 범위) | 줄 수 | 지문 | 변화 |
| --- | --- | ---: | --- | --- |
| L | `tests/integration/_helpers.py` | 107 → 109 | `d3e4a850b228` → `d13fafc0523d` | 정본 +2 |
| L | `tests/integration/aws_config_get_ac1.py` | 26 → 20 | `6495c697c5d1` → `b57ebf53fd6a` | 사본 −6 (`_ac1`·`_ac2`·`_ac4`) |
| L | `tests/integration/github_app_installation_token_ac5.py` | 2 → 0 | `af94de9d38c2` → `e3b0c44298fc` | 사본 −2 |
| L | `tests/integration/github_commit_status_ac1.py` | 11 → 9 | `4a959c1d4c16` → `31dd22b03fbe` | 사본 −2 (`github_commit_status_ac2`) |
| D | `tests/integration/github_app_installation_token_ac5_checks.py` | 22 → 19 | `f6f05a5b0038` → `997a544f9e81` | 사본 −3 |

수치는 산술이 아니라 **게이트가 인쇄한 실측**이다 — `scripts/check_comment_policy.py` 의 R2·R6
불일치 메시지가 돌려준 새 줄 수·지문을 그대로 칸에 옮겼다. 순증감은 −11줄(사본 −13 · 정본 +2)이다.

## 재앵커의 근거

다섯 행 모두 이 패스가 들인 새 명제는 0이다. 정본 2줄은 자매 행들에서 네 경로를 전부 물어 **유지**로
닫힌 문면을 축자로 옮긴 것이고, 나머지는 사본을 걷기만 했다. 걷은 자리 밖의 주석·docstring 줄은
base 와 바이트 동일이므로 각 행의 네 축 판정은 그대로이고 지문만 움직였다 — 그래서 `판정 축` 은
`①②③④` 를 유지하고 두 축을 새 지문에 다시 묶었다(R11).
