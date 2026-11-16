# PRD: pocket_aide_chat_presence_get

PocketAide(`dlddu/pocket-aide`)의 채팅 화면을 사용자가 **지금 보고 있는지** 조회하는 읽기 전용 도구.

## 달성 가치
- **V5: 클러스터 내부 앱 기능의 도구화** — 클러스터 안에서 도는 PocketAide 의 접속 상태 조회 엔드포인트
  (`GET /api/external/chat-presence`, pocket-aide PRD-12 AC22)를 도구로 열어, 에이전트가 **지금 어떻게
  말을 걸어야 닿는지**를 안다. 노출하는 것은 「접속 상태 한 건 읽기」뿐이고 조회 대상은 인자가 아니다.
- **V3: 안전한 운영(Safe-by-default)** — 아무것도 바꾸지 않는 읽기이고, 인자가 없어 남의 접속 상태를
  묻는 표면이 아예 없다. 돌려받는 것은 접속 여부·마지막 읽기 시각·창 길이 셋뿐이라 대화 내용이
  도구 결과로 흘러나오지 않는다. PocketAide 의 거부 사유(호출자 미등록·구 버전)를 구분해 돌려주며,
  미설정 시 graceful 거부가 기본값이다 — **「모른다」를 「없다」로 답하지 않는다.**

## 왜 필요한가

`pocket_aide_notification_send` 는 사용자가 찾으러 오기 전에 폰을 울리는 수단이고, 채팅 탭의 답은
사용자가 그 화면에 있을 때 즉시 읽히는 수단이다. 둘 중 무엇을 쓸지는 **사용자가 지금 화면을 보고 있는가**로
갈리는데, 에이전트에게는 그걸 알 길이 없었다. 그래서 둘 다 하거나(보고 있는 사람의 폰을 괜히 울린다)
아무것도 하지 않는다(떠난 사람에게 채팅으로만 답하고 몇 시간 뒤에 읽힌다). 이 도구는 그 갈림길에
사실 하나를 놓는다.

## 도구 개요
- 입력: **없다.** `tools/list` 의 스키마가 속성 0 · `additionalProperties: false` 이고, 인자를 실어
  부르면 보내기 전에 거부된다(AC3).
- 동작: PocketAide 에 `GET /api/external/chat-presence` 한 건을 이 서버의 ServiceAccount 토큰으로
  보낸다. 조회 대상은 PocketAide 에서 이 서버의 ServiceAccount(`homelab-k3s-mcp/homelab-k3s-mcp`)를
  호출자로 등록한 사용자 하나다. 승인 요청을 만들지 않고, 알림을 보내지 않으며, 사용자를 방해하지 않는다.
- 반환: `{present, last_seen_at, ttl_seconds}`
  - `present` — 채팅 화면이 `ttl_seconds` 안에 대화를 읽었는가. PocketAide 의 채팅 화면은 보이는
    동안에만 대화를 다시 읽으므로(pocket-aide PRD-1 AC1·AC8, 유휴 15초) 이 값은 **「채팅 화면이 최근
    45초 안에 열려 있었다」**는 뜻이다. 「폰 잠금이 풀렸다」나 「앱이 켜져 있다」가 아니다.
  - `last_seen_at` — 마지막 읽기의 unix 초. PocketAide 가 그 화면을 한 번도 못 봤으면 `null` 이다.
    **`null`(모름)과 `present: false` + 시각(있었다가 떠남)은 다르다** — 섞으면 에이전트가 「한 번도 본
    적 없음」을 「방금 떠남」으로 읽는다.
  - `ttl_seconds` — 그 창의 길이(45). 값을 서버가 외우지 않고 PocketAide 가 말한 것을 그대로 넘긴다.
- 서버 요구 설정: 알림 도구와 같은 `APPROVAL_GATE_URL`(PocketAide 주소)과 projected 토큰
  (`APPROVAL_GATE_TOKEN_FILE`, 기본 `/var/run/secrets/pocket-aide/token`, audience `pocket-aide`).
  새 시크릿·네트워크 설정이 없다.
- 어노테이션: `readOnlyHint=true`, `destructiveHint=false`, `idempotentHint=true`, `openWorldHint=true`
  (읽기이고 되풀이해도 상태가 바뀌지 않는다. 답은 시간이 지나면 달라지지만, 그건 호출이 만든 변화가
  아니라 사람이 화면을 떠난 변화다.)
- 승인 게이트: 대상 밖이다. PocketAide API 읽기이지 쿠버네티스 권한을 행사하지 않으므로
  `prd-approval-gate` 의 정의 범위 밖이고(`pocket_aide_notification_send` 와 같은 자리), 바꾸는 것이
  없어 「승인 없이는 상태가 바뀌지 않는다」는 보증과도 무관하다.

> **구현 상태(2026-11-16)**: `internal/pocketaide`(클라이언트)와 `internal/mcp`(등록·디스패치)가 이 PRD 와
> 같은 변경에서 착지했다. 상대 엔드포인트는 dlddu/pocket-aide 의 PRD-1 AC8 · PRD-12 AC22 로 들어간다.
> **PocketAide 가 먼저 배포돼야 한다** — 그 전의 PocketAide 에는 이 경로가 없어 404 이고, 도구는 그것을
> 「구 버전을 배포 중」으로 구분해 알린다(AC4). 시나리오별 자동화 현황은
> `test-pocket-aide-chat-presence.md` 의 각 「자동화」 필드가 말한다.

## Acceptance Criteria

