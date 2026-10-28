# 2026-10-28 필요성 판정 첫 슬라이스 (`rct_20261028-0003`)

2026-10-28 필요성 개정(#293) 뒤 첫 판정 슬라이스. 개정 전 원장은 전 행 미판정(`—`)으로 되돌려졌고, 이 패스는
그 가운데 #279~#290 창이 들인 신규 행 전부와 같은 계열(PocketAide 승인 게이트·인증) 행을 한 슬라이스로 가져왔다.
판정 대상 줄 주석 377줄 · docstring 8줄, 행 10개. 열린 PR 과 겹치는 파일은 없었다.

판정 기준은 `README.md` 「판정 절차」다 — 기계가 읽는가 → 관례 최소 doc 인가 → 필요성 시험(지우면 다음 편집자가
틀린 판단을 하는가, 결과를 한 문장으로 적는다) → 더 나은 자리 → 주석끼리 중복. 비주석 diff 는 없다.

## 제거 (줄 주석 59줄)

| 범위 | 줄 | 걷은 것 | 유형 |
| --- | ---: | --- | --- |
| `internal/mcp/mcp.go` · `toolslist.go` | 104 → 87 | `prd-*` AC 번호 지시(`gate` 필드·`events` 필드·`gateWait`·`toolsCall`·`askGate`·`outcome`·`recordTarget`·`afterVerdict` 등 12곳), `gateOf`·`noteGate`·`extractArguments`·`sessionRead` 의 이름 재진술 doc, `rpcErr` 의 「JSON-RPC 오류는 핸들러에 닿지 않는다」(정본은 `outcome` doc), `gate` 필드 doc, 구획선 `--- tool implementations ---`·`--- shared helpers ---`, `toolsListJSON` 의 「리터럴로 둔 이유」 | 저장소 문서 재진술 · 코드 재진술 · 주석끼리의 중복 · 장식 |
| `internal/mcp/provenance_test.go` | 21 → 14 | 테스트 이름을 산문으로 옮긴 doc 셋 | 코드 재진술 |
| `internal/sessionplatform/workload_types*.go` | 19 → 8 | `configPath` 의 경위(#153·minimax-code), `checkWorkloadType`·`offeredWorkloadTypes` 의 이름 재진술, 테스트의 `configStub` 설명·이름 재진술 doc 셋(`shellWorkloadType` doc 이 정본인 「shell 은 조회가 필요 없다」 사본 포함) | PR 이력 · 코드 재진술 · 주석끼리의 중복 |
| `tests/k8s/kind/pocket-aide-dex.yaml` | 11 → 10 | 파일 이름이 말하는 첫 문장 | 코드 재진술 |
| `tests/k8s/kind/pocket-aide-fixture.yaml` | 52 → 45 | 파일 첫머리의 `docs/test-approval-gate.md` 가리킴과 같은 말을 되풀이한 제목 | 저장소 문서 재진술 |
| `tests/k8s/kind/pocket-aide-injector.yaml` | 75 → 71 | 코드(`residue` 검사)가 말하는 프로브 롤백 단언 설명 | 코드 재진술 |
| `internal/auth/auth.go` · `auth_test.go` | 51 → 39 | `configureOAuth`·`parseAPIKeys`·`verify`·`matchAPIKey`·`recordingSink` 의 이름 재진술, `prd-*` AC 번호, 테스트 구획선 다섯 | 코드 재진술 · 저장소 문서 재진술 · 장식 |
| `internal/pocketaide/pocketaide.go` | 23 → 23 | `Unavailable` doc 끝의 `(prd-event-log AC4)` 만 걷음(줄 수 불변, 지문 변경) | 저장소 문서 재진술 |
| `tests/k8s/kind/approval-variant.yaml` · `gate-broken-variant.yaml` | (위 두 행) | 이 파일을 쓰는 시나리오 목록, 「더 짧은 경로가 덜 깨진다」, 「Service 를 안 쓴 이유」(검토하고 버린 대안), PRD 속성(AC5) 지목 | 소감 · PR 이력 · 저장소 문서 재진술 |

docstring: `pocket_aide_notification_send_sc4.py` 의 함수 docstring 3줄(AC 번호와 테스트 이름 재진술)을 걷었다(8 → 5).

## 유지 (사유 한 문장)

기계가 읽는 주석(`검증 AC:`·`mock-exception:`·`# noqa`)과 Go 관례 최소 doc(패키지 주석·exported 식별자 1줄)은 사유를 따로 대지 않는다. 아래는 필요성 시험으로 남긴 것이다.

### `internal/mcp/mcp.go`
- `defaultSensitiveKind` — 지우면 `RESOURCE_GATED_KINDS` 기본값을 바꾸는 사람이 이 상수를 같이 바꿔야 한다는 것을 놓쳐 둘이 어긋난다.
- `gate`·`gateReader`·`gateCollectionReader` 의 「never nil」 — 지우면 nil 검사를 더하거나 nil 을 허용하는 쪽으로 고쳐 미설정 배포가 거부 대신 패닉한다.
- `registry` — 지우면 패키지 전역으로 되돌려 프로덕션에 없는 도구로 디스패치하는 테스트가 불가능해진다.
- `events` 의 「nil 은 eventlog.Log」 — 지우면 nil 이 기록 끔으로 읽힌다.
- `WithGateReader`·`WithGateCollectionReader` — 한 옵션으로 합치면 한쪽만 빠진 배포가 조용히 게이트 설명 없이 통과한다는 실패 모드.
- `reasonOf` — 기본값이 `gate_unconfigured` 인 까닭과 `-32602` 만 입력 오류라는 분류 근거가 코드만으로는 안 보인다.
- `afterVerdict`·`gateRefusal` — 백엔드가 승인한 뒤의 거부와 타입 없는 오류의 의미(unreachable)가 코드 형태만으로는 안 보인다.
- `callNote`·`gateWait` — 컨텍스트로 실어 나르는 이유와 대기 시간을 호출 시간에서 뺀다는 계약.
- `toolsCall`·「게이트는 여기서」·`askGate`·`outcome` — 기록·게이트를 핸들러가 아닌 한 곳에 둔 이유(핸들러가 잊을 수 있다)와 JSON-RPC 오류가 항상 거부라는 정본 명제(`rpcErr` 사본은 걷음).
- `recordTarget` 의 「그 네 필드만」 — 지우면 인자 전체를 기록에 싣게 된다.
- `sessionList` 의 빈 목록 — 「세션 없음」과 「컨트롤 플레인 도달 불가」를 가르는 근거.
- `sessionFields` 의 `pod` 생략 — 지우면 빈 이름을 파드가 있는 것처럼 보고한다.
- `sessionWrite` — 요청 전에 거부해 세션이 건드려지지 않았다는 구조적 보장.
- `toolError` — 응답은 같아도 기록은 다른 이유(미설정은 오류가 아니라 거부).

### `internal/mcp/provenance.go` · `provenance_test.go`
- `approvalNeed` — 프로브가 실패하면 「항상 묻는다」로 보는 이유(새 도구가 reason 을 요구하는 쪽으로 틀리게 한다).
- `requireReason` — 모든 게이트 경로 앞이라 거부가 승인 요청·사전 읽기를 쓰지 않는다는 순서 보장.
- `splitReason` — 맵 왕복을 피한 이유(승인 화면에 원문 바이트를 그대로 보인다).
- 테스트 `TestStrictHandlersNeverSeeReason` — `resource_create` 핸들러가 `manifest` 외 인자를 거부하므로 reason 이 새면 깨지는 도구라는 것.

### `internal/sessionplatform/workload_types.go`
- `configPath` — 지우면 목록 사본을 다시 들여 플랫폼이 유형을 더할 때마다 어긋난다.
- `shellWorkloadType` — 기본값이고 모든 배포가 제공해 조회가 필요 없다는 근거.
- 옛 컨트롤 플레인 — 목록을 안 주면 400 이 판정을 대신한다는 것(지우면 이 경로를 지워도 되는 폴백으로 오해).

### `internal/pocketaide/pocketaide.go`
- 전부 Go 관례 최소 doc(패키지·exported 1줄) 또는 사유가 있는 본문: `Service` 의 `requester` 의미(호출자가 쓴 값이며 PocketAide 가 그 호출자 옆에 기록), `New` 의 매 호출 토큰 읽기(kubelet 이 투영 토큰을 제자리 교체), `FromEnv` 의 승인 게이트 자격증명 재사용, `ValidateTitle` 의 설정 여부와 무관한 동일 거부.

### `tests/k8s/kind/` 픽스처
- `pocket-aide-dex.yaml` — 인스턴스를 따로 둔 이유(전역 `idTokens` 20초 값·롤아웃 순서), password grant, 발급자·audience 가 `pocket-aide-fixture.yaml` 과 글자까지 같아야 한다는 결합, bcrypt 해시의 출처, 사용자 id 결합.
- `pocket-aide-fixture.yaml` — 소스에서 빌드한 이유(`POCKET_AIDE_SHA` 계약), SA 가 `auth-delegator` 에 묶이지 않으면 503 이 되는 실패 모드, 기록 프록시가 관측면이며 토큰을 남기지 않는다는 것, Recreate·복제 1 의 SQLite 불변식, 푸시가 꺼진 채 뜬다는 한계, 설정 결합 두 곳.
- `approval-variant.yaml` — env 가 배포당이라 변형이 필요한 세 이유, 소유권으로 격리되는 자동 응답 모드, ClusterRoleBinding 이 따로인 이유, `RESOURCE_GATED_KINDS` 가 목록을 대체하므로 `v1/Secret` 을 빼면 조용히 게이트가 풀린다는 것. `등재:` 줄은 모킹 정책 등재 표지라 그대로 둔다.
- `pocket-aide-injector.yaml` — 「PocketAide 스텁이 아니다」(실제 409·500 분기를 만드는 선행조건만 심는다는 판정 근거), 별도 배포인 이유(단일 노드 RWO), SQLite 동시성·`mode=rw`, uid 65532, NOT NULL 컬럼은 프로브로 검증, Recreate 의 이유.
- `gate-broken-variant.yaml` — 변형이 따로여야 하는 이유(공유 네임스페이스 재시작은 파일 간 경쟁), 각 변형이 어느 `FromEnv` 분기를 겨누는지와 거부가 `Describe` 호출 없이 일어난다는 것, 호출자로 등록되지 않은 이유.

### `internal/auth/auth.go` · `auth_test.go` · `github_oidc.go` · `internal/toolscope/toolscope.go`
- `maxRecordedBody` — 접두부로는 이름을 신뢰할 수 없고 미인증 호출자를 위해 더 읽지 않는다는 근거.
- `apiKeyIndex` — 조기 반환 없이 비교 횟수로 어느 키가 맞는지 새지 않게 하는 상수 시간 근거.
- `verify` 안 「API 키 전용 모드」 — 지우면 nil JWKS 를 가져오려는 경로가 생긴다.
- `recordRefusal`·`refusedToolName`·`RecordTo` — `initialize`·`tools/list` 는 도구 호출이 아니라 기록하지 않는다는 경계, 요청 봉투를 두 번째 프로토콜 파서로 만들지 않는다는 것, 두 계층이 한 sink 를 공유해야 지표가 모든 거부를 본다는 것.
- `withPrincipal` — 자격증명 자체가 따라 내려가지 않는 이유(`eventlog.Principal` 이 보관).
- `oidcServer` — 라이브 발급자 없이 `FromEnv` 의 discovery 를 끝내려는 용도.
- `github_oidc.go` 의 저장소 소문자화 — GitHub 은 저장소 이름을 대소문자 무시로 다루고 클레임은 만든 대로의 대소문자를 싣는다.
- `toolscope` — 패키지 doc 의 「서로 import 할 수 없어 여기에 둔다」와 exported 1줄 doc 들(관례 최소).
