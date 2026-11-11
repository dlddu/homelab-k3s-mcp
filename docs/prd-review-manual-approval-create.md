# PRD(제안·미착지): review_manual_approval_create

> **상태: 제안(RFC).** 이 문서는 구현 전 설계 질문을 소유자(`dlddu`)에게 올리기 위한 초안이다.
> `docs/review-policy.md`(MA2) · `internal/approvalgate`·`internal/auth`(MA3)에 걸쳐 있어, 이
> 문서 자체와 뒤따르는 구현 PR은 저장소의 사람 승인 대상이다. 이 PR은 코드를 포함하지 않는다 —
> 아래 「열린 질문」이 정리되기 전에 구현하면 2026-10-23/24 에 겪은 것과 같은 되돌림을 반복할
> 위험이 있다(「직접 관련 선례」 참고).

터미널이 없는 사용자가 관리용 채팅(PocketAide)에서 PR 수동 승인(`review/manual-approval`)을
본인 승인으로 완료할 수 있게 하는 도구.

## 배경

`docs/review-policy.md`가 정의하는 현재의 사람 승인 경로는 둘뿐이다:

1. 저장소 소유자가 직접 `gh api -X POST .../statuses/<sha>`를 친다.
2. 사람이 있는 Claude Code 세션에서 `reconciler-manual-approval` 스킬(사람 세션 전용)이 대화로
   설명하고, 사람이 "승인"이라고 답하면 `github_commit_status_create`를 호출한다.

둘 다 **터미널(세션)이 있는 사람**을 전제한다. 터미널 없이 PocketAide(폰 알림 승인) 화면만
보는 사용자는 이 두 경로 어디에도 들어가지 못한다 — 이미 클러스터 쓰기 승인에 쓰는 PocketAide를
PR 리뷰 승인에도 쓰자는 것이 이 제안이다.

### 직접 관련 선례 — 2026-10-23/24 에 겪은 되돌림

`github_commit_status_create`를 PocketAide 승인 게이트에 태우는 시도가 **이미 있었고 하루 만에
되돌려졌다**:

- #281(2026-10-23, `feat(commit-status): github_commit_status_create 에 승인 게이트`) — 이 도구가
  승인 게이트를 타게 했다. PR 본문이 스스로 적은 운영 영향: *"이 도구를 무인으로 부르던 흐름(예:
  reconciler 리뷰 단계의 `review/` status 기록)은 이제 PocketAide 승인을 기다리고... **수동 승인
  스킬은 사람이 PocketAide 에서 한 번 더 승인하게 된다**."*
- #283(2026-10-24, `revert(commit-status): ... #281 되돌림`) — 하루 뒤 소유자 결정으로 되돌렸다.
  되돌림 PR 자체는 "위조 방지는 허용 접두사가 맡는다"로만 적혀 있고, 되돌린 **이유**(이중 승인
  마찰인지, PocketAide 화면이 리뷰에 필요한 설명을 못 싣는 것인지, 다른 이유인지)는 어느 PR에도
  글로 남아 있지 않다(두 PR 모두 댓글 0건 — 소유자가 로컬에서 직접 승인·머지).

이 제안은 **그 되돌림과 범위가 다르다**: #281은 기존 `github_commit_status_create`를 범용으로
게이트에 태워, 이미 사람이 세션에서 판단을 마친 reconciler-manual-approval 경로에까지 중복
승인을 강제했다. 이 제안은 **신규 전용 도구**이고, 터미널이 전혀 없어 PocketAide가 유일한
판단 화면인 경우에만 쓴다 — 세션이 있는 경로(소유자 직접·reconciler 스킬)는 그대로 둔다.
그럼에도 "PocketAide 화면이 PR 리뷰 판단에 필요한 설명을 실을 수 있는가"라는, 되돌림의 추정
이유 중 하나가 이 제안에도 똑같이 걸린다 — 「열린 질문 1」.

## 달성 가치
- **V3: 안전한 운영(Safe-by-default)** — 식별 결합 없는 자유문 "승인"을 승인으로 인정하지 않고,
  설정이 비면 전부 거부하고, 승인 뒤 head가 바뀌면 그 승인을 무효로 재판정한다.
