# PRD: session_tool_approvals · session_tool_approval_decide

`claude-code` 세션의 **실행 중인 턴**이 세션 소유자에게 올린 도구 호출을 읽고, 그중 하나를
승인·거절하는 도구 둘.

세션 쪽의 계약은 session-platform `docs/prd/claude-code-workload.md` AC-E2
「턴 중 도구 승인」이 갖는다. `toolApprovals: true`로 queue된 턴은 정책이 허용하는 도구 호출마다
그 턴 안에서 소유자의 결정을 기다리고, 승인은 모델이 만든 인자를 그대로 돌리고 거절은 사유와
함께 거부한다. 이 PRD는 그 결정을 **부모 대화에서** 하게 여는 몫만 갖는다 —
`session_tool_approvals`가 무엇이 기다리는지 읽고, `session_tool_approval_decide`가 그중
하나를 결정한다.

## 달성 가치
- **V5: 클러스터 내부 앱 기능의 도구화** — 제어면의 `GET /api/v1/sessions/{id}/tool-approvals`와
  `POST …/{approvalId}/decide`를 도구로 열어, 세션에 맡긴 작업의 위험한 한 걸음을 대화에서
  판단하고 그 턴을 이어가게 한다.
- **V3: 안전한 운영(Safe-by-default)** — 결정은 자신이 선 **호출 하나**에만 묶인다. 세 결속
  (요청 id·인자 해시·실행 epoch) 중 하나라도 어긋나면 결정이 아니고, 결속을 다 적지 않은
  호출은 제어면에 **닿지 않는다**. 정책이 거부한 호출은 애초에 여기 오지 않으므로 이 도구는
  거부를 승인으로 바꾸는 경로가 아니다.

## 도구 개요
- `session_tool_approvals`
  - 입력: `id`(필수 — 세션 id)
  - 동작: 제어면에 기다리는 호출 목록을 묻는다. **수동적 읽기**다 — `session_list`처럼 유휴
    세션을 승격하지 않고, 스냅샷을 복원하지 않으며, 접근으로 세지 않는다.
  - 반환: `epoch`(그 호출들을 올린 **실행**의 이름)와 `approvals`(오래된 것부터). 항목마다
    `id`·`tool`·`input`(모델이 요청한 인자 그대로)·`argsHash`·`toolUseId`·`epoch`·`askedAt`·
    `expiresAt`.
  - 어노테이션: `readOnlyHint=true`, `destructiveHint=false`, `idempotentHint=true`,
    `openWorldHint=true`
- `session_tool_approval_decide`
  - 입력: `id`·`approvalId`·`decision`(`approve`|`deny`)·`argsHash`·`epoch` (모두 필수),
    `note`(선택)
  - 동작: 그 호출 하나에 대한 결정을 제어면에 전달한다. **접근으로 센다**(write와 같다).
  - 반환: 결정한 `id`와 `decision`
  - 어노테이션: `readOnlyHint=false`, `destructiveHint=true`, `idempotentHint=false`,
    `openWorldHint=true`
    (destructive=true: 승인은 세션 안에서 그 도구 호출을 실제로 실행시킨다.
    idempotent=false: 같은 결정의 재전송은 이미 결정된 호출에 not found로 답한다)
- 서버 요구 설정: `SESSION_PLATFORM_ENDPOINT` (`session_list`와 공유)

> **구현 상태(2026-11-16)**: 도구 계층이 구현됐다 — `internal/sessionplatform/tool_approvals.go`
> (제어면 클라이언트)와 `internal/mcp/session_tool_approvals.go`(등록·디스패치)이며, AC1~AC6이
> Go 단위로 검증된다. 통합 e2e는 미작성이고 그 축의 소유자는 AC ↔ e2e 1:1 렌즈다
> (`docs/doc-tracker/`) — 이 흐름의 e2e 는 **두 번째 행위자**(턴을 띄운 채 소유자로서 결정하는
> 쪽)가 하네스에 서야 밟을 수 있다.

## Acceptance Criteria

### AC1: 기다리는 호출을 모델이 만든 모습 그대로 읽는다
- **설명**: `session_tool_approvals`는 그 세션의 실행 중인 턴이 올린 호출을 도구 이름과 **모델이
  요청한 인자 그대로** 돌려준다. 인자를 고쳐 보내는 경로는 없다 — 승인은 읽은 그 호출을 그대로
  돌리거나 돌리지 않는다. 각 항목의 `argsHash`와 응답의 `epoch`가 AC2의 결속에 쓰인다. 기다리는
  호출이 없거나 세션에 살아 있는 파드가 없으면 빈 목록이다.
