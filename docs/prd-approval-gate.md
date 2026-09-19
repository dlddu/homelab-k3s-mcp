# PRD: approval-gate (승인 게이트 공통)

일부 도구 호출이 실행되기 **전에**, gatekeeper(`dlddu/gatekeeper`)의 사람 승인을 받아야만
통과하는 공통 경계.

개별 도구가 아니라 **동사와 종류에 걸리는 횡단 관심사**다. `prd-platform-auth-safety`가
"누가 서버에 접근할 수 있는가"를 정하듯, 이 문서는 "인증을 통과한 호출이 무엇을 실행해도
되는가"를 정한다.

## 게이트 대상

게이트는 **도구 이름이 아니라 그 도구가 행사하는 RBAC verb로** 판정한다. 도구가 verb와
1:1이므로(`prd-resource-generic`) 이 표는 도구 목록이자 verb 목록이다.

### 쓰기 게이트 — 상태를 바꾸는 모든 verb

| verb | 도구 | 비고 |
|------|------|------|
| `create` | `resource_create` | |
| `update` | `resource_update` | `subresource=scale`(레플리카 변경) 포함 |
| `patch` | `resource_patch` | SSA(`apply-patch+yaml`)와 롤링 재시작 모두 이 verb다 |
| `delete` | `resource_delete` | |
| `deletecollection` | `resource_delete_collection` | `context`에 대상 수와 이름 목록이 필수 |
| `create` on `pods/exec` | `resource_exec` | SPDY 실행기가 POST로 스트림을 연다 |
| `create` on `pods/attach` | `resource_attach` | 새 프로세스가 아니라 주 프로세스 stdio에 붙는다 |
| `create` on `pods/portforward` | `resource_port_forward` | 파드 네트워크로 단발 TCP 왕복 |
| HTTP 메서드별 verb on `⟨kind⟩/proxy` | `resource_proxy` | `Pod`·`Service`·`Node`. 경로 제한 없음. `GET`도 게이트를 탄다 |

### 읽기 게이트 — 민감 종류

| verb | 도구 | 조건 |
|------|------|------|
| `get` | `resource_get` | 대상이 민감 종류일 때만 |
| `watch` | `resource_watch` | 대상이 민감 종류일 때만. 스트림은 객체 전문을 밀어 주므로 `get`과 같은 노출이다 |

민감 종류(`RESOURCE_GATED_KINDS`, 기본 `v1/Secret`)는 **쓰기도 게이트를 탄다** — 다만 쓰기는
원래 모든 종류에서 게이트 대상이므로 실질적인 추가는 읽기 쪽이다. 쓰기의 `context`에서는
값을 가린다(`prd-resource-generic` AC16).

민감 종류는 `RESOURCE_GATED_KINDS`로 정하고 기본값은 `v1/Secret`이다.
평문 자격증명을 `spec`에 두는 CRD가 있으면 같은 목록에 더한다. 종류를 코드에 박지 않는
이유는 무엇이 자격증명인지가 클러스터마다 다르기 때문이다.

### 게이트 밖

| verb | 도구 | 이유 |
|------|------|------|
| `list` | `resource_list` | Table 표현이라 값이 전송되지 않는다(`prd-resource-generic` AC2·AC17) |
| `watch` (비민감 종류) | `resource_watch` | 상태를 바꾸지 않고 자격증명도 아니다 |
| `get` (비민감 종류) | `resource_get` | 상태를 바꾸지 않고 자격증명도 아니다. **`⟨kind⟩/proxy`의 `get`은 여기 해당하지 않는다** — 쿠버네티스 객체를 읽는 것이 아니라 클러스터 내부의 임의 엔드포인트에 도달하는 것이라 성질이 다르다 |
| — | `api_resources` | 리소스 권한을 행사하지 않는다 |

### 부여하지 않는 verb는 없다

이 서버는 쿠버네티스 RBAC의 모든 관련 verb를 행사한다 — `get`·`list`·`watch`·`create`·
`update`·`patch`·`delete`·`deletecollection`과 서브리소스 `exec`·`attach`·`portforward`·
`proxy`(`Node` 포함)·`log`·`scale`. 초안들이 유지하던 금지 목록은 비었다.