- **V2: 단명·최소권한 자격증명** — 기존 `github_commit_status_create`·`github_app_installation_token`
  과 같은 토큰 비노출·서버 내부 발급 원칙을 그대로 따른다. 새 토큰 종류를 만들지 않는다.

## 요청 요구사항 → 설계 매핑

| # | 요구사항 | 이 설계의 답 |
|---|---|---|
| 1 | 기존 코드·정책 조사 | 이 문서의 「배경」·「직접 관련 선례」. 조사 대상(`github_commit_status_create`·`approvalgate`·`review-policy.md`·`reconciler-manual-approval` 스킬)의 실제 코드를 읽고 인용했다 |
| 2 | 채팅에서 repo/PR/head/diff·MA 사유를 검토 가능하게 제시 | `approvalgate.Call.Describe`가 PocketAide에 보내는 승인 화면 텍스트를 만든다 — repo·PR 번호·`expectedHeadSha`·걸린 MA 케이스 ID(호출자가 실행한 판정기 결과를 인자로 받아 그대로 싣는다; 이 도구는 판정기를 server 안에서 다시 돌리지 않는다)를 사람이 읽는 문장으로 렌더링 |
| 3 | `approver=dlddu`·`approved=true` 같은 자유문 금지, 서명된/신뢰 경로만 인정 | 승인 판정은 PocketAide의 등록된 호출자·기기 식별(`approvalgate.Decision`)에서만 나온다. 도구 인자에 "approved" 같은 불 값은 받지 않는다 — `repository`·`pullNumber`·`expectedHeadSha`·`context`만 받고, 승인 여부는 오직 `approvalgate.Gate.Authorize`의 반환값이다 |
| 4 | 대상 고정, head 변경 시 거절·재검토, 무권한/타 작업자 요청 거절, replay 안전 | **승인 요청 시점**과 **status 기록 시점** 둘 다 PR의 현재 head를 GitHub에서 다시 읽어 `expectedHeadSha`와 비교한다(뒤 「AC4」). 둘 중 하나라도 어긋나면 거부·재검토 안내. `approvalgate.Decision.Consume()`이 1승인-1실행을 강제해 재전송에 안전(기존 패턴 재사용). "무권한/타 작업자"는 PocketAide 자체가 등록된 캐릭터이므로, 이 서버가 호출자를 추가로 구분하지 않는다 — PocketAide 쪽 캐릭터·기기 등록이 유일한 신원 경계다(「열린 질문 2」) |
| 5 | 승인 주체·대상 SHA·시각·근거/요청 ID 감사, 비밀 비저장 | 디스패처의 기존 `eventlog.Record`(모든 `tools/call`에 자동 적용 — `Principal`·`Result`·`Gate{RequestID, Decision}`)에 더해, 핸들러가 `slog`로 repo·PR·sha·context·PocketAide RequestID를 한 줄 남긴다. 토큰·PocketAide 내부 식별자 외에는 비밀이 없다 |
| 6 | 전용 도구는 review/manual-approval 한정·repo allowlist·최소 `statuses:write` 서버 내부 토큰, 임의 context/성공 상태 작성기 금지 | 입력에 `state`를 받지 않는다 — **항상 `success`만**, **`context`는 `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES`로 좁힌 접두사 중 하나로 호출자가 지정**(기존 `github_commit_status_create`의 AC4를 그대로 재사용 — 새 allowlist를 만들지 않는다). 내부적으로 `github.Service.CreateCommitStatus`를 그대로 호출해 토큰 발급·폐기 경로를 중복하지 않는다 |
| 7 | 소유자 직접승인 정책과 충돌 시 명시, 정식 위임정책 변경안 준비, 실제 반영 전 새 경로로 기존 승인 금지 | 바로 이 문서. `docs/review-policy.md`의 "승인할 수 있는 사람: 저장소 소유자(`dlddu`)"·"사람의 지시로 도는 승인 전용 도구: ...(사람 세션 전용)" 문장은 이 제안이 **추가하는** 제3의 경로를 아직 인정하지 않는다. 소유자가 이 PRD를 승인하면, `review-policy.md`의 해당 행에 "PocketAide 등록 캐릭터의 터미널 없는 승인"을 제3의 승인 주체로 명시적으로 더하는 후속 PR(별도, MA2)이 필요하다. 이 PR은 그 문서를 고치지 않는다 |
| 8 | 플랫폼 UI/승인 연동이 필요하면 최소 범위·초기 설치/정책 승인 경로 보고, 도구 코드 PR만으로 배포·정책 반영 단정 금지 | 새 UI는 필요 없다 — PocketAide의 기존 승인 화면(이미 설치·운영 중)을 그대로 쓴다. 다만 이 도구의 배포만으로 `review-policy.md`의 승인 주체가 넓어지는 것은 **아니다** — 위 7번처럼 정책 문서의 별도 반영(소유자 승인)이 있어야 실제로 유효한 승인 경로가 된다 |
| 9 | 기본거부·타인승인·head변경·허위승인·재전송·실패응답·비밀 비노출 테스트, 운영 PR에 실제 승인 테스트 금지 | 뒤 「테스트 계획」. 이 PR에는 **코드가 없으므로 테스트도 없다** — go 툴체인이 없는 이 환경에서 작성해도 실행·검증할 수 없는 테스트를 "검증됨"이라 적을 수 없기 때문이다(아래 「이 환경의 실행 차단」) |