- **달성 가치**: V5
- **검증 방법**: 제어면이 호출 하나를 보고하는 상황에서 `tool`·`input`·`argsHash`·`toolUseId`·
  `epoch`·`expiresAt`이 그대로 실리고, 응답의 `epoch`가 목록의 것과 같음을 확인한다.

### AC2: 결정은 그 호출·그 실행에 묶여 전달된다
- **설명**: `session_tool_approval_decide`는 `approvalId`·`argsHash`·`epoch`를 **받은 그대로**
  제어면 요청 본문에 실어 보낸다. 세 결속이 결정을 호출 하나에 묶는 수단이고, 어긋남의 판정은
  이 서버가 갖지 않고 제어면과 세션 에이전트가 갖는다. `note`는 세션 기록에 남고 거절에서는
  모델이 도구 결과로 받는다. 제어면 본문의 필드명은 `reason`이지만 **도구 인자는 `note`**다 —
  이 서버는 `reason`을 승인 게이트 전용 인자로 예약하고 있고(`internal/mcp/provenance.go`),
  그 이름을 쓰면 승인을 요구하지 않는 도구가 승인 인자를 광고하는 모순이 된다.
- **달성 가치**: V5
- **검증 방법**: 승인·거절 각각에서 제어면이 받은 본문의 `decision`·`argsHash`·`epoch`·`reason`이
  호출 인자와 같음을 확인한다.

### AC3: 결속 없는 결정은 제어면에 닿지 않는다
- **설명**: `id`·`approvalId`·`decision`·`argsHash`·`epoch` 중 하나라도 없거나, `decision`이
  `approve`·`deny` 밖이면 제어면에 **보내기 전에** 인자 오류(`-32602`)로 거부한다. 결속을 반쯤
  적은 결정이 「지금 기다리는 아무 호출」에 적용되는 일을 구조적으로 막는 것이 이 AC다.
  `tools/list`의 입력 스키마도 그 다섯을 `required`로 광고하고 `note`만 선택이다.
- **달성 가치**: V3
- **검증 방법**: 다섯 인자를 하나씩 뺀 호출과 `decision: "allow"`가 모두 `-32602`이고 제어면
  요청이 0건임을 확인한다. `tools/list`의 `required`가 정확히 그 다섯이고 `decision`이
  `enum: [approve, deny]`임을 확인한다.

### AC4: 거부 응답의 구분 전달
- **설명**: 제어면의 거부가 호출자에게 구분되어 전달된다 — 없는 세션·더 이상 기다리지 않는
  호출(이미 결정됨·턴 종료·창 경과)은 not found로 **재시도가 의미 없고**, 결정이 다른 호출이나
  이전 실행을 가리키면 conflict로 **목록을 다시 읽어야 한다**는 뜻이며, `claude-code`가 아닌
  타입은 invalid argument다.
- **달성 가치**: V3
- **검증 방법**: 제어면이 404·409·422를 돌려주는 각 상황에서 서로 구분되는 도구 에러가 되고,
  제어면이 거부한 결정이 성공으로 보이지 않음을 확인한다.

### AC5: 미설정 시 graceful 거부
- **설명**: `SESSION_PLATFORM_ENDPOINT`가 없으면 두 도구 모두 unavailable 류 에러를 반환하며,
  「기다리는 것이 없다」로 보이지 않는다. 서버 기동과 다른 도구에는 영향이 없다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 두 도구가 도구 에러를 반환하고 서버는 계속 동작한다.

### AC6: 조회는 수동적이고 결정은 파괴적이라고 표기한다
- **설명**: `tools/list`에서 `session_tool_approvals`는 `readOnlyHint=true`·`idempotentHint=true`로,
  `session_tool_approval_decide`는 `readOnlyHint=false`·`destructiveHint=true`·
  `idempotentHint=false`로 광고된다. 승인은 세션 안에서 그 도구 호출을 실제로 실행시키므로
  되돌릴 수 없는 부수 효과를 낼 수 있고, 같은 결정의 재전송은 같은 결과가 아니다.
- **달성 가치**: V3
- **검증 방법**: `tools/list` 응답의 두 어노테이션 묶음이 위와 같음을 확인한다.