그 결과 **RBAC는 백스톱이 아니다.** 이전에는 미부여가 "도구 레이어가 뚫려도 apiserver가
막는" 2차 방어선이었지만, 이제 게이트 판정이 유일한 경계다. 특히 `nodes/proxy`가 열려
있으므로 `(verb, resource)` 쌍은 호출이 무엇을 하는지 한정하지 못한다 —
`create nodes/proxy` 하나가 `/healthz` POST와 임의 파드 exec을 동시에 뜻한다.
한정하는 것은 **경로이고, 경로는 `context`에만 나타난다**(AC3).

이것은 이 설계가 명시적으로 받아들인 위치다. 경로나 패치 내용으로 예외를 주면 분류기가
하나 더 생기고 그 분류기가 곧 우회 경로가 되므로, 대신 게이트가 **무엇 하나 숨기지 않고**
보여 주고 판단을 사람에게 남긴다. 그 대가는 `context`의 품질이 곧 보안이라는 것이다 —
AC3가 이 문서에서 가장 무거운 AC인 이유다.

> **남은 예외 1건 — `dear_baby_reset_user`**: `create` on `pods/exec`을 행사하므로 위
> 쓰기 게이트 정의에 해당한다. 그럼에도 편입하지 않은 것은 이 도구가 V5(앱 기능의
> 도구화)에 속해 좌표가 아니라 앱 의미로 대상을 지정하기 때문이며, 편입하려면
> `prd-dear-baby-reset-user`의 AC를 함께 고쳐야 한다. `doc-tracker/`의 "수용된 위험"에
> 미결로 기록한다.
>
> `session_write`는 이 목록에서 뺐다. 제어면 API 호출이지 쿠버네티스 권한을 행사하지
> 않으므로 이 게이트의 정의 범위 밖이다.

## 달성 가치

- **V3: 안전한 운영(Safe-by-default)** — `destructiveHint`는 클라이언트에 대한 *광고*일 뿐
  실행을 막지 못한다. 이 게이트는 서버 쪽에서 실행 자체를 막는다.
- **V1: 자연어로 클러스터 운영** — 게이트가 있기 때문에 `resource_patch`·`resource_delete`
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
| `RESOURCE_GATED_KINDS` | X | 민감 종류. 기본 `v1/Secret` |

## Acceptance Criteria

### AC1: 게이트는 RBAC verb로 판정한다
- **설명**: 각 도구는 자신이 행사하는 `(verb, resource[/subresource])` 쌍을 선언하고,
  게이트는 **도구 이름이 아니라 그 쌍**으로 판정한다. 상태를 바꾸는 verb
  (`create`·`update`·`patch`·`delete`·`create pods/exec`)는 **예외 없이** 게이트 대상이며,
  민감 종류의 `get`·`watch`도 같다. 이들은 gatekeeper 판정이 `APPROVED`인 경우에만
  쿠버네티스 API에 도달한다.

  판정은 **verb에만** 걸리고 패치 내용이나 경로를 보지 않는다. 대상 종류를 보는 것은
  민감 종류 판정 한 곳뿐이다. 그 밖에는 내용을 보지
  않는다. 내용으로 판정하면 "이 패치는 재시작이니 봐준다" 같은 예외가 생기고, 그런 예외는
  분류기를 하나 더 만드는 일이라 그 분류기가 곧 우회 경로가 된다.

  게이트를 건너뛰는 경로는 존재하지 않는다. 게이트 호출은 도구 핸들러가 선택적으로
  부르는 것이 아니라 디스패처 단계에서 강제되며, **선언되지 않은 쌍을 행사하는 도구는
  등록 자체가 거부된다** — 선언과 실제 행사가 어긋나면 게이트 표가 거짓말을 하게 되고,
  그 어긋남은 런타임에 조용히 통과한다.
