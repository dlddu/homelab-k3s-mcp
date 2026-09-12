# PRD: approval-gate (승인 게이트 공통)

일부 도구 호출이 실행되기 **전에**, gatekeeper(`dlddu/gatekeeper`)의 사람 승인을 받아야만
통과하는 공통 경계.

개별 도구가 아니라 **동사와 종류에 걸리는 횡단 관심사**다. `prd-platform-auth-safety`가
"누가 서버에 접근할 수 있는가"를 정하듯, 이 문서는 "인증을 통과한 호출이 무엇을 실행해도
되는가"를 정한다.

## 게이트 대상

게이트는 **도구 이름이 아니라 그 도구가 행사하는 쿠버네티스 권한으로** 판정한다.
판정 키는 `(verb, resource[/subresource])` 쌍이며, verb는 발명하지 않고 RBAC 어휘를
그대로 쓴다 — `get`·`list`·`watch`·`create`·`update`·`patch`·`delete`·`deletecollection`,
그리고 서브리소스 `exec`·`attach`·`portforward`·`proxy`·`log`·`scale`.

발명한 이름(`apply`·`restart`·`describe`)으로 판정하면 게이트와 RBAC가 **서로 다른 어휘로
같은 것을 재게** 된다. 그러면 "이 서버가 무엇을 할 수 있나"를 어느 한쪽만 읽어서는 알 수
없고, 새 도구가 기존 verb를 행사하면서 게이트 표에 없다는 이유로 빠져나간다.

### 쓰기 게이트 — 되돌리기 어려운 변경

| 도구 | 행사하는 권한 | 비고 |
|------|----------------|------|
| `resource_apply` | `patch` + `create` on ⟨kind⟩ | Server-Side Apply는 `apply-patch+yaml` 콘텐츠 타입의 `patch`다 |
| `resource_delete` | `delete` on ⟨kind⟩ | |
| `resource_exec` | `create` on `pods/exec` | SPDY 실행기가 POST로 스트림을 연다 |

### 읽기 게이트 — 값 자체가 자격증명인 종류

| 도구 | 행사하는 권한 | 조건 |
|------|----------------|------|
| `resource_get` | `get` on ⟨kind⟩ | 대상이 읽기 게이트 종류일 때만 |
| `resource_describe` | `get` on ⟨kind⟩ + `list` on `events` | 대상이 읽기 게이트 종류일 때만 |

읽기 게이트 종류는 `RESOURCE_READ_GATED_KINDS`로 정하고 기본값은 `v1/Secret`이다.
평문 자격증명을 `spec`에 두는 CRD가 있으면 같은 목록에 더한다. 종류를 코드에 박지 않는
이유는 무엇이 자격증명인지가 클러스터마다 다르기 때문이다.

### 게이트 밖

| 도구 | 행사하는 권한 | 게이트를 타지 않는 이유 |
|------|----------------|--------------------------|
| `resource_list` | `list` on ⟨kind⟩ | Table 표현이라 값이 전송되지 않는다 |
| `resource_logs` | `get` on `pods/log` | |
| `resource_scale` | `update` on ⟨kind⟩`/scale` | 가역적·일상적 (아래) |
| `resource_restart` | `patch` on `apps/v1` 워크로드 | 가역적·일상적 (아래) |

> **`restart`와 `apply`는 RBAC가 구별하지 못한다.** 둘 다 `patch`다. `restart`를 게이트
> 밖에 두려면 RBAC가 `patch`를 줘야 하고, 그 순간 `apply`에 대한 RBAC 백스톱도 같은
> 리소스 범위에서 사라진다. 완화는 **리소스 범위로 좁히는 것**뿐이다 — `patch`를
> `apps/v1`의 세 워크로드에만 주면, 다른 종류에 대한 `apply`는 여전히 AC14의 "권한 밖"으로
> 떨어진다. `scale`은 `⟨kind⟩/scale` 서브리소스라 이 문제가 없다.

### 게이트 밖이면서 승인도 필요 없는 이유 — `scale`과 `restart`

둘은 파괴적 표기(`destructiveHint=true`)를 유지하되 게이트를 타지 않는다. 데이터를 지우지
않고 매니페스트를 되돌리면 원상 복구되는 **가역적** 조작이며, 홈랩 운영에서 가장 자주 쓰는
일상 동작이다. 여기에 매번 승인 프롬프트를 걸면 운영자가 gatekeeper의 `AUTO_APPROVE`를
켜게 되고, 그러면 쓰기 게이트와 읽기 게이트까지 한꺼번에 무인 승인으로 새어 나간다.
**게이트를 자주 울리게 만드는 설계는 게이트를 끄게 만든다.** 승인을 드물고 무겁게 유지하는
편이 실제 보호에 낫다.