### AC1: 접속 상태 조회
- **설명**: 도구를 부르면 PocketAide 에 `GET /api/external/chat-presence` 한 건이 이 서버의
  ServiceAccount 토큰으로 나가고, PocketAide 가 답한 `present`·`last_seen_at`·`ttl_seconds` 가 그대로
  반환된다. 토큰은 호출마다 파일에서 다시 읽는다(kubelet 이 제자리 교체한다). 조회 대상은 PocketAide 의
  호출자 등록이 정하므로 요청에 사용자를 가리키는 값이 실리지 않는다.
- **달성 가치**: V5
- **검증 방법**: PocketAide 가 `{present: true, last_seen_at: 1800000000, ttl_seconds: 45}` 로 답하면
  도구 결과가 같은 값이고, 요청은 `GET /api/external/chat-presence` 한 건에
  `Authorization: Bearer <projected 토큰>` 이며 본문·질의 문자열이 없다.

### AC2: 「모름」과 「떠남」을 구분한다
- **설명**: `last_seen_at` 이 `null` 인 답(PocketAide 가 그 화면을 본 적 없음 — 예: 그 파드가 막 뜬 뒤)과
  `present: false` 에 시각이 있는 답(있었다가 떠남)이 서로 다른 결과로 돌아온다. 도구는 `null` 을 0 이나
  필드 누락으로 바꾸지 않는다 — 키는 언제나 있고 값만 `null` 이다.
- **달성 가치**: V3
- **검증 방법**: 두 응답을 각각 받으면 결과의 `last_seen_at` 이 `null` 과 숫자로 갈리고, `null` 쪽에도
  키가 존재한다.

### AC3: 인자를 받지 않는다
- **설명**: 입력 스키마에 속성이 없고(`additionalProperties: false`), 인자를 하나라도 실어 부르면
  PocketAide 에 닿기 전에 인자 오류로 거부된다 — 조용히 무시하지 않는다. 사용자를 지정해 남의 접속
  상태를 묻는 경로가 없고, 무시하면 「지정이 받아들여졌다」로 읽힌다.
- **달성 가치**: V3
- **검증 방법**: `{"user": "alice"}` 로 부르면 「takes no arguments」 인자 오류이고 PocketAide 요청이
  0건이다. `{}` 로 부르면 조회 1건이 나간다. `tools/list` 의 입력 스키마가 속성 0 ·
  `additionalProperties: false` 다.

### AC4: PocketAide 거부 사유의 구분 전달
- **설명**: PocketAide 가 이 서버를 호출자로 받지 않으면(401) 「호출자로 등록돼 있는지」를, 그 경로가
  없으면(404) 「그 엔드포인트가 있는 PocketAide 를 배포하라(PRD-12 AC22)」를 담은 도구 에러가 돌아온다.
  그 밖의 응답은 상태 코드를 담고, 읽을 수 없는 본문은 그렇게 말한다. 이 거부들은 미설정(AC5)과 다르게
  읽히고, **어느 것도 `present: false` 로 둔갑하지 않는다.**
- **달성 가치**: V3
- **검증 방법**: PocketAide 응답 401·404·500 마다 도구 에러 문구가 서로 다르고, 어느 것도 접속 상태
  결과로 돌아오지 않는다.

### AC5: 미설정 시 graceful 거부
- **설명**: `APPROVAL_GATE_URL` 이 없으면 unavailable 류 에러(`pocket-aide unavailable: …
  APPROVAL_GATE_URL`)를 반환하며, 서버 기동·다른 도구에는 영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 미설정 배포에서 호출하면 unavailable 도구 에러이고, 직후 `ping` 이 `pong` 이다.

## 결정 사항
- **PocketAide 주소·자격 증명은 알림 도구의 것을 그대로 쓴다.** 이 서버는 이미 PocketAide 에 SA
  호출자로 등록돼 있고 조회 엔드포인트는 같은 호출자 미들웨어 뒤에 있다. 별도 환경 변수를 두면 같은
  상대를 두 번 설정하게 되고 둘이 어긋날 수 있다(알림 도구의 같은 결정, 2026-10-25).
- **조회 대상은 인자가 아니다.** PocketAide 가 호출자 등록으로 대상을 정한다. 도구가 대상을 받으면
  에이전트가 남의 접속 상태를 캐물을 수 있는 표면이 생긴다 — 수신자를 인자로 받지 않는 알림 도구와
  같은 이유다.
- **접속 상태를 이 서버가 캐시하지 않는다.** 45초 창의 값을 캐시하면 「이미 떠난 사람이 아직 있다」고
  답할 수 있고, 그 오답이 바로 이 도구를 쓰는 이유(폰을 괜히 울리지 않기)를 깨뜨린다. 호출마다 묻는다.
- **TTL 을 이 서버가 정하지 않는다.** 창의 길이는 PocketAide 의 채팅 화면 폴링 간격에서 나오는 값이라
  (pocket-aide PRD-1 AC8) 그쪽이 정의하고, 이 도구는 받은 `ttl_seconds` 를 그대로 넘긴다. 양쪽에 상수를
  두면 PocketAide 가 간격을 바꿀 때 조용히 어긋난다.
- **인자 오류로 거부하고 무시하지 않는다.** 스키마가 `additionalProperties: false` 라도 서버가 한 번 더
  막는다 — 스키마를 보지 않는 클라이언트의 「사용자 지정」이 조용히 무시되면, 에이전트는 자기가 지정한
  사람의 상태를 받았다고 믿는다.
- **배포 순서: PocketAide 먼저.** 경로가 없는 PocketAide 는 404 를 답한다. 알림 도구의 `requester`
  때처럼 전부 실패하는 종류는 아니지만(이 도구만 실패한다), 404 를 미설정과 구분해 알려 운영자가
  무엇을 배포해야 하는지 알게 한다.

## 열린 질문
- 없음.