## 도구 개요 (제안)

- 이름(가제): `review_manual_approval_create`
- 입력
  - `repository`(필수) — owner 뺀 repo 이름
  - `pullNumber`(필수) — PR 번호(판정기가 돌아간 그 PR)
  - `expectedHeadSha`(필수) — 승인 요청을 띄우기 전에 호출자가 확인한 head. 40자 hex
  - `context`(필수) — `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES` 접두사 안의 값(이 레포는 현재
    `review/manual-approval` 하나)
  - `matchedCases`(필수) — 호출자가 이미 돌린 판정기가 찾은 MA 케이스 ID와 걸린 경로(사람이
    읽을 설명을 만들기 위한 재료일 뿐, 이 도구는 판정기를 다시 돌리지 않는다)
  - `targetUrl`(선택) — PR URL
- `state`는 입력에 없다. 이 도구는 **항상 `success`**만 적는다 — 거부·보류 상태를 적는 용도가
  아니다(요구사항 6).
- 동작
  1. 입력 검증(기존 `github_commit_status_create`의 AC3 재사용 + `pullNumber` 양의 정수)
  2. GitHub에서 PR의 **현재** head를 읽어 `expectedHeadSha`와 비교(AC4) — 다르면 재검토 안내로
     거부, status 기록도 승인 요청도 하지 않음
  3. 그 `context`의 최신 status가 이미 `success`면 중복 승인으로 알리고 끝(멱등 거부)
  4. `approvalgate.Gate.Authorize`로 PocketAide에 승인 요청 — `Call.Describe`가 repo·PR·head·
     `matchedCases`를 사람이 읽는 문장으로 렌더링
  5. `APPROVED`가 아닌 모든 응답(거절·만료·타임아웃·통신 실패·미설정)은 거부로 끝. **PocketAide
     외의 어떤 입력도 승인으로 인정하지 않는다** — 도구 인자에 승인 여부를 나타내는 필드가 없는
     것이 이를 구조적으로 보장
  6. 승인 직후 **다시** 2·3을 재확인(폴링 중 head가 바뀌었거나 다른 경로로 이미 기록됐을 수
     있음) — 어긋나면 기록하지 않고 재검토 안내
  7. `github.Service.CreateCommitStatus(context=<그 context>, state="success", ...)` 호출 —
     기존 접두사 검증(AC4)·토큰 발급/폐기(AC2)를 그대로 통과
  8. 성공을 `slog`로 한 줄 남기고(repo·PR·sha·context·PocketAide RequestID), 결과 반환

## 범위 밖
- **판정기 재실행** — `scripts/review-policy/judge.py`를 서버 안에서 다시 돌리지 않는다. 호출자
  (오늘은 reconciler-manual-approval 스킬이 하듯, 내일은 다른 에이전트/워크플로가)가 이미 돌린
  결과를 인자로 받는다. 서버에 Python 실행 환경을 들이지 않는다
- **새 allowlist·새 토큰 종류** — `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES`·`statuses: write` 내부
  발급 경로를 그대로 재사용한다
- **거부·반려 경로** — 이 도구는 성공만 기록한다. 거부는 (오늘처럼) 아무것도 쓰지 않는 것으로
  표현된다 — reconciler task의 `review_rejected` 전이 같은 반려 기록은 호출자(스킬/워크플로) 쪽
  책임으로 남는다