그 대가로 이 게이트의 보증은 "승인 없이 클러스터를 바꿀 수 없다"가 아니라 **"승인 없이
되돌리기 어려운 일을 하거나 자격증명을 읽을 수 없다"**로 좁다. `resource_scale`로 0까지
줄이는 것과 `resource_restart`로 파드를 교체하는 것은 승인 없이 가능하며, 둘 다 서비스
중단을 부를 수 있다. 이것은 누락이 아니라 선택이다.

### 어떤 도구도 행사하지 않는 verb

`watch`·`update`(전체 교체)·`deletecollection`과 서브리소스
`attach`·`portforward`·`proxy`는 이 서버의 어느 도구도 행사하지 않는다. 따라서 게이트
표에 없고, **RBAC에도 부여하지 않는다**(`prd-resource-generic` AC15). 나중에 이들을
행사하는 도구가 생기면 게이트 표에 먼저 들어와야 한다 — 등급은 다음과 같다.

| verb | 등급 | 근거 |
|------|------|------|
| `watch` on 읽기 게이트 종류 | **금지** | 변경 스트림이 객체 전문을 밀어 주므로 `data`가 그대로 흘러나온다. 읽기 게이트를 통째로 우회한다 |
| `attach` on `pods/attach` | 쓰기 게이트 (`exec` 등급) | 실행 중인 컨테이너의 stdio에 붙는다. `exec`과 같은 권능이다 |
| `portforward` on `pods/portforward` | 쓰기 게이트 (`exec` 등급) | 파드 네트워크로 임의 TCP 터널을 연다. 네트워크 정책을 우회한다 |
| `proxy` on `nodes/proxy` | **금지** | kubelet API에 직접 닿는다 — 모든 파드의 로그와 exec이 여기서 열린다. 이 서버의 다른 모든 경계를 무의미하게 만든다 |
| `deletecollection` | 쓰기 게이트 + 영향 건수 필수 | 한 번의 승인이 몇 개를 지우는지 모르면 승인이 아니다. `context`에 대상 수가 들어가야 한다 |

> **남은 예외 1건 — `dear_baby_reset_user`**: `create` on `pods/exec`을 행사하므로 위
> 쓰기 게이트 정의에 해당한다. 그럼에도 편입하지 않은 것은 이 도구가 V5(앱 기능의
> 도구화)에 속해 좌표가 아니라 앱 의미로 대상을 지정하기 때문이며, 편입하려면
> `prd-dear-baby-reset-user`의 AC를 함께 고쳐야 한다. `doc-tracker.md`의 "수용된 위험"에
> 미결로 기록한다.
>
> `session_write`는 이 목록에서 뺐다. 제어면 API 호출이지 쿠버네티스 권한을 행사하지
> 않으므로 이 게이트의 정의 범위 밖이다.

## 달성 가치

- **V3: 안전한 운영(Safe-by-default)** — `destructiveHint`는 클라이언트에 대한 *광고*일 뿐
  실행을 막지 못한다. 이 게이트는 서버 쪽에서 실행 자체를 막는다.
- **V1: 자연어로 클러스터 운영** — 게이트가 있기 때문에 `resource_apply`·`resource_delete`
  같은 넓은 표면과 Secret 읽기를 열 수 있다. 게이트는 기능을 제한하는 장치가 아니라,
  넓은 표면을 열기 위한 전제 조건이다.

## 구성

| 환경 변수 | 필수 | 설명 |
|-----------|------|------|
| `GATEKEEPER_BASE_URL` | O | gatekeeper 백엔드 주소 (예: `http://gatekeeper.gatekeeper.svc:3000`) |
| `GATEKEEPER_API_KEY` | O | `x-api-key` 헤더 값. `POST /api/requests`와 `GET /api/requests/{id}` 양쪽에 필요 |
| `GATEKEEPER_USER_ID` | X | 푸시 알림을 받을 사용자. 미지정 시 알림 없이 웹 UI 확인에만 의존 |
| `GATEKEEPER_TIMEOUT_SECONDS` | X | 승인 대기 상한. 기본 300 |
| `GATEKEEPER_POLL_INTERVAL_SECONDS` | X | 판정 폴링 주기. 기본 2 |
| `RESOURCE_READ_GATED_KINDS` | X | 읽기 게이트 종류. 기본 `v1/Secret` |

## Acceptance Criteria

