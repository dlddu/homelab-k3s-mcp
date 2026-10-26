# PRD: pocket_aide_notification_send

PocketAide(`dlddu/pocket-aide`)를 거쳐 사용자의 폰으로 단순 푸시 알림을 보내는 도구.

## 달성 가치
- **V5: 클러스터 내부 앱 기능의 도구화** — 클러스터 안에서 도는 PocketAide 의 알림 엔드포인트
  (`POST /api/external/notifications`, pocket-aide PRD-12 AC16)를 도구로 열어, 에이전트가 긴 작업의
  완료·실패처럼 **결정이 필요 없는** 상태 변화를 사용자가 찾으러 오기 전에 폰으로 알린다.
  노출하는 것은 「알림 한 건 보내기」뿐이고 수신자는 인자가 아니다.
- **V3: 안전한 운영(Safe-by-default)** — 잘못된 입력은 PocketAide 에 닿기 전에 거부하고, PocketAide 의
  거부 사유(호출자 미등록·푸시 미설정)를 구분해 돌려주며, 미설정 시 graceful 거부가 기본값이다.
  PocketAide 가 남기는 알림 기록에는 등록 호출자(이 서버)와 함께 **이 도구를 부른 인증 주체**가
  실리고, 그 값은 서버가 인증에서 정하므로 도구 인자로 위조할 수 없다.

## 도구 개요
- 입력: `title`(필수 — 공백 제거 후 1~120자), `body`(선택)
- 동작: PocketAide 에 알림 한 건을 보낸다. 수신자는 PocketAide 에서 이 서버의 ServiceAccount
  (`homelab-k3s-mcp/homelab-k3s-mcp`)를 호출자로 등록한 사용자이고, 그 사용자의 등록 기기 전부에
  제목·본문이 **그대로** 도착한다. 본문에는 이 MCP 요청의 인증 주체가 `requester` 로 실리고(AC5),
  PocketAide 는 알림을 등록 호출자·`requester` 와 함께 그 사용자의 알림 기록에 남긴다(dlddu/pocket-aide
  PRD-12 AC16·AC17). 승인 요청은 만들지 않는다.
- 반환: `{devices, sent}` — 수신자의 등록 기기 수와 APNs 가 받은 수. `devices: 0` 은 오류가 아니라
  「아무에게도 닿지 않았다」는 사실이다.
- 서버 요구 설정: 승인 게이트와 같은 `APPROVAL_GATE_URL`(PocketAide 주소)과 projected 토큰
  (`APPROVAL_GATE_TOKEN_FILE`, 기본 `/var/run/secrets/pocket-aide/token`, audience `pocket-aide`).
  새 시크릿·네트워크 설정이 없다.
- 어노테이션: `readOnlyHint=false`, `destructiveHint=false`, `idempotentHint=false`,
  `openWorldHint=true`
  (idempotent=false: 같은 호출을 되풀이하면 알림이 그만큼 더 간다. destructive=false: 지우거나
  덮어쓰는 것이 없다)
- 승인 게이트: 대상 밖이다. PocketAide API 호출이지 쿠버네티스 권한을 행사하지 않으므로
  `prd-approval-gate` 의 정의 범위 밖이다(`session_write`·`opensearch_document_put` 과 같은 자리).
  알림은 사람의 판정 신호로 쓰이지 않아 `github_commit_status_create` 의 위조 우려도 없다.

> **구현 상태(2026-10-25)**: `internal/pocketaide` 와 `internal/mcp` 의 등록·디스패치가 이 PRD 와 같은
> 변경에서 착지했다. 상대 엔드포인트는 dlddu/pocket-aide#317 로 들어간다. 시나리오별 자동화 현황은
> `test-pocket-aide-notification-send.md` 의 각 「자동화」 필드가 말한다.
>
> **AC5(2026-10-27)**: `requester` 를 싣는 변경이 이 개정과 같은 PR 에서 착지했다. 상대는 PocketAide 의
> 알림 기록 변경(PRD-12 AC16 개정·AC17 신설)이고, **PocketAide 가 먼저 배포돼야 한다** — 그 전의
> PocketAide 는 모르는 필드를 400 으로 거부하므로(DisallowUnknownFields) 이 서버의 알림이 **전부**
> `PocketAide rejected the notification (400)` 으로 끝난다.

## Acceptance Criteria

### AC1: 알림 발송과 도달 수
- **설명**: 제목(과 본문)을 주면 PocketAide 에 알림 한 건이 이 서버의 ServiceAccount 토큰으로
  나가고, 결과로 PocketAide 가 돌려준 등록 기기 수·발송 수가 그대로 반환된다. 토큰은 호출마다 파일에서
  다시 읽는다(kubelet 이 제자리 교체한다). 앞뒤 공백은 잘라 보내고 빈 본문은 보내지 않는다.
- **달성 가치**: V5
- **검증 방법**: PocketAide 가 `{devices: 2, sent: 2}` 로 답하면 도구 결과가 같은 값이고, 요청은
  `POST /api/external/notifications` 한 건에 `Authorization: Bearer <projected 토큰>`, 본문
  `{title, body, requester}` 다(`requester` 는 AC5). 토큰 파일을 바꾸면 다음 호출이 새 토큰을 쓴다.

