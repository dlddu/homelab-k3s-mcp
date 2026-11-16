# 테스트 문서: session_tool_approvals · session_tool_approval_decide

## 검증 대상 AC
- AC1: 기다리는 호출을 모델이 만든 모습 그대로 읽는다 (PRD: session_tool_approvals)
- AC2: 결정은 그 호출·그 실행에 묶여 전달된다 (PRD: session_tool_approvals)
- AC3: 결속 없는 결정은 제어면에 닿지 않는다 (PRD: session_tool_approvals)
- AC4: 거부 응답의 구분 전달 (PRD: session_tool_approvals)
- AC5: 미설정 시 graceful 거부 (PRD: session_tool_approvals)
- AC6: 조회는 수동적이고 결정은 파괴적이라고 표기한다 (PRD: session_tool_approvals)

> **자동화 상태**: 도구 계층은 구현·검증됐다 — 아래 "자동화" 필드의 Go 단위는 실재하는 테스트
> 이름이다. 통합 e2e는 모두 `(미작성)`이고, 그 표기는 `tests/integration/check_ac_mapping.py`가
> 실측 파일 집합과 대조해 CI에서 강제한다. 잔여 공백과 그 **선행 조건**은
> `docs/doc-tracker/`의 AC ↔ e2e 1:1 렌즈가 단일 사실 원천이다 — 여기에 개수를 다시 적지 않는다.
> 이 흐름의 e2e에 필요한 선행은 하나로 모인다: **두 번째 행위자**. 턴이 호출을 올려 둔 동안
> 소유자로서 제어면을 부르는 단계가 하네스에 있어야 하고, 모델이 자기 호출을 승인하지 못하는
> 것이 이 기능의 요점이라 한 행위자만으로는 밟을 수 없다.

## 테스트 시나리오

### 시나리오 1: 기다리는 호출을 읽는다
- **사전 조건**: `toolApprovals: true`로 write된 턴이 도구 호출 하나를 올려 둔 `claude-code` 세션
- **실행 단계**: `session_tool_approvals`를 부른다
- **기대 결과**: 응답에 `epoch`와 호출 하나가 있고, 그 항목의 `tool`·`input`(모델이 요청한 인자
  그대로)·`argsHash`·`toolUseId`·`epoch`·`expiresAt`이 제어면이 보고한 것과 같다. 인자를 고쳐
  보낼 수 있는 입력은 어디에도 없다.
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/server/session_tool_approvals_test.go`
  (`TestSessionToolApprovalsListsWhatWaits` — 항목의 여섯 칸과 응답의 `epoch`를 모두 대조한다) +
  통합 `(미작성)`

### 시나리오 2: 결정이 세 결속과 함께 전달된다
- **사전 조건**: 시나리오 1의 세션
- **실행 단계**: 목록의 `id`·`argsHash`와 응답의 `epoch`로 `approve`를 보내고, 다른 호출에
  `deny`를 `note`와 함께 보낸다
- **기대 결과**: 제어면이 받은 본문의 `decision`·`argsHash`·`epoch`·`reason`이 호출 인자와 같고,
  두 결정 모두 성공으로 답한다. 도구 인자 이름은 `note`이고 제어면 본문 필드는 `reason`이다.
- **검증 AC**: AC2
- **자동화**: Go 단위 `internal/server/session_tool_approvals_test.go`
  (`TestSessionToolApprovalDecidePassesTheBindingsThrough` — 승인과 거절 두 번의 전달을 구조체
  비교로 본다) + 통합 `(미작성)`

### 시나리오 3: 결속을 적지 않은 결정은 제어면에 닿지 않는다
- **사전 조건**: 없음(도구 계층만으로 밟는다)
- **실행 단계**: `id`·`approvalId`·`decision`·`argsHash`·`epoch`를 하나씩 뺀 호출과
  `decision: "allow"`, 문자열이 아닌 `epoch`를 보낸다
- **기대 결과**: 모두 `-32602` 인자 오류이고 제어면 요청은 0건이다. `tools/list`의 `required`가
  정확히 그 다섯이며 `note`만 선택이고, `decision`은 `enum: [approve, deny]`다.
- **검증 AC**: AC3
- **자동화**: Go 단위 `internal/server/session_tool_approvals_test.go`
  (`TestSessionToolApprovalDecideRejectsUnboundDecisions` — 거부마다 제어면 호출 0건을 함께
  단언한다, `TestSessionToolApprovalToolsAreDeclared` — 스키마의 `required`와 `enum`) +
  통합 `(미작성)`

### 시나리오 4: 거부가 구분되어 전달된다
- **사전 조건**: 제어면이 404·409·422를 돌려주는 상황
- **실행 단계**: 더 이상 기다리지 않는 호출, 다른 호출·이전 실행을 가리키는 결정,
  `claude-code`가 아닌 세션에 대해 각각 부른다
- **기대 결과**: 각각 not found(재시도 무의미)·conflict(목록을 다시 읽어야 한다)·
  invalid argument로 구분되고, 제어면이 거부한 결정이 성공으로 보이지 않는다.
- **검증 AC**: AC4
- **자동화**: Go 단위 `internal/sessionplatform/tool_approvals_test.go`
  (`TestDecideToolApprovalDistinguishesRefusals`·`TestToolApprovalsDistinguishesRefusals` —
  상태 코드별 에러 종류) + `internal/server/session_tool_approvals_test.go`
  (`TestSessionToolApprovalDecideSurfacesRefusals` — 거부가 도구 에러로 보인다) + 통합 `(미작성)`

### 시나리오 5: 미설정 시 두 도구가 거부한다
- **사전 조건**: `SESSION_PLATFORM_ENDPOINT` 없음
- **실행 단계**: 두 도구를 각각 부른다
- **기대 결과**: 둘 다 unavailable 류 도구 에러이고 「기다리는 것이 없다」로 보이지 않으며,
  서버와 다른 도구는 계속 동작한다.
- **검증 AC**: AC5
- **자동화**: Go 단위 `internal/server/session_tool_approvals_test.go`
  (`TestSessionToolApprovalsNeedTheControlPlane`) + 통합 `(미작성)`

### 시나리오 6: 표면의 표기
- **사전 조건**: 없음
- **실행 단계**: `tools/list`를 읽는다
- **기대 결과**: `session_tool_approvals`는 `readOnlyHint=true`·`idempotentHint=true`,
  `session_tool_approval_decide`는 `readOnlyHint=false`·`destructiveHint=true`·
  `idempotentHint=false`다.
- **검증 AC**: AC6
- **자동화**: Go 단위 `internal/server/session_tool_approvals_test.go`
  (`TestSessionToolApprovalToolsAreDeclared`) + 통합 `(미작성)`