### AC1: 게이트는 RBAC verb로 판정한다
- **설명**: 각 도구는 자신이 행사하는 `(verb, resource[/subresource])` 쌍을 선언하고,
  게이트는 **도구 이름이 아니라 그 쌍**으로 판정한다. 위 표의 쌍에 해당하는 호출은
  gatekeeper 판정이 `APPROVED`인 경우에만 쿠버네티스 API에 도달한다.

  게이트를 건너뛰는 경로는 존재하지 않는다. 게이트 호출은 도구 핸들러가 선택적으로
  부르는 것이 아니라 디스패처 단계에서 강제되며, **선언되지 않은 쌍을 행사하는 도구는
  등록 자체가 거부된다** — 선언과 실제 행사가 어긋나면 게이트 표가 거짓말을 하게 되고,
  그 어긋남은 런타임에 조용히 통과한다.
- **달성 가치**: V3
- **검증 방법**: 게이트 대상 쌍을 승인 없이 호출하면 쿠버네티스 클라이언트가 단 한 번도
  호출되지 않는다(가짜 k8s 서비스의 호출 카운트가 0). 게이트 대상이 아닌 쌍
  (`update` on `/scale`, `patch` on 워크로드, 비게이트 종류의 `get`, `list`)은 같은 조건에서
  정상 동작한다. 선언에 없는 쌍을 행사하는 가짜 도구를 등록하면 기동이 실패한다.

### AC2: 승인 요청 생성
- **설명**: 게이트 대상 호출은 `POST /api/requests`로 승인 요청을 만든다. `x-api-key` 헤더,
  `externalId`(호출마다 고유), `context`(AC3), `requesterName`,
  `timeoutSeconds`(`GATEKEEPER_TIMEOUT_SECONDS`)를 보낸다. `GATEKEEPER_USER_ID`가 있으면
  `userId`를 함께 보내 푸시 알림이 가게 한다.
- **달성 가치**: V3
- **검증 방법**: 요청 본문에 위 필드가 모두 담기고, `externalId`가 호출마다 달라진다.

### AC3: 판정 가능한 context
- **설명**: `context`는 운영자가 **푸시 알림과 승인 화면만 보고** 판단할 수 있어야 한다.
  다음을 모두 담는다 — 도구 이름, 대상 좌표(`apiVersion`/`kind`/`namespace`/`name`),
  동사별 상세, 그리고 요청 시각. 동사별 상세는:
  - `apply` — 신규 생성인지 갱신인지, 갱신이면 바뀌는 필드 요약
  - `delete` — 삭제 대상과 `gracePeriodSeconds`
  - `exec` — 컨테이너 이름과 **실행할 명령 전문**(요약·생략 금지)
  - `get`·`describe`(읽기 게이트) — 어떤 종류의 어떤 대상을 읽는지, 그리고 `get`은 값까지
    반환되고 `describe`는 키 이름과 바이트 수만 반환된다는 구분

  대상 좌표를 해석할 수 없거나 상세를 만들 수 없으면 승인 요청을 만들지 않고 거부한다.
  무엇을 승인하는지 모르는 승인 요청은 승인 버튼을 형식적 절차로 만든다.
- **달성 가치**: V3
- **검증 방법**: 동사별로 생성된 `context` 문자열에 위 구성 요소가 모두 포함된다. 특히
  `exec`의 명령 인자가 잘리거나 `[...]`로 축약되지 않는다.

### AC4: 판정 폴링
- **설명**: 요청 생성 후 `GET /api/requests/{id}`를 `GATEKEEPER_POLL_INTERVAL_SECONDS`
  주기로 폴링해 판정을 기다린다. `APPROVED`면 실행하고, `REJECTED`·`EXPIRED`면 거부한다.
  gatekeeper는 만료를 백그라운드로 처리하지 않고 **조회 시점에 평가**하므로
  (`expiresAt` 경과 + `PENDING` → `EXPIRED` 전이), 폴링 없이 최초 응답만 믿으면 만료를
  영영 관측하지 못한다.
- **달성 가치**: V3
- **검증 방법**: `PENDING` → `APPROVED` 전이가 폴링으로 관측되고 실행이 이어진다.
  `timeoutSeconds`가 지난 요청을 폴링하면 `EXPIRED`로 관측되고 거부된다.

### AC5: Fail-closed
- **설명**: 승인이 확인되지 않은 모든 경우는 거부다. 구체적으로 — `REJECTED`, `EXPIRED`,
  `GATEKEEPER_TIMEOUT_SECONDS` 내 판정 없음, `externalId` 충돌(409), gatekeeper 5xx,
  네트워크 오류·타임아웃, `GATEKEEPER_BASE_URL`/`GATEKEEPER_API_KEY` 미설정. 어느 경우든
  쿠버네티스 API를 호출하지 않고 에러를 반환한다.

  이는 이 서버의 기존 graceful degradation 원칙과 어긋나지 않는다. 통합이 미설정이면
  **해당 도구만** 에러를 반환하고 서버는 계속 산다는 원칙은 그대로이며, 여기서는 그
  "해당 도구"가 게이트 대상 호출 전체일 뿐이다. gatekeeper가 죽어 있어도
  비게이트 읽기와 `scale`·`restart`는 계속 동작한다.
