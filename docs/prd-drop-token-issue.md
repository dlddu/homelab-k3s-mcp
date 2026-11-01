# PRD: drop_token_issue

클러스터 안의 파일 공유 서비스 drop(`dlddu/drop`, 공개 주소 `https://drop.llkm.nl`)의 **단기 API 토큰만**
발급하는 도구. 파일을 실제로 올리고 내려받는 일은 에이전트가 셸에서 `curl` 로 한다.

## 달성 가치
- **V5: 클러스터 내부 앱 기능의 도구화** — 클러스터 안에서만 닿는 drop 의 서비스 계정 교환 엔드포인트
  (`POST /api/v1/sa/token`, dlddu/drop#2)를 도구로 열어, 에이전트가 운영자에게 파일을 건네거나(최대 30일
  보관, 받는 쪽은 아이디 + 비밀번호로 내려받는다) 운영자가 올린 파일을 가져가는 일을 포트포워딩·인그레스
  계정 없이 한다. 노출하는 것은 「토큰 한 장 발급」뿐이다.
- **V2: 단명·최소권한 자격증명** — 발급되는 것은 기본 1시간(최대 24시간) 뒤 만료되는 drop 전용 API 키이고,
  서버의 베이스 자격증명(audience `drop` 으로 좁힌 projected ServiceAccount 토큰)은 서버 밖으로 나가지 않는다.
  공유 비밀(정적 API 키)을 두지 않는다.
- **V3: 안전한 운영(Safe-by-default)** — 파일 내용은 구조적으로 도구를 지나지 않고(입력 스키마에 그 자리가
  없고 결과에도 없다), 잘못된 인자는 drop 에 닿기 전에 거부하며, drop 의 거부 사유를 구분해 돌려주고, 미설정
  시 graceful 거부가 기본값이다.

## 도구 개요
- 입력(전부 선택, 그 밖의 속성은 거부 — `additionalProperties: false`):
  - `valid_minutes` — 정수 1~1440, 기본 60. 토큰 유효 시간(분)
  - `max_days` — 정수 1~30. 이 토큰으로 만든 보관함이 요청할 수 있는 최대 보관 일수. 없으면 drop 의 상한(30)
  - `label` — 문자열, 공백 제거 후 40자 이하. drop 이 토큰과 함께 기록한다(앞에 `sa:<ns>/<sa> ` 가 붙는다)
  - `reason` 은 없다 — 승인 게이트 대상이 아니다
- 동작: 서버가 자기 ServiceAccount 토큰(audience `drop`)을 Bearer 로 drop 의 `POST /api/v1/sa/token` 에
  보내고, drop 은 TokenReview 로 그 계정을 확인한 뒤 허용 목록(`SA_ALLOW`)에 있으면 `drk_…` API 키를 발급한다.
- 반환(`structuredContent`): `token`, `expires_at`(RFC3339), `max_days`, `base_url`(curl 을 보낼 주소 —
  drop 의 `PUBLIC_BASE_URL`), `created_by`(`sa:<ns>/<sa>`), `usage`(셸에서 그대로 돌리는 curl 견본 —
  `setup`·`upload`·`upload_note`·`list`·`delete`·`download_list`·`download_file`·`download_zip`·`note`).
  **파일 내용·파일 목록·보관함 정보는 반환하지 않는다.**
- 서버 요구 설정:
  - `DROP_URL` — drop 주소. 있어야 도구가 켜진다. 운영 `http://drop-external.drop.svc.cluster.local`
    (ExternalName → Traefik → Sablier → drop)
  - `DROP_TOKEN_FILE` — projected 토큰 경로, 기본 `/var/run/secrets/drop/token`(`k8s/deployment.yaml` 의
    `drop-token` 볼륨, audience `drop`, 3600초)
  - drop 쪽: `SA_ALLOW` 에 `homelab-k3s-mcp/homelab-k3s-mcp`, drop 의 ServiceAccount `drop` 에
    `system:auth-delegator` 바인딩
- 어노테이션: `readOnlyHint=false`, `destructiveHint=false`, `idempotentHint=false`, `openWorldHint=true`
  (readOnly=false: drop 에 자격 증명을 하나 만든다. idempotent=false: 부를 때마다 새 토큰이다.
  destructive=false: 지우거나 덮어쓰는 것이 없다)
- 승인 게이트: 대상 밖이다. drop API 호출이지 쿠버네티스 권한을 행사하지 않는다(`touchesNothing` 표지 —
  「reaches drop, never the apiserver」). `grafana_token`·`github_app_installation_token` 과 같은 자리다.