- **달성 가치**: V3
- **검증 방법**: 변경 verb 여섯과 민감 종류의 `get`·`watch`를 승인 없이 호출하면 쿠버네티스
  클라이언트가 단 한 번도 호출되지 않는다(가짜 k8s 서비스의 호출 카운트가 0). `list`와
  비게이트 종류의 `get`은 같은 조건에서 정상 동작한다. 선언에 없는 쌍을 행사하는 가짜
  도구를 등록하면 기동이 실패한다.

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
  - `create` — 만들어질 객체의 종류와 이름, 그리고 주요 스펙 요약. 상세는 **매니페스트
    자체**에서 만든다 — 대상이 아직 없으므로 클러스터에서 읽어 올 것이 없다. 여러 문서가
    담긴 매니페스트는 문서마다 승인 요청이 하나씩이고, **한 요청의 `context`는 그 문서
    하나만** 기술한다. 승인 버튼 하나가 인가하는 것이 그 문서 하나이기 때문이다
    (`prd-resource-generic` AC7)
  - `update` — 교체 전후로 바뀌는 필드 요약. `subresource=scale`이면 현재 → 목표 레플리카
  - `patch` — `patchType`과 **패치 본문 전문**. 재시작 어노테이션 패치도 예외가 아니다 —
    무엇을 바꾸는 패치인지는 본문을 봐야 알 수 있고, 요약하면 그 판단을 서버가 대신하게 된다
  - `delete` — 삭제 대상과 `gracePeriodSeconds`
  - `deletecollection` — 셀렉터, **삭제될 대상 수와 이름 목록**(많으면 앞 20개와 총 개수)
  - `exec` — 컨테이너 이름과 **실행할 명령 전문**(요약·생략 금지)
  - `attach` — 컨테이너 이름, `readSeconds`, 그리고 `stdin`이 있으면 **그 내용 전문**
  - `port_forward` — 대상 포트와 **보낼 페이로드 전문**. 네트워크 정책이 막아 둔 포트에
    도달할 수 있으므로 어디로 무엇을 보내는지가 승인의 전부다
  - `proxy` — HTTP 메서드, 대상 종류·이름, **경로와 본문 전문**
  - `get`·`watch`(민감 종류) — 어떤 종류의 어떤 대상을 읽는지

  두 가지 예외 처리가 붙는다.
  - **민감 종류 쓰기는 값을 가린다** — `data`·`stringData`를 키 이름과 바이트 수로 대체한다.
    값을 그대로 실으면 승인 화면과 푸시 알림이 유출 경로가 되어, 거절해도 이미 본 것이 된다.
  - **kubelet 고권한 경로는 표시한다** — `nodes/proxy`의 경로가 `/exec`·`/attach`·
    `/portForward`·`/run`·`/logs`·`/containerLogs`에 해당하면 `context`에 그 사실을 눈에
    띄게 적는다. **막지 않는다** — 경로는 이미 전문으로 실려 있고, 이 표시는 운영자가
    놓치지 않게 하는 것뿐이다.

  스트림 넷(`exec`·`attach`·`port_forward`·`proxy`)은 RBAC로 내용을 가릴 수 없어 게이트가
  유일한 방어선이다. 이들의 `context`에서 전문 노출은 선택이 아니다.

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
  "해당 도구"가 게이트 대상 호출 전체일 뿐이다. gatekeeper가 죽어 있으면 **클러스터를
  바꾸는 일은 전부 멈추고** 읽기(`list`, 비게이트 종류의 `get`)만 남는다. 이는 의도된
  것이다 — 승인 경로가 죽었는데 변경이 나가면 게이트가 있으나 마나다.
- **달성 가치**: V3
- **검증 방법**: 위 실패 경로 각각에서 게이트 대상 호출이 에러를 반환하고 가짜 k8s 서비스
  호출 카운트가 0이다. 같은 조건에서 `list`와 비게이트 종류의 `get`은 정상 동작한다.