- **달성 가치**: V3
- **검증 방법**: 위 실패 경로 각각에서 게이트 대상 호출이 에러를 반환하고 가짜 k8s 서비스
  호출 카운트가 0이다. 같은 조건에서 비게이트 읽기는 정상 동작한다.

### AC6: 승인–실행 정합성 (TOCTOU)
- **설명**: 승인 요청을 만든 시각과 실제로 실행하는 시각 사이에 대상이 바뀔 수 있다.
  운영자가 승인한 것은 "그 시점의 그 상태에 대한 그 조작"이므로, 실행 직전에 대상의
  `resourceVersion`이 승인 요청 작성 시 읽은 값과 같은지 확인한다. 다르면 실행하지 않고
  거부하며, 재승인이 필요함을 알린다. `exec`는 대상 파드의 `uid`로 같은 확인을 한다
  (같은 이름의 새 파드는 다른 파드다).

  읽기 게이트에도 같은 확인이 필요하다. 승인 후 실행 전에 Secret이 교체되면 운영자가
  승인한 것과 다른 값을 반환하게 된다.
- **달성 가치**: V3
- **검증 방법**: 승인 후 실행 전에 대상을 외부에서 변경하면 실행이 거부된다. Secret 읽기도
  같다.

### AC7: 1승인 1실행
- **설명**: 승인 하나는 단 한 번의 쿠버네티스 API 호출만 인가한다. 승인을 소비한 뒤
  그 `externalId`·요청 id는 폐기되며 재사용되지 않는다. 실행이 실패해 재시도할 때도
  승인을 새로 받는다. 읽기 게이트도 예외가 아니다 — 한 번 승인받은 Secret을 대화가
  이어지는 동안 다시 읽으려면 다시 승인받는다.
- **달성 가치**: V3
- **검증 방법**: 같은 승인 id로 두 번째 실행을 시도하면 거부된다.

### AC8: 감사 기록
- **설명**: 게이트 대상 호출마다 요청 id, `externalId`, 도구·동사·대상 좌표, 판정 결과,
  판정자(`processedById`), 실행 결과를 서버 로그에 남긴다. 승인 기록 없이 실행된 게이트
  대상 호출은 로그상 존재할 수 없다.
- **달성 가치**: V3
- **검증 방법**: 승인 후 실행한 호출의 로그에 요청 id와 판정자가 남고, 거부된 호출은
  거부 사유와 함께 남는다.

### AC9: 자동 응답 모드의 가시화
- **설명**: gatekeeper는 사용자별 `autoResponseMode`(`AUTO_APPROVE`/`AUTO_REJECT`)를 지원하며,
  `AUTO_APPROVE`가 켜져 있으면 사람이 보지 않은 채 승인이 떨어진다. 이는 게이트를 사실상
  무력화하는 경로이므로 숨기지 않는다. 요청 생성 응답의 `autoApproved`/`autoRejected`를
  로그에 남기고, 자동 승인으로 통과한 실행은 **도구 응답 본문에도** 자동 승인이었음을
  표기한다.
- **달성 가치**: V3
- **검증 방법**: `AUTO_APPROVE` 사용자로 게이트 대상 호출을 수행하면 응답에 자동 승인 표기가
  포함되고, 로그에도 남는다.

### AC10: 승인된 읽기의 값은 응답에만 담긴다
- **설명**: 읽기 게이트가 열리면 자격증명 값이 반환되므로, 그 값이 **응답 외의 어디에도
  남지 않게** 한다.
  - 승인 요청 `context`에 값을 넣지 않는다. 좌표만 넣는다. 값이 승인 화면과 푸시 알림에
    뜨면 게이트가 오히려 유출 경로가 된다 — 승인하지 않아도 이미 본 것이 된다.
  - 감사 로그(AC8)에 값을 넣지 않는다.
  - 에러 메시지에 값을 넣지 않는다. 특히 실행 실패 시 대상 객체를 통째로 덤프하지 않는다.
  - `list`는 값을 담지 않는 표현으로만 응답한다(`prd-resource-generic` AC10).
- **달성 가치**: V3
- **검증 방법**: Secret 읽기 승인 요청의 `context`, 서버 로그, 그리고 각 실패 경로의 에러
  메시지 어디에도 `data` 값이 등장하지 않는다. 승인 후 정상 응답에만 담긴다.