### AC2: 입력 검증이 PocketAide 보다 앞선다
- **설명**: `title` 이 없거나 공백뿐이거나 120자를 넘으면 PocketAide 에 요청을 보내기 전에
  「invalid notification」 도구 에러로 거부된다. 이 검증은 PocketAide 설정 여부와 무관하게 같다.
  `title`·`body` 가 문자열이 아니면 인자 오류다.
- **달성 가치**: V3
- **검증 방법**: 세 경우 모두 `isError` 도구 결과에 「invalid notification」이 있고, PocketAide 가
  받은 요청이 0건이다. 숫자 `title` 은 JSON-RPC 인자 오류다.

### AC3: PocketAide 거부 사유의 구분 전달
- **설명**: PocketAide 가 이 서버를 호출자로 받지 않으면(401) 「호출자로 등록돼 있는지」를, 푸시를
  보낼 수 없으면(503) 「APNs 미설정」을, 입력을 거부하면(400) 그 사유를 담은 도구 에러가 돌아온다.
  그 밖의 응답은 상태 코드를 담는다. 이 거부들은 미설정(AC4)과 다르게 읽힌다.
- **달성 가치**: V3
- **검증 방법**: PocketAide 응답 401·503·400·500 마다 도구 에러 문구가 서로 다르고, 어느 것도
  unavailable(미설정) 류가 아니다.

### AC4: 미설정 시 graceful 거부
- **설명**: `APPROVAL_GATE_URL` 이 없으면 unavailable 류 에러(`pocket-aide unavailable: …
  APPROVAL_GATE_URL`)를 반환하며, 서버 기동·다른 도구에는 영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 미설정 배포에서 호출하면 unavailable 도구 에러이고, 직후 `ping` 이 `pong` 이다.

### AC5: 알림은 도구를 부른 인증 주체를 `requester` 로 싣는다
- **설명**: 모든 알림 요청 본문에 `requester` 가 **항상** 실리고, 그 값은 이 MCP 요청을 인증한 주체를
  이벤트 기록(`prd-event-log` AC1)과 같은 철자로 쓴 것이다 — `jwt:<sub>`, `api_key:<n>`,
  `github_oidc:<owner/name/workflow>`, 인증을 끈 배포에서는 `anonymous`. 도구는 `requester` 인자를
  받지 않는다(스키마에 없고, 인자로 보내도 본문 값은 바뀌지 않는다). PocketAide 의 상한(200자)을 넘는
  주체는 거부하지 않고 앞 200자로 잘라 보낸다. PocketAide 는 이 값을 등록 호출자(이 서버의
  ServiceAccount)와 함께 알림 기록에 남긴다.
- **달성 가치**: V3, V5
- **검증 방법**: API 키 첫째·둘째로 각각 호출하면 PocketAide 가 받은 본문의 `requester` 가 `api_key:1`·
  `api_key:2` 이고, 인자에 `requester: "spoofed"` 를 넣어도 같다. JWT 주체는 `jwt:<sub>` 로 실린다.
  `tools/list` 의 입력 스키마 속성은 `title`·`body` 둘뿐이다. 200자를 넘는 주체는 앞 200자로 잘려
  나가고 요청은 성공한다.

## 결정 사항
- **PocketAide 주소·자격 증명은 승인 게이트의 것을 그대로 쓴다(2026-10-25).** 이 서버는 이미
  PocketAide 에 SA 호출자로 등록돼 있고 알림 엔드포인트는 같은 호출자 미들웨어 뒤에 있다. 별도 환경
  변수를 두면 같은 상대를 두 번 설정하게 되고 둘이 어긋날 수 있다.
- **수신자는 인자가 아니다.** PocketAide 가 호출자 등록으로 수신자를 정한다. 도구가 수신자를 받으면
  에이전트가 남에게 알림을 보낼 수 있는 표면이 생긴다.
- **`requester` 는 인증에서 정하고 인자로 받지 않는다(2026-10-27).** PocketAide 는 등록 호출자는
  토큰으로 알지만 그 호출자 뒤에서 **누가** 도구를 불렀는지는 모른다. 그 값이 도구 인자라면 에이전트가
  아무 이름이나 적을 수 있어 기록이 증거가 되지 못한다 — 그래서 서버가 이벤트 기록과 같은 주체 문자열을
  넣는다. 자격 증명 자체는 그 문자열에 들어가지 않는다(`prd-event-log` AC3).
- **상한을 넘는 주체는 자른다.** 주체 문자열은 이 서버가 만들고 에이전트가 고칠 수 없으므로, 거부하면
  그 호출자는 알림을 영영 보낼 수 없다. 기록의 식별력을 조금 잃더라도 알림이 가는 쪽을 택했다.
- **배포 순서: PocketAide 먼저(2026-10-27).** PocketAide 의 디코더는 모르는 필드를 400 으로 거부한다.
  `requester` 를 받는 PocketAide 가 먼저 나가지 않으면 이 서버의 알림이 전부 실패한다. PocketAide 는
  1단계에서 `requester` 를 선택 필드로 받고, 이 서버가 배포된 뒤 2단계에서 필수로 바꿀 예정이다.
- **제목 규칙은 도구가 먼저 적용한다.** PocketAide 도 같은 규칙(120자)으로 400 을 내지만, 미설정
  배포에서도 같은 입력에 같은 거부가 나와야 에이전트가 입력 오류와 환경 문제를 구분한다.

## 열린 질문
- 없음.