### AC6: 승인–실행 정합성 (TOCTOU)
- **설명**: 승인 요청을 만든 시각과 실제로 실행하는 시각 사이에 대상이 바뀔 수 있다.
  운영자가 승인한 것은 "그 시점의 그 상태에 대한 그 조작"이므로, 실행 직전에 대상의
  `resourceVersion`이 승인 요청 작성 시 읽은 값과 같은지 확인한다. 다르면 실행하지 않고
  거부하며, 재승인이 필요함을 알린다. `exec`는 대상 파드의 `uid`로 같은 확인을 한다
  (같은 이름의 새 파드는 다른 파드다).

  `update`·`patch`도 같다 — 승인 시점에 읽은 `resourceVersion`으로 프리컨디션을 건다.
  **갱신 시 `resourceVersion` 충돌이 발생하면 자동으로 거부하고 현재 호출을 종료한다.**
  서버가 새 버전을 읽어 조건을 바꾸거나, 쓰기를 자동 재시도하거나, 새 승인 요청을
  자동으로 만들지 않는다. 호출자가 나중에 별도 호출로 다시 시도하려면 새 승인이 필요하다.
  읽기 게이트에도 같은 확인이 필요하다. 승인 후 실행 전에 Secret이 교체되면 운영자가
  승인한 것과 다른 값을 반환하게 된다.

  **`create`는 예외이고, 그 예외는 느슨함이 아니다.** 생성에는 승인 시점에 읽을 상태가
  없다 — 대상이 아직 없는 것이 정상 경로이므로 비교할 `resourceVersion`도 `uid`도
  존재하지 않는다. 여기서 운영자가 승인한 전제는 "그 좌표가 비어 있다"이고, 실행 직전에
  그 전제가 여전히 참인지는 **apiserver의 `create` 자신이 원자적으로** 판정한다(이름이
  이미 쓰이고 있으면 409). 게이트가 대상을 미리 읽어 두었다가 재확인하는 절차를 여기에
  두지 않는 이유는 그 절차가 **더 약하기** 때문이다 — 읽고 나서 쓰기까지 새 창이 생기는데,
  apiserver의 판정에는 그 창이 없다. 따라서 **`create`를 행사하는 도구는 게이트에 읽을
  대상을 넘기지 않으며, 그것은 선언 누락이 아니라 이 절이 준 면제다.** 면제의 범위는
  `create` 하나다: 같은 호출이 `update`나 `patch`를 함께 행사한다면 그 쪽에는 위 문단이
  그대로 적용된다. `resource_create`가 이 면제를 어떻게 쓰는지는 `prd-resource-generic`
  AC7에 있다.
- **달성 가치**: V3
- **검증 방법**: 승인 후 실행 전에 대상을 외부에서 변경하면 실행이 거부된다. Secret 읽기도
  같다. `create`는 승인 후 실행 전에 같은 이름의 객체가 외부에서 생기면 **apiserver의
  409**로 거부되고, 그 거부가 게이트의 재확인 없이 일어난다(게이트는 생성 대상을 미리
  읽지 않는다).

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

### AC10: 자격증명 값은 응답에만 담긴다
- **설명**: 민감 종류의 값은 읽기에서는 응답으로 나오고 쓰기에서는 요청으로 들어온다.
  어느 쪽이든 그 값이 **응답 외의 어디에도 남지 않게** 한다.
  - 승인 요청 `context`에 값을 넣지 않는다. 좌표만 넣는다. 값이 승인 화면과 푸시 알림에
    뜨면 게이트가 오히려 유출 경로가 된다 — 승인하지 않아도 이미 본 것이 된다.
  - 감사 로그(AC8)에 값을 넣지 않는다.
  - 에러 메시지에 값을 넣지 않는다. 특히 실행 실패 시 대상 객체를 통째로 덤프하지 않는다.
  - `list`는 값을 담지 않는 표현으로만 응답한다(`prd-resource-generic` AC17).
  - **쓰기 요청의 값도 같다** — `create`·`update`·`patch`로 들어온 `data`·`stringData`는
    `context`·로그·에러 어디에도 남지 않는다. 들어오는 값이 나가는 값보다 안전할 이유가 없다.
- **달성 가치**: V3
- **검증 방법**: Secret 읽기·쓰기 양쪽에서 승인 요청 `context`, 서버 로그, 각 실패 경로의
  에러 메시지 어디에도 `data` 값이 등장하지 않는다. 읽기는 승인 후 정상 응답에만 담기고,
  쓰기는 어디에도 담기지 않는다.