- **review-policy.md 본문 수정** — 요구사항 7의 위임정책 변경안은 이 문서가 **제안**하고, 실제
  반영은 소유자가 승인한 뒤 별도 PR(MA2)로 한다

## 열린 질문 (구현 전 소유자 확인 필요)

1. **PocketAide 승인 화면이 PR 리뷰 판단에 필요한 설명을 실을 수 있는가?** `reconciler-manual-approval`
   스킬은 이 질문 때문에 "버튼·선택 위젯을 쓰지 않는다 — 렌더링되면서 설명을 가리거나 접어,
   사용자가 무엇을 승인하는지 읽지 못한 채 선택지만 보게 된다"고 명시한다(스킬 Step 3). PocketAide의
   승인 요청 `context`(자유 텍스트 필드로 보임, `createRequestBody.Context`)가 같은 양의 설명을
   실을 수 있는지, 아니면 PocketAide 쪽 UI가 애초에 짧은 텍스트만 보여주도록 되어 있는지 확인이
   필요하다 — 전자라면 이 설계가 성립하고, 후자라면 "판단 없이 누르는" 위험이 reconciler 스킬이
   피하려던 바로 그 실패 모드가 된다
2. **"무권한/타 작업자 요청 거절"의 신원 경계는 결국 PocketAide의 캐릭터·기기 등록인가?** 이 서버는
   호출자(에이전트/세션)를 구분하지 않고 "PocketAide에 승인 요청을 띄울 수 있다"는 사실만 본다.
   그 요청에 응답할 수 있는 사람이 소유자 한 명으로 제한되는지는 PocketAide 쪽 등록에 달려 있고,
   이 서버 코드가 보증할 수 있는 범위 밖이다. 이것이 요구사항 3·4가 바라는 "서명된 신뢰 경로"로
   충분한지 소유자가 판단해야 한다
3. **2026-10-24 되돌림의 진짜 이유가 "이중 승인 마찰"뿐이라면, 신규 전용 도구는 문제가 없다.**
   다른 이유(예: "PocketAide는 클러스터 쓰기 전용이고 GitHub 판정에는 쓰지 않는다"는 원칙적
   결정)였다면 이 설계 전체가 다시 검토 대상이다. 되돌림 PR에 이유가 글로 남아 있지 않아 이 문서
   조사만으로는 확인할 수 없었다

## 이 환경의 실행 차단

이 PR을 준비한 세션에는 Go 툴체인이 없다(`go: command not found`, `apt-get install golang-go`도
패키지 미발견으로 실패). 그래서:
- 이 PR은 **코드를 포함하지 않는다** — 작성해도 빌드·테스트·`gofmt`를 실행할 수 없는 Go 코드를
  "검증됨"이라 적을 수 없기 때문이다
- 위 「열린 질문」이 정리되고 구현을 진행할 때는, go 툴체인이 있는 환경(CI 또는 로컬)에서 작성·
  `go build ./...`·`go vet ./...`·`gofmt -l .`·`go test ./...`까지 실행해 확인해야 한다

## 테스트 계획 (구현 시, 미실행)

- 기본거부: `GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES` 미설정 → 거부, PocketAide·GitHub 호출 0건
- head 변경: 승인 요청 전/승인 후 폴링 중 head가 `expectedHeadSha`와 달라짐 → 두 시점 각각 거부,
  status 미기록
- 허위승인 거부: 도구 인자에 승인 여부 필드가 없으므로 "approved=true" 같은 입력 자체가 무의미함을
  스키마 테스트로 고정(알 수 없는 필드 거부)
- 재전송/replay: 같은 요청을 승인 뒤 다시 호출 → `approvalgate.ErrConsumed` 경로, 중복 status 미기록
- 실패 응답: PocketAide 거절·만료·타임아웃·통신 실패·미설정 각각 도구 에러로 끝나고 GitHub 호출 0건
- 비밀 비노출: 성공·실패 응답 전문에 설치 토큰·PocketAide 내부 식별자가 없음
- **운영 PR에 대한 실제 승인 호출은 테스트에서도 수행하지 않는다** — 전부 github-mock·PocketAide
  스텁 대상
