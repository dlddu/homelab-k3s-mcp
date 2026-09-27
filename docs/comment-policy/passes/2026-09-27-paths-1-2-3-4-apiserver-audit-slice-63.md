# 슬라이스 63 — 시나리오 11 이 들인 3행 90줄을 네 경로 전건 판정

`rct_20260927-0010` / `tbm_homelab-k3s-mcp-comment-redundancy`. 판정 대상은 PR #265 가 등재만 해
두고 판정 축을 `—` 로 남긴 세 행이다 — 줄 주석 표면 2행(`tests/k8s/kind/apiserver-audit-variant.yaml`
43줄 · `tests/integration/approval_gate_ac11.py` 13줄)과 docstring 표면 1행(같은 `.py` 34줄).
행별 판정과 근거는 [`../ledger.md`](../ledger.md) 의 결과 칸이 지고, 이 문서는 **패스 국소의 경위와
측정 방법**만 담는다.

## 축을 쪼개지 않은 이유

이 표면의 최근 패스는 ①② 축과 ③④ 축을 따로 돌았다(슬라이스 60·61·62). 이 슬라이스는 넷을 한 번에
닫는다. ③ 의 모집단이 **저작 PR #265 본문 하나**이고 ④ 는 이 레포에서 적용 대상이 0(README
「④의 현재 실태」)이라, 쪼개면 같은 본문을 두 번 읽고 지문을 두 번 움직여 R11 재앵커 비용만 는다.
슬라이스 49 가 같은 가족(시나리오 1 의 fixture + 전용 e2e + probe)을 한 슬라이스로 닫은 선례를
그대로 따랐다.

## 제거 9줄 · 개작 8줄 — 표면별 내역

| 표면 | 범위 | 줄 수 | 지문 | 제거 | 개작 |
| --- | --- | ---: | --- | ---: | ---: |
| L | `apiserver-audit-variant.yaml` | 43 → 39 | `3bdbffeeccf0` → `e98b63faac9b` | 4 | 3 |
| L | `approval_gate_ac11.py` | 13 → 12 | `bd3e84520d1c` → `8d2e72c6960f` | 1 | 3 |
| D | `approval_gate_ac11.py` | 34 → 30 | `ab4cfe2994a7` → `ad5762c7e8fc` | 4 | 2 |

수치는 산술이 아니라 **게이트가 인쇄한 실측**이다 — `scripts/check_comment_policy.py` 의 R2·R6
불일치 메시지가 새 줄 수와 새 지문을 그대로 돌려주므로 그 값을 칸에 옮겼다.

## 이 파일들이 다른 fixture 보다 제거율이 낮은 이유

`apiserver-audit-variant.yaml` 은 43줄 중 4줄만 걷혔다(9%). 같은 형의 `gate-refusal-variant.yaml`
은 슬라이스 49 에서 **전건 유지**였고, 이 파일도 같은 성질이다 — 머리말이 드는 것이 대부분
**부재의 의도**(토큰 복사본이 회전하지 않는다)와 **서 있는 편집 구속**(프록시를 별도 파드로 빼면
개인키를 레포에 커밋해야 한다 · 자동 마운트가 emptyDir 을 덮으면 승자가 구현 세부다)이라 복원 경로
넷 어디에도 없다. YAML 표면의 과거 제거율 41% 를 기대치로 쓰면 이 자리에서는 과대 추정이 된다.

다만 #245(시나리오 1)와 갈리는 점이 하나 있다: **#265 본문은 이 fixture 머리말의 설계 논증을 거의
축자로 되풀이한다.** 그래서 ③ 축이 여기서는 실제로 문다. 그럼에도 걷지 않은 것은 그 셋이 README
3분 표 **둘째 줄**(서 있는 불변식)에 들기 때문이고, 같은 판정을 슬라이스 62 가
`check_doc_inventory.py` ¶6 에서 이미 했다. ③ 가 실제로 처분을 바꾼 자리는 **docstring ¶1 하나**인데,
그것도 ③ 단독이 아니라 같은 명제가 fixture 머리말에 서 있어 **제거 대상 ③(주석끼리의 중복)** 이
먼저 발동한 자리다.

## 쌍둥이를 남긴 자리 — hop-by-hop 과 재전송 Content-Length