### AC11: 게이트 자신이 행사하는 권한을 선언한다
- **설명**: 게이트는 승인을 중개하기만 하지 않는다. AC3의 `context`를 채우려면 대상의
  현재 상태를 읽어야 하고(스케일의 현재 레플리카, `deletecollection`의 대상 수와 이름),
  AC6의 프리컨디션을 걸려면 `resourceVersion`을 읽어야 한다. 이것들은 **게이트가 행사하는
  쿠버네티스 권한**이며 도구의 것이 아니다.

  게이트는 다음 두 쌍을 선언한다. 이전 판에서는 `prd-resource-generic` AC19의 RBAC 대조가
  이를 함께 셌고, 선언은 그 대조를 성립시키기 위해 필요했다. AC19가 결번이 되어 대조는
  없어졌지만 **선언은 남긴다** — 게이트가 승인 전에 무엇을 읽는지는 대조와 무관하게
  리뷰어가 알아야 하는 사실이고, 아래 민감 종류 예외가 걸리는 자리가 바로 이 두 쌍이다.

  | 쌍 | 쓰임 |
  |----|------|
  | `get` on ⟨kind⟩ | `resourceVersion` 프리컨디션, 스케일의 현재 레플리카 |
  | `list` on ⟨kind⟩ | `deletecollection`의 대상 수·이름 목록 |

  **민감 종류에는 예외가 적용된다.** Secret의 `resourceVersion`을 얻겠다고 전체
  객체를 `get`하면 게이트가 승인 전에 값을 읽게 된다 — 승인 여부와 무관하게 이미 서버
  메모리에 값이 들어온 것이고, 그 상태에서 거절이 나면 게이트는 아무것도 막지 못한 셈이다.
  따라서 민감 종류의 프리컨디션은 **PartialObjectMetadata**
  (`Accept: application/json;as=PartialObjectMetadata;v=v1;g=meta.k8s.io`)로 받는다.
  apiserver가 서버 사이드로 메타데이터만 잘라 보내므로 `data`가 전송되지 않는다.

  **이 문자열이 틀리면 조용히 반대로 동작한다.** 버전·그룹 파라미터가 어긋나도 apiserver는
  오류를 주지 않고 **전체 객체**로 답한다 — 즉 게이트가 승인 전에 `data`를 읽게 되고,
  이 AC가 존재하는 이유가 그 자리에서 뒤집힌다. 그래서 `prd-resource-generic` AC2의 Table
  헤더와 같은 취급을 한다: 값을 손으로 적지 말고 `metav1.SchemeGroupVersion`에서 파생시키며,
  응답이 PartialObjectMetadata가 아니면 **거부한다**(전체 객체로 폴백하지 않는다).

  **컬렉션 목록의 선행 읽기**는 별도 `CollectionTargetReader.ListTargets`로 제공한다.
  도구의 `Service`나 단일 객체 `TargetReader`에 합치지 않는다. 이 내부 읽기는 유효한
  `namespace` 하나와 namespaced 종류만 받고, 두 셀렉터를 모든 페이지에 그대로 전달한다.
  컬렉션에는 `as=PartialObjectMetadataList`를 사용한다(그룹·버전은 위와 같이 파생).
  [상류 메타데이터 목록 계약](https://github.com/kubernetes/client-go/blob/v0.33.3/metadata/metadata.go#L240)을
  따르되, 전체 객체 폴백은 제공하지 않는다. 비민감 종류도 같은 메타데이터 표현을 쓴다.
  반환하는 것은 목록의 `resourceVersion`과 대상별 이름·namespace·uid·resourceVersion뿐이다.

  한 페이지를 전체 대상 수로 오인하지 않도록 `continue`가 끝날 때까지 같은 목록 버전의
  페이지를 모으고 이름순으로 정렬한다. 버전 불일치, 잘못된 표현/좌표/식별자, 중복 대상,
  반복 커서, 완료 페이지의 남은 건수, 만료(410)·통신·디코딩 오류는 **부분 결과 없이 거부**한다.
  0건도 유효한 목록 버전을 가진 빈 결과다. 요청당 500개 페이지, 전체 10,000개 대상 또는
  100페이지를 상한으로 두어 승인 전 작업량을 제한하며, 상한 초과는 축약하지 않고 선택을
  좁히도록 거부한다. 호출 context 취소도 부분 결과를 반환하지 않는다.

  이 읽기 기반의 완료는 `resource_delete_collection` 등록이나 AC11 전체 완료가 아니다.
  승인 context의 수/앞 20개 이름, 0건의 승인 생략, 실행 직전 대상 비교와 실제
  `deletecollection` 호출, 승인 1건당 실행 1회, 실물 apiserver/gatekeeper 검증은 후속 범위다.
- **달성 가치**: V3
- **검증 방법**: 게이트가 승인 전에 행사하는 쌍이 위 표와 같고 그 밖의 쌍이 없다(요청 기록
  관측 — `rbac.yaml` 대조는 AC19와 함께 없어졌다). Secret 읽기 승인을 **거절**한 뒤 서버
  프로세스의 요청 기록을 보면 전체 객체 `get`이 한 번도 없고 PartialObjectMetadata 요청만
  있다. 그 요청의 `Accept` 헤더는 모양이 아니라 **값으로** 단언하고, 표현을 협상하는 서버 앞에서
  확인한다 — 헤더가 어긋나면 요청이 실패하는 것이 아니라 전체 객체가 돌아오므로, 자기가 만든
  응답을 먹이는 테스트는 그 어긋남을 볼 수 없다.