> **구현 상태(2026-11-01)**: `internal/drop` 과 `internal/mcp` 의 등록·디스패치, `k8s/deployment.yaml` 의
> `DROP_URL`·`drop-token` 볼륨이 이 PRD 와 같은 변경에서 착지했다. 상대 엔드포인트는 dlddu/drop#2 이고
> kind 하네스는 그 PR 이 발행한 이미지(`ghcr.io/dlddu/drop:4360def…`)를 실물로 띄운다
> (`tests/k8s/kind/drop.yaml`). 시나리오별 자동화 현황은 `test-drop-token-issue.md` 의 각 「자동화」 필드가 말한다.

## Acceptance Criteria

### AC1: 서비스 계정 교환으로 단기 토큰을 발급한다
- **설명**: 호출하면 서버의 projected ServiceAccount 토큰이 `Authorization: Bearer` 로 drop 의
  `POST /api/v1/sa/token` 에 한 번 나가고, 본문은 `{valid_minutes, max_days?, label?}` 다(`valid_minutes` 는
  생략 시 60). drop 의 201 응답의 `token`·`expires_at`·`max_days`·`created_by`·`base_url` 이 그대로(`base_url`
  끝의 `/` 만 걷어) 결과에 실린다. 토큰 파일은 호출마다 다시 읽는다(kubelet 이 제자리 교체한다). 잠든 drop 을
  깨우는 Sablier 가 같은 URL 로 307 을 주면 본문째 다시 보낸다(요청 예산 45초).
- **달성 가치**: V2, V5
- **검증 방법**: `{valid_minutes: 5}` 로 부르면 `expires_at` 이 지금 + 5분이고 `token` 이 `drk_` 로 시작하며
  `created_by` 가 `sa:homelab-k3s-mcp/homelab-k3s-mcp` 다. 토큰 파일을 바꾸면 다음 호출이 새 토큰을 쓴다.
  첫 응답이 307 이면 두 번째 요청이 같은 본문·같은 Bearer 를 싣는다.

### AC2: 결과는 셸에서 그대로 쓰는 curl 견본을 담는다
- **설명**: `usage` 는 명령 문자열들의 객체다. 토큰 원문은 `setup`(`export DROP_TOKEN='…'`) 한 줄에만 있고,
  토큰이 필요한 명령(`upload`·`list`·`delete`)은 `-H "Authorization: Bearer $DROP_TOKEN"` 으로 가리킨다.
  모든 명령은 결과의 `base_url` 을 쓴다. 내려받기 견본(`download_file`·`download_zip`)은 아이디·비밀번호만
  쓰고(토큰 불필요) **항상 `-o` 로 디스크에 저장한다**. 업로드 견본의 `days` 는 7 이되 토큰의 `max_days` 가 더
  작으면 그 값이다. 큰 파일의 조각 업로드 API 와 토큰 만료 시 재호출은 짧은 안내 문장으로만 싣는다.
- **달성 가치**: V5
- **검증 방법**: 견본의 `base_url` 만 하네스 주소로 바꾼 채 그대로 실행해 파일을 올리고, 응답의 아이디·비밀번호로
  `download_file` 을 돌리면 표준 출력은 비어 있고 저장된 파일의 바이트가 올린 것과 같다. `list` 에 그 보관함이
  있고 `delete` 뒤에는 없다. `usage` 직렬화에서 토큰 원문은 정확히 한 번 나온다.

### AC3: 파일 내용은 도구를 지나지 않는다
- **설명**: 도구는 파일 내용을 **받지도 돌려주지도 않는다**. 입력 스키마의 속성은 `valid_minutes`·`max_days`·
  `label` 셋뿐이고 `additionalProperties: false` 이며, 그 밖의 인자(`file`·`content` 등)는 실행 전에 인자 오류로
  거부된다. 결과는 AC1 의 다섯 필드와 `usage`(명령 문자열)뿐이고 drop 의 보관함·파일 정보를 읽어 오지 않는다.
  도구 설명은 「이 도구는 파일 내용을 보내거나 받지 않는다 — 올리기·내려받기는 curl 로, 내려받기는 `-o` 로
  저장한다 — 파일 바이트를 대화 맥락 밖에 두기 위해서다」를 말한다. 서버가 drop 에 보내는 본문도 AC1 의 세
  필드뿐이다.
- **달성 가치**: V3
- **검증 방법**: `tools/list` 의 속성 집합이 정확히 셋이고 이름에 파일·내용류가 없다. `{file: …}`·`{content: …}` 는
  JSON-RPC 인자 오류다. 고유 표지 문자열을 담은 파일을 견본으로 올린 뒤 다시 부른 결과의 `structuredContent` 와
  `content` 어디에도 그 표지가 없다.

### AC4: 입력 검증이 drop 보다 앞선다
- **설명**: `valid_minutes` 가 정수가 아니거나 1~1440 밖이면, `max_days` 가 정수가 아니거나 1~30 밖이면,
  `label` 이 문자열이 아니거나 40자를 넘으면 drop 에 요청하지 않고 인자 오류(-32602)로 거부한다. 이 검증은
  drop 설정 여부와 무관하게 같다. 경계값(1440·30·40자)은 통과한다.