이 프록시는 `tests/k8s/kind/http-trace.yaml` 의 `trace_proxy.py` 를 TLS 판으로 베낀 것이라 두 가드
주석이 그 파일에 **쌍둥이**로 있고, 그 행(`http-trace.yaml` 20줄)은 이미 `①②③④` 로 판정받으며 두
문장을 **유지**했다. 사본을 걷지 않은 판별식은 둘이다 — ⑴ 각 주석이 **자기 파일의 프록시 구현**을
구속하므로 제거 대상 ③ 의 「양쪽이 각자 다른 논증의 전제로 쓰면 둘 다 남긴다」에 든다 ⑵ 어겼을 때
**조용히 깨진다**(hop-by-hop 을 흘리면 연결 재사용이 어긋나고, 재전송 길이를 안 지우면 중복
`Content-Length` 가 된다). 한쪽을 걷으면 그것은 `http-trace.yaml` 행의 유지 판정을 번복하는 것이다.

## `#:` 속성 doc 이 전건 유지로 끝나지 않은 첫 행

이 레포 `scripts/check_doc_inventory.py` 행과 econ 쪽 선례는 `#:` 모듈 속성 doc 을 **전건 유지**로
닫았다. 그 근거는 「`#:` 이라서」가 아니라 그 줄들이 규약의 소유자·조용한 0 의 회피처럼 ① 밖 내용을
들었기 때문이다. `approval_gate_ac11.py` 의 세 `#:` 는 **앞 절만** 상수 이름의 재진술이라
(`스케일 목표.` ↔ `SCALE_TARGET_REPLICAS` · `` `cluster-admin` 이 덮는 것으로 보는 verb 집합.`` ↔
`COVER_ALL_CLUSTER_ROLES = {"cluster-admin"}`) 절 단위로 걷고 뒤 절(측정 가능성 근거 · 경계 선언)을
남겼다. `PRD_PATH` 의 한 줄만 두 절이 모두 복원돼 통째로 걷혔다.

## 무영향 증명 — 클러스터 없이

e2e 는 kind 하네스가 필요해 이 호스트에서 돌지 않는다. 그래서 거동 불변을 **바이트·AST 두 축**으로
닫았다.

- `apiserver-audit-variant.yaml`: 줄머리 `#` 줄을 전부 뺀 나머지가 부모 커밋과 **바이트 동일**.
- `approval_gate_ac11.py`: ⑴ 모든 docstring 을 벗긴 AST(`ast.dump`)가 부모와 **동일** ⑵ docstring
  줄과 `#` 줄을 뺀 줄 목록이 부모와 **바이트 동일**. 임베드 `audit_proxy.py` 는 ConfigMap 문자열
  안이라 ⑴ 의 yaml 축이 함께 덮는다.

즉 이 PR 이 움직인 바이트는 전부 주석·docstring 안에 있다.

## 게이트 다섯

`scripts/check_comment_policy.py` · `scripts/check_mock_policy.py` ·
`scripts/check_doc_inventory.py` · `tests/integration/check_ac_mapping.py` ·
`python3 -m compileall -q tests/integration` 전건 rc=0.

이 문서 자신이 `docs/` 에 `.md` 를 하나 더하므로 `check_doc_inventory.py` 의 **D4**(허브 집계 ↔ 실측)
가 깨진다. `docs/doc-tracker/2026-09.md` 의 「허브 도달 가능 문서」 줄을 총계 122 → 123 · 정책
82 → 83 으로 함께 올렸다. 주석 패스는 매번 파일을 낳으므로 이 자리는 매번 같이 움직인다.

준비 중에 자매 슬라이스 #266(`rct_20260927-0008`)이 착지해 base 가 `ab3a213` 에서 `b197938` 로
움직였다. 그 PR 은 `tests/integration/check_ac_mapping.py` 와 `docs/doc-tracker/2026-09.md` 를
고쳤는데 **주석을 0줄 더했고**(그 파일의 원장 행은 `①②③④` 로 닫혀 있어 한 줄만 더해도 축이 `—` 로
되돌아간다) 허브 집계 줄도 건드리지 않아, 이 패스의 세 행과 집계 앵커는 그대로였다. 위 수치는
`b197938` 위에서 다시 잰 값이다.

## 범위 밖

이미 `①②③④` 로 닫힌 84행의 재판정, README 가 control plane 소관으로 못박은 D 표면 262줄 격차,
주석의 정확성·문서 품질(정책이 보지 않는 것). 이 PR 착지로 as-is 지문이 움직여 재감지가 돌지만
새 `—` 행은 생기지 않는다 — 세 표면 모두 미판정 0행이다.