- **달성 가치**: V3
- **검증 방법**: 각 범위 밖 값이 인자 오류이고 drop 의 발급 기록이 늘지 않는다. 경계값 호출은 drop 에 정확히
  한 번 닿는다. 미설정 배포에서도 범위 밖 값은 같은 인자 오류다.

### AC5: drop 의 거부 사유를 구분해 돌려준다
- **설명**: drop 이 400(인자)·401(토큰 무효·audience 불일치)·403(`SA_ALLOW` 밖)·502(TokenReview 실패 —
  `system:auth-delegator` 미바인딩)·503(교환 미설정)으로 답하면 각각 원인과 고칠 자리를 말하는 도구 에러가
  돌아온다(drop 이 준 사유 포함). 그 밖의 응답은 상태 코드와 본문 앞 300자를 담는다. 201 인데 토큰이 없으면
  에러다. 이 거부들은 미설정(AC6)과 다르게 읽힌다.
- **달성 가치**: V3
- **검증 방법**: 각 상태 코드마다 도구 에러 문구가 서로 다르고 고칠 자리(`audience "drop"`·`SA_ALLOW`·
  `system:auth-delegator`)를 이름으로 대며, 어느 것도 unavailable(미설정) 류가 아니다.

### AC6: 미설정 시 graceful 거부
- **설명**: `DROP_URL` 이 없으면 `drop unavailable: drop is not configured (DROP_URL)` 도구 에러를 반환하며,
  서버 기동·다른 도구에는 영향을 주지 않는다. `DROP_URL` 은 있는데 토큰 파일을 읽을 수 없거나 URL 이 절대
  http(s) 가 아니면 기동 시 그 사유로 unavailable 이 된다(기동은 계속된다).
- **달성 가치**: V3
- **검증 방법**: 미설정 배포에서 호출하면 위 문구의 도구 에러이고, 직후 `ping` 이 `pong` 이다.

## 결정 사항
- **도구는 토큰만 발급하고, 파일은 에이전트가 `curl` 로 직접 옮긴다(2026-11-01, 소유자 결정).** 소유자의 말로:
  「mcp 도구에 직접 파일이 왔다갔다하면 컨텍스트를 많이 사용하니까 그 문제를 방지하기 위해서야」. MCP 도구의
  인자와 결과는 대화 맥락에 그대로 실린다 — 파일 바이트(대개 base64 로 더 부푼다)를 도구로 주고받으면 파일
  하나가 맥락을 통째로 태운다. 그래서 도구는 토큰과 명령 몇 줄만 돌려주고, 바이트는 셸 안에서 `curl` 로만
  움직인다. 이 결정이 AC3(스키마·결과에 내용 자리가 없다)과 AC2 의 「내려받기는 항상 `-o`」의 근거다 —
  `-o` 없이 내려받으면 내용이 터미널 출력으로 맥락에 들어와 같은 문제가 셸 쪽에서 되살아난다.
  업로드·다운로드·목록·삭제 도구를 따로 두지 않는 것도 같은 이유다.
- **인증은 projected ServiceAccount 토큰 교환이고 공유 비밀을 두지 않는다(2026-11-01).** drop 에 정적 API 키를
  만들어 Secret 으로 넣는 길도 있었지만, 그러면 회전·유출 대응이 사람 몫으로 남는다. ServiceAccount 토큰은
  kubelet 이 1시간마다 갈고 audience `drop` 으로 좁혀져 다른 상대에게 쓸 수 없으며(PocketAide 토큰과 볼륨을
  나눈 이유), drop 은 TokenReview 와 `SA_ALLOW` 로 호출자를 확인한다. 승인 게이트(PocketAide)와 같은 형태다.
- **기본 유효 시간은 1시간이다(2026-11-01, 소유자 결정).** 한 작업에서 올리고 받기에 충분하고, 새는 경우의
  창을 짧게 둔다. 길게 필요하면 `valid_minutes` 로 최대 24시간까지 받고, 만료되면 다시 부른다.
- **`usage` 의 토큰은 `$DROP_TOKEN` 자리표시자로 가리킨다.** 명령마다 토큰 원문을 박으면 같은 비밀이 대화
  맥락과 셸 이력에 명령 수만큼 남는다. 원문은 `setup` 한 줄에만 둔다.
- **라벨 상한은 40자다.** drop 은 라벨 앞에 `sa:<ns>/<sa> `(운영 35자)를 붙인 전체를 80자로 제한한다. 80 을 그대로
  받으면 41자부터 drop 의 400 이 되므로, 도구가 먼저 좁게 거부한다.
- **승인 게이트 대상이 아니다.** 쿠버네티스 권한을 행사하지 않고, 발급되는 토큰은 drop 안에서만 쓰이는 단명
  키다. 받은 파일은 이미 운영자가 공유하려고 둔 것이다.

## 열린 질문
- 없음.
