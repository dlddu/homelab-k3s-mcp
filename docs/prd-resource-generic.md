# PRD: resource_* (generic resource 도구군)

특정 워크로드 종류에 묶이지 않고 **리소스 좌표**(`apiVersion` + `kind` + `namespace` + `name`)로
임의의 쿠버네티스 리소스를 다루는 도구군.

도구는 **쿠버네티스 RBAC verb와 1:1**이다. 도구 하나가 verb 하나를 행사하며, 그 이상도
이하도 아니다.

## 달성 가치

- **V1: 자연어로 클러스터 운영** — `kubectl`이 다루는 리소스 표면 대부분을 자연어로 연다.
  종류 제약이 사라지므로 Service·Ingress·ConfigMap·PVC·CRD를 다루려고 도구를 새로 만들 일이
  없다.
- **V3: 안전한 운영(Safe-by-default)** — 상태를 바꾸는 verb는 **예외 없이** 사람 승인을
  거치고(`prd-approval-gate` AC1), 민감 종류는 읽기도 쓰기도 승인을 거치며(AC16),
  그 값이 응답 밖으로 새지 않는다(AC17).

## 도구 개요

| 도구 | RBAC verb | 게이트 | 흡수하는 폐기 도구 |
|------|-----------|--------|---------------------|
| `resource_list` | `list` | — | `namespace_list`, `workload_list` |
| `resource_get` | `get` (+`subresource`) | 민감 종류만 | `workload_logs` (`subresource=log`) |
| `resource_watch` | `watch` | 민감 종류만 | — |
| `resource_create` | `create` | O | — |
| `resource_update` | `update` (+`subresource`) | O | `workload_scale` (`subresource=scale`) |
| `resource_patch` | `patch` | O | `workload_restart` |
| `resource_delete` | `delete` | O | — |
| `resource_delete_collection` | `deletecollection` | O | — |
| `resource_exec` | `create` on `pods/exec` | O | — |
| `resource_attach` | `create` on `pods/attach` | O | — |
| `resource_port_forward` | `create` on `pods/portforward` | O | — |
| `resource_proxy` | HTTP 메서드에 대응하는 verb on `⟨kind⟩/proxy` | O | — |
| `api_resources` | (discovery — 리소스 권한 아님) | — | — |

스트림 서브리소스 넷을 한 도구로 묶지 않는 이유도 1:1이다. `pods/exec`·`pods/attach`·
`pods/portforward`·`⟨kind⟩/proxy`는 RBAC에서 **서로 다른 서브리소스**이고 따로 부여·회수된다.
한 도구로 묶으면 그 도구의 선언이 네 쌍을 한꺼번에 들고 있게 되어, RBAC가 셋만 허용한
상태에서 그 도구를 어떻게 판정할지 답이 없어진다.

`resource_proxy`가 행사하는 verb는 HTTP 메서드를 따라간다 — `GET`→`get`, `POST`→`create`,
`PUT`→`update`, `PATCH`→`patch`, `DELETE`→`delete`. 따라서 선언은 이 다섯 쌍이며,
**메서드와 무관하게 전부 게이트 대상이다**.

게이트 자신도 권한을 행사한다 — 승인 `context`를 만들고 TOCTOU 프리컨디션을 걸려면 대상의
현재 상태를 읽어야 한다. 그 권한은 도구가 아니라 게이트의 것이므로 따로 선언한다
(`prd-approval-gate` AC11). 다른 곳에서 `get`이 게이트 밖인 것과 어긋나
보이지만, 프록시의 `get`은 쿠버네티스 객체를 읽는 것이 아니라 **클러스터 내부의 임의
엔드포인트에 도달하는 것**이라 성질이 다르다.

이 표가 **게이트의 입력**이다 — 게이트는 도구 이름이 아니라 이 verb로 판정한다
(`prd-approval-gate` AC1). 이전 판에서는 RBAC의 입력이기도 해서 `k8s/rbac.yaml`의 쌍
집합이 이 표와 정확히 같아야 했으나(AC19, 결번), `rbac.yaml`이 `cluster-admin` 바인딩이 된
지금은 대조할 상대가 없다. **표가 좁다는 것이 곧 권한이 좁다는 뜻이 아니다** — 여기 없는
verb도 ServiceAccount는 행사할 수 있고, 막는 것은 게이트뿐이다.

## Acceptance Criteria

### AC1: 좌표로 목록 조회
- **설명**: `resource_list`는 `apiVersion`+`kind`로 임의 종류의 목록을 조회하며 `list`
  verb만 행사한다. `namespace`를 생략하면 전 네임스페이스, 지정하면 해당 네임스페이스로
  좁힌다. `labelSelector`·`fieldSelector`는 서버 사이드로 전달해 apiserver가 걸러낸 결과만
  받는다. 클러스터 스코프 리소스에 `namespace`가 오면 무시하지 않고 거부한다.

  폐기되는 `namespace_list`(`v1`/`Namespace`)와 `workload_list`(`apps/v1`의 세 종류)는 이
  도구의 인자 조합으로 표현된다. 이벤트 조회(`v1`/`Event` + `fieldSelector`)도 마찬가지다.
- **달성 가치**: V1
- **검증 방법**: `v1/Namespace`, `v1/Service`, `apps/v1/Deployment`,
  `networking.k8s.io/v1/Ingress`, `v1/Event`, 임의 CRD에 대해 목록이 반환되고, 셀렉터가
  결과를 실제로 좁힌다.

### AC2: 목록은 표 형식으로 반환
- **설명**: 목록 응답은 apiserver의 Table 표현
  (`Accept: application/json;as=Table;v=v1;g=meta.k8s.io, application/json`)을 사용해
  `kubectl get`과 같은 컬럼 형태로 반환한다. 객체 전문을 JSON으로 덤프하지 않는다. 목록 하나가
  어시스턴트의 컨텍스트를 채워버리면 그다음 판단을 할 여지가 없어지기 때문이다.

  **이 문자열은 손으로 적지 않는다** — 버전과 그룹은 `metav1.SchemeGroupVersion`에서 파생시킨다.
  파라미터가 어긋나면 apiserver는 **오류로 답하지 않는다**. 헤더가 폴백으로 `application/json`을
  함께 제시하므로 평범한 List가 돌아오고, 그 본문은 `metav1.Table`로 전부 0인 값으로 디코드되어
  **모든 좌표에 빈 표**로 나타난다. 실제로 `v=1`(어떤 apiserver도 서빙하지 않는 값)을 들고
  머지 직전까지 간 적이 있다(`doc-tracker/`의 2026-09-14 항목). 그래서 본문이 Table이 아니면
  **거부한다** — 객체를 대신 렌더링하는 폴백 경로는 두지 않는다(그 폴백이 곧 Secret 값을 목록에
  싣는 경로이고, `list`가 게이트 밖인 근거가 AC17이다).
- **달성 가치**: V1
- **검증 방법**: 동일 목록에 대해 Table 응답이 객체 전문 대비 현저히 작고, 컬럼 헤더와 행이
  보존된다. 헤더 문자열은 모양이 아니라 **값으로** 단언하고, 표현을 협상하는 서버(실 apiserver
  또는 협상을 흉내 내는 페이크) 앞에서 확인한다 — 자기가 만든 Table을 먹이는 테스트는 이 계열의
  어긋남을 원리상 볼 수 없다.

### AC3: 목록 절단과 이어보기
- **설명**: `limit`은 기본 100이고 상한은 500이다. **초과 값은 클램프하지 않고 거부한다** —
  AC5가 `tailLines`에 대해 정한 것과 같은 처리이고, 도구 스키마가 `maximum: 500`으로 선언하는
  것도 같은 상한이다. 조용히 낮춘 값으로 답하면 「호출이 통과했다」는 관측만 남고 호출자는
  자기가 요청한 것보다 좁은 창을 봤다는 사실을 끝내 모른다. apiserver가 `continue` 토큰을 주면
  응답에 그대로 실어 다음 페이지를 이어 볼 수 있게 한다. 절단이 일어났다는 사실을 응답에
  명시해, 목록이 전부인 것처럼 오인되지 않게 한다.
- **달성 가치**: V1, V3
- **검증 방법**: 항목이 `limit`보다 많은 종류를 조회하면 절단 표시와 `continue` 토큰이 함께
  오고, 그 토큰으로 다음 페이지가 조회된다. 상한 초과 호출은 거부되고, 거부이므로 apiserver에
  요청이 나가지 않는다.

### AC4: 단건 조회와 잡음 제거
- **설명**: `resource_get`은 `name`을 **필수로** 받아 객체 전문을 반환하되
  `metadata.managedFields`와 `kubectl.kubernetes.io/last-applied-configuration` 어노테이션을
  제거한다. 둘은 객체 크기의 대부분을 차지하면서 운영 판단에 기여하지 않는다.
  `labelSelector`로 대상을 찾아 주는 경로는 두지 않는다 — 그 해석은 `list`를 먼저 행사하는
  것이어서 verb 1:1을 깬다.
- **달성 가치**: V1
- **검증 방법**: 반환된 객체에 위 두 필드가 없고 나머지 `spec`/`status`는 온전하다.
  `name` 없이 호출하면 거부되고, 메시지가 `resource_list`로 먼저 찾으라고 안내한다.

### AC5: 서브리소스 조회
- **설명**: `resource_get`은 `subresource`로 `log`·`scale`·`status`를 받는다. 모두 `get`
  verb이므로 1:1이 유지된다.

  `subresource=log`가 `workload_logs`를 흡수한다. 그 성질을 그대로 가져온다 — `tailLines`는
  기본 200, 허용 1–5000이고 초과 값은 클램프하지 않고 **거부**한다. `previous=true`는 종료된
  직전 컨테이너 인스턴스의 로그를 반환하므로 Running 파드가 없는 크래시 루프에서도 동작한다.
  컨테이너가 둘 이상이면 `container`가 필요하며, `timestamps`·`sinceSeconds`로 형식과 시간
  범위를 조절한다.
- **달성 가치**: V1
- **검증 방법**: `subresource=log`로 최근 로그가 반환되고 `tailLines`가 라인 수를 바꾸며
  5001은 거부된다. 크래시 루프 파드에서 `previous=true`가 직전 인스턴스 로그를 반환한다.
  다중 컨테이너 파드에서 `container` 누락이 거부된다. `subresource=scale`이 현재 레플리카를
  반환한다.

### AC6: 변경 스트림 관측
- **설명**: `resource_watch`는 `watch` verb만 행사한다. 요청·응답 모델에 맞추기 위해
  `watchSeconds`(기본 10, 상한 60) 창 동안 변경 이벤트(`ADDED`/`MODIFIED`/`DELETED`)를 모아
  반환하고 닫는다. 스트림을 호출에 걸쳐 유지하지 않는다. `resourceVersion`을 받아 그 시점
  이후의 변경만 받을 수 있고, 이벤트 수에 상한을 두어 폭주하는 리소스가 응답을 채우지
  못하게 한다.

  롤아웃이 끝나기를 기다리거나 파드가 Ready로 가는지 보는 데 쓴다. `resource_list`를
  반복 호출하는 것보다 정확하고 싸다.

  **민감 종류에 대해서는 게이트를 탄다.** 변경 스트림은 객체 전문을 밀어 주므로
  Secret을 `watch`하면 `data`가 그대로 흘러나온다 — `get`과 같은 노출이므로 같은 취급을
  받는 것이 맞다. 스트림이라는 이유로 금지하는 대신 게이트를 건다.
- **달성 가치**: V1, V3
- **검증 방법**: Deployment를 `watch`하는 동안 레플리카를 바꾸면 `MODIFIED` 이벤트가 창
  안에서 관측된다. `watchSeconds=61`이 거부된다. `kind=Secret`은 미승인 시 거부되고,
  승인 후에도 이벤트 수·창 상한이 그대로 적용된다.

### AC7: 생성
- **설명**: `resource_create`는 매니페스트를 받아 객체를 만들며 `create` verb만 행사한다.
  인자는 `manifest` 하나다: JSON 객체 또는 YAML/JSON 문자열을 받으며, 문자열의 여러
  문서는 `---`로 구분한다. 각 문서는 `apiVersion`·`kind`·`metadata.name`을 명시하고,
  네임스페이스 범위 객체는 `metadata.namespace`도 명시한다(기본값을 추측하지 않는다).
  `generateName`만으로는 승인 화면의 대상 이름을 확정할 수 없어 거부한다.
  이미 존재하면 409를 그대로 알리고 덮어쓰지 않는다 — 덮어쓰기는 `update`나 `patch`의
  일이고, 생성 승인을 받은 호출이 갱신까지 하면 운영자가 승인한 것과 달라진다.
  여러 문서가 담긴 매니페스트는 문서마다 별도 승인을 받는다.

  **문서 수만큼 승인을 받는 것은 이 도구의 재량이 아니다.** `prd-approval-gate` AC7이
  "승인 하나는 단 한 번의 쿠버네티스 API 호출만 인가한다"이고 문서 하나가 곧 `create`
  호출 하나이므로, 한 승인으로 두 문서를 만드는 길은 애초에 없다. 위 문장은 그 따름정리다.

  **승인은 전부 먼저 받고, 그 다음에 첫 객체를 만든다.** 문서 순서대로 N개의 승인 요청을
  만들고, 전부 승인된 뒤에야 apiserver를 부른다. 하나라도 거절되거나 시간이 다하면
  **아무것도 만들지 않고** 호출 전체를 거부한다 — 운영자가 거절한 문서가 있는데 다른
  문서는 이미 서 있다면, 운영자가 승인 화면에서 내린 판단과 클러스터의 상태가 어긋난다.
  이 순서 덕에 **거절로 인한 반쪽 상태는 발생하지 않는다.** 받아 두고 쓰지 않은 승인은
  소비되지 않은 채 남으며, `prd-approval-gate` AC7대로 재사용되지 않는다.

  **승인을 다 받은 뒤 실행 도중 실패하면 거기서 멈추고 되돌리지 않는다.** k번째 문서가
  409나 그 밖의 이유로 실패하면 나머지 문서는 시도하지 않는다. 이미 만들어진 k−1개는
  **삭제하지 않는다** — 되돌리려면 이 서버가 승인 없는 `delete`를 행사해야 하는데,
  `prd-approval-gate` AC1은 상태를 바꾸는 verb를 예외 없이 게이트 대상으로 두고 AC7은
  승인 하나에 호출 하나만 허락한다. 뒷정리를 위해 게이트를 한 번 건너뛰는 것도 게이트를
  건너뛰는 것이고, 그런 예외는 그 자체가 우회 경로가 된다. 되돌리는 대신 응답이
  **만들어진 것 · 실패한 것 · 시도하지 않은 것**을 각각 좌표로 보고해, 운영자가 무엇이
  남았는지 읽고 다음 수를 직접 고르게 한다.

  응답의 `structuredContent`는 문서 순서의 `created` 배열, 실패 좌표와 `error`를 담은
  `failed` 객체(전부 성공하면 null), `unattempted` 배열로 구분한다. 좌표는
  `apiVersion`·`kind`·`namespace`·`name`이며 매니페스트 값은 응답에 다시 담지 않는다.
  실패하면 `isError=true`다. 네트워크 오류나 서버 5xx는 생성 여부가 불확실할 수 있으므로
  그 사실을 알리고, 대상 확인 후 새 승인으로 다음 작업을 정한다. 자동 재시도하지 않는다.

  **`create`의 TOCTOU 프리컨디션은 apiserver의 409다.** `prd-approval-gate` AC6은 승인
  시점에 읽은 `resourceVersion`을 실행 직전에 재확인하라고 하지만, `create`에는 승인
  시점에 읽을 상태가 없다 — 대상이 아직 없는 것이 정상 경로다. 그 자리를 대신하는 것은
  이 AC가 이미 요구한 것, 즉 **"이미 존재하면 덮어쓰지 않고 409"** 이며, 그 판정은
  apiserver의 `create`가 원자적으로 내린다. 그래서 게이트는 `create` 대상을 미리 읽어
  두지 않고(읽을 것이 없다), 승인 `context`의 상세는 **매니페스트 자체**에서 만든다
  (`prd-approval-gate` AC3의 `create` 항목이 이미 그렇게 적혀 있다). 다중 문서에서는
  승인 하나의 `context`가 **그 문서 하나만** 기술한다 — 승인 버튼 하나가 인가하는 것이
  그 문서 하나이기 때문이다.
- **달성 가치**: V1, V3
- **검증 방법**: 신규 객체가 생성되고, 기존 이름으로 재호출하면 409로 거부된다.
  다중 문서 매니페스트에서 문서 수만큼 승인 요청이 생성된다.
  2문서 매니페스트에서 **둘째 문서의 승인을 거절하면 첫째 문서의 객체도 만들어지지
  않는다**(쿠버네티스 호출 0건). 둘 다 승인한 뒤 둘째가 409로 실패하면 첫째는 그대로
  남고, 응답이 만들어진 것·실패한 것·시도하지 않은 것을 구분해 보고한다. 승인 후 실행
  전에 같은 이름의 객체가 외부에서 생기면, 게이트의 재확인이 아니라 **apiserver의
  409**로 거부된다.

### AC8: 전체 교체
- **설명**: `resource_update`는 매니페스트로 객체를 통째로 교체하며(PUT) `update` verb만
  행사한다. `subresource=scale`을 받아 레플리카를 바꾸며, 이것이 `workload_scale`을
  흡수한다. 그 성질을 그대로 가져온다 — `replicas=0`도 허용하고, 음수와 누락은 거부하며,
  DaemonSet처럼 레플리카 개념이 없는 종류는 거부하되 사유가 권한이나 존재 여부가 아니라
  **그 종류에 레플리카가 없다는 사실**임을 밝힌다.
- **달성 가치**: V1, V3
- **검증 방법**: 지정 레플리카가 반영되고 `replicas=0`도 적용된다. 음수·누락이 거부된다.
  DaemonSet 요청이 레플리카 부재를 사유로 거부된다.

### AC9: 부분 수정
- **설명**: `resource_patch`는 `patch` verb만 행사하며 `patchType`으로
  `merge`·`strategic`·`json`·`apply`를 받는다. `apply`는 Server-Side Apply
  (`application/apply-patch+yaml`)이며 `fieldManager`를 함께 받는다.

  네 형식 모두 게이트가 읽은 `resourceVersion`·`uid`에만 조건부로 실행한다
  (`prd-approval-gate` AC6). `merge`·`strategic`은 `metadata`에 두 값을 붙이고,
  `apply`는 Server-Side Apply가 applied config의 `uid`를 immutable 필드로 거부하므로
  `resourceVersion`만 붙인다. 호출자가 이미 적은 값이 승인 상태와 다르면 네 형식 모두
  거부한다. `json`은 원래 연산의
  앞뒤에 두 값의 `test`를 추가해 현재 대상과 연산 결과의 조건이 모두 유지돼야 한다.
  원래 데이터 변경과 JSON 숫자는 보존한다. `metadata`를 객체가 아닌 값으로 지우거나,
  `strategic`의 루트·`metadata`에 `$` 지시어를 넣어 조건을 제거하는 요청은 거부한다.
  `spec` 등 그 밖의 위치에서 쓰는 strategic 지시어는 그대로 전달한다.

  `apply`도 기존 대상에만 쓴다 — 승인된 `resourceVersion`·`uid`를 게이트가 실행
  직전에 다시 읽어 대조하므로(`prd-approval-gate` AC6), 사라진 대상을 다시 만드는
  경로가 되지 않는다.
  apply 본문은 apiserver가 applied config '객체'로 해석해 본문의 gvk에서 타입을 찾는
  유일한 patch 본문이므로, 좌표의 `apiVersion`·`kind`를 호출자가 쓰지 않았을 때만
  본문에 채워 보내고, 호출자가 좌표와 다른 값을 쓰면 `resourceVersion`·`uid`와
  똑같이 전송 전에 거부한다.
  `fieldManager`는 호출자가 고른 값을 유지하고 `force`를 보내지
  않는다. 버전·필드 소유권 충돌 또는 JSON `test` 실패는 현재 호출의 **자동 거부**로
  끝난다. 버전을 새로 읽어 대체하거나, 같은 승인으로 재시도하거나, 자동 재승인을
  요청하지 않는다. HTTP `Retry-After`도 재시도하지 않는다. 네트워크·5xx 실패는 결과가
  불확실할 수 있음을 알리고, 새로운 호출은 별도 승인이 필요하다.

  롤링 재시작은 이 도구로 한다 — 파드 템플릿에 `kubectl.kubernetes.io/restartedAt`
  어노테이션을 얹는 `strategic` 패치다. 이것이 `workload_restart`를 흡수하며, 전용 도구를
  두지 않는 이유는 재시작이 verb가 아니라 **특정 내용의 patch**이기 때문이다. 게이트도
  내용이 아니라 verb로 판정하므로, 재시작은 다른 patch와 똑같이 승인을 받는다.
- **달성 가치**: V1, V3
- **검증 방법**: 네 `patchType`이 각각 동작한다. 재시작 어노테이션 패치 후 파드가 교체되고
  `spec.replicas`와 나머지 스펙은 호출 전과 동일하다.

### AC10: 삭제
- **설명**: `resource_delete`는 **단일 객체**를 삭제하며 `delete` verb만 행사한다.
  `name`이 필수이고 `gracePeriodSeconds`를 받는다. 셀렉터로 여러 개를 지우는 것은 이
  도구가 아니라 `resource_delete_collection`(AC11)의 일이다 — verb가 다르므로 도구도 다르다.
- **달성 가치**: V1, V3
- **검증 방법**: 지정 객체만 삭제된다. `name` 없이 셀렉터만으로 호출하는 경로가 이 도구에
  존재하지 않는다.

### AC11: 컬렉션 일괄 삭제
- **설명**: `resource_delete_collection`은 `deletecollection` verb만 행사하며
  `labelSelector`·`fieldSelector`로 대상을 정한다. **`namespace`는 필수다** — 전 네임스페이스
  일괄 삭제 경로를 두지 않는다.

  승인 `context`에는 **삭제될 대상의 수와 이름 목록**이 들어간다(이름이 많으면 앞 20개와
  총 개수). 몇 개가 지워지는지 모르는 승인은 승인이 아니기 때문이다. 그 수를 세려면
  승인 요청 전에 `list`를 해야 하는데, 그 `list`는 이 도구가 아니라 **게이트가 행사하는
  권한**이다(`prd-approval-gate` AC11).

  셀렉터가 0건을 가리키면 승인 요청을 만들지 않고 그대로 알린다 — 아무것도 지우지 않는
  일에 사람을 부르지 않는다.
- **달성 가치**: V1, V3
- **검증 방법**: 셀렉터에 맞는 객체만 사라지고 나머지는 남는다. `context`에 대상 수와 이름이
  담긴다. `namespace` 누락이 거부된다. 0건 셀렉터는 승인 요청 없이 알린다. 승인과 실행
  사이에 대상이 늘면 실행이 거부된다(`prd-approval-gate` AC6).

### AC12: 컨테이너 안에서 명령 실행
- **설명**: `resource_exec`은 `create` on `pods/exec`만 행사한다. 대상 파드·컨테이너와
  명령 배열을 받아 새 프로세스를 띄우고 stdout/stderr를 반환한다. 컨테이너가 둘 이상이면
  `container`가 필요하다. 출력에는 바이트 상한과 실행 시간 상한을 둔다 — 무한히 출력하는
  명령이 응답을 채우지 못하게 한다.
- **달성 가치**: V1, V3
- **검증 방법**: 명령이 실행되고 stdout/stderr가 구분되어 반환된다. 다중 컨테이너 파드에서
  `container` 누락이 거부된다. 상한을 넘기는 출력이 잘리고 잘렸음이 응답에 표시된다.

### AC13: 실행 중 컨테이너의 stdio에 접속
- **설명**: `resource_attach`는 `create` on `pods/attach`만 행사한다. `exec`과 달리 새
  프로세스를 띄우지 않고 **이미 돌고 있는 주 프로세스의 스트림에 붙는다**. 요청·응답
  모델에 맞추기 위해 `readSeconds`(기본 5, 상한 30) 동안 출력을 모아 반환하며,
  `stdin`이 주어지면 붙은 직후 한 번 써 넣는다. AC12와 같은 이유로 **출력에는 스트림당
  256KiB의 바이트 상한**을 두고, 상한에 걸리면 응답의 `stdoutTruncated`/`stderrTruncated`가
  참이 된다 — 시간 상한만으로는 끝없이 출력하는 주 프로세스를 막지 못한다.

  이 도구가 `exec`과 같은 등급인 이유는 권능이 같기 때문이다 — 대화형 셸이 PID 1로 도는
  파드에서는 `attach`로 stdin을 보내는 것이 `exec`으로 셸을 여는 것과 구별되지 않는다.
- **달성 가치**: V1, V3
- **검증 방법**: 출력을 내보내는 파드에 붙어 `readSeconds` 동안의 출력이 반환된다.
  `readSeconds=31`이 거부된다. `stdin`이 대상 프로세스에 전달된다. 상한을 넘기는 출력이
  잘리고 잘렸음이 응답에 표시된다.

### AC14: 포트 포워드
- **설명**: `resource_port_forward`는 `create` on `pods/portforward`만 행사한다. MCP는
  요청·응답 모델이고 게이트는 1승인 1실행(`prd-approval-gate` AC7)이므로, **터널을 걸쳐
  유지하지 않는다** — 한 번의 호출이 터널을 열고, 주어진 페이로드를 보내고, 응답을 읽고,
  닫는 단발 왕복이다. 지속 세션이 필요하면 그것은 이 서버가 아니라 `kubectl port-forward`의
  일이다.

  `port`와 보낼 바이트를 받고, 읽기에 바이트·시간 상한을 둔다. 이 도구는 네트워크 정책이
  막아 둔 파드 포트에 도달할 수 있으므로, 승인 `context`에 **포트와 페이로드가 그대로**
  들어가야 한다(`prd-approval-gate` AC3).
- **달성 가치**: V1, V3
- **검증 방법**: 픽스처 파드의 포트로 요청을 보내 응답을 받는다. 호출이 끝나면 터널이
  닫혀 있다(같은 승인으로 두 번째 왕복이 불가능하다). 상한 초과가 거부된다.

### AC15: 프록시
- **설명**: `resource_proxy`는 apiserver를 거쳐 대상의 HTTP 엔드포인트에 도달하며,
  행사하는 verb는 HTTP 메서드를 따라간다. `kind`는 `Pod`·`Service`·`Node`를 받고,
  **경로 제한을 두지 않는다.** 모든 메서드와 모든 경로가 게이트를 탄다.

  `Node` 대상은 kubelet API에 직접 닿는다 — `/healthz`·`/metrics`·`/pods` 같은 관측
  엔드포인트도, `/exec`·`/attach`·`/portForward`·`/logs` 같은 고권한 엔드포인트도 같은
  쌍(`⟨메서드 verb⟩ on nodes/proxy`)으로 표현된다. 그래서 **이 도구에 한해
  `(verb, resource)` 쌍은 호출이 무엇을 하는지 한정하지 못한다.** 한정하는 것은 경로이며,
  경로는 게이트 `context`에만 나타난다(`prd-approval-gate` AC3).

  이것은 결함이 아니라 선택이다. 경로 허용목록을 두면 "이 경로는 봐준다"는 분류기가
  하나 더 생기고, 그 분류기가 곧 우회 경로가 된다 — 같은 이유로 패치 내용을 보지 않기로
  했다(AC9). 대신 게이트가 **경로를 숨기지 않고** 그대로 보여 운영자가 판단한다.

  게이트는 kubelet의 고권한 엔드포인트(`/exec`·`/attach`·`/portForward`·`/run`·`/logs`·
  `/containerLogs`)를 `context`에 **표시**한다. 막지 않는다 — 승인 화면에서 눈에 띄게
  하는 것뿐이다.

  요청·응답 모델에 맞추기 위해 `readSeconds`(기본 10, 상한 30) 동안 응답을 읽고 닫으며,
  상한을 넘는 값은 클러스터에 닿기 전에 거부된다. 다른 셋과 달리 이 도구의 창은 **경로
  허용목록을 두지 않기로 한 선택의 따름결과**다 — `/containerLogs?follow=true`처럼 끝나지
  않도록 **설계된** 엔드포인트가 그대로 열려 있으므로, 이 왕복을 끝내는 것은 시간뿐이다.
  AC12·AC13과 같은 이유로 **본문에는 256KiB의 바이트 상한**을 두고, 상한에 걸리면 응답의
  `truncated`가 참이 된다.

  응답은 대상 자신의 HTTP 상태(`status`)와 본문(`body`)에 더해 `bodyEncoding`과 실제로 쓴
  창 `readSeconds`를 함께 돌려준다. `bodyEncoding`은 본문이 UTF-8 그대로인지(`utf-8`)
  base64로 감싼 바이트인지(`base64`)를 말한다 — 이 도구는 `/metrics`의 텍스트도 이진
  페이로드도 같은 자리로 돌려주고, **잘린 본문과 이진 본문은 말해 주지 않으면 온전한 본문과
  구별되지 않는다.**
- **달성 가치**: V1, V3
- **검증 방법**: `Pod`·`Service`·`Node` 대상의 `GET`·`POST`가 동작하고 각각 `get`·`create`
  쌍으로 게이트를 탄다. 승인 없는 `GET`도 거부된다. `nodes/proxy`의 `/exec` 경로 호출이
  **실행되되** 그 승인 요청 `context`에 경로 전문과 고권한 표시가 함께 담긴다.
  `readSeconds=31`이 **클러스터 호출 없이** 거부되고, 미지정이면 기본 10이 쓰인다. 상한을
  넘기는 본문이 잘리고 `truncated`로 그렇게 말하며, 이진 본문이 `bodyEncoding=base64`로
  표시된다.

### AC16: 민감 종류는 읽기도 쓰기도 승인 게이트를 거친다
- **설명**: `v1/Secret`을 비롯한 **민감 종류**(`RESOURCE_GATED_KINDS`, 기본 `v1/Secret`)를
  대상으로 하는 호출은 verb와 무관하게 게이트를 통과해야만 실행된다 — 읽기(`get`·`watch`)도,
  쓰기(`create`·`update`·`patch`·`delete`·`deletecollection`)도 같다. 쓰기는 원래 모든
  종류에서 게이트 대상이므로 실질적인 추가는 읽기 쪽이다.

  `list`는 예외다. Table 표현이 apiserver에서 만들어져 값이 전송되지 않으므로(AC17)
  게이트를 타지 않는다.

  **쓰기의 `context`에서는 값을 가린다.** 매니페스트나 패치 본문의 `data`·`stringData`는
  키 이름과 각 값의 바이트 수로 대체된다 — "ns/name에 Secret 생성, 키 `token`(32B)·
  `ca.crt`(1834B)". 값을 그대로 실으면 승인 화면과 푸시 알림이 자격증명 유출 경로가 되고,
  그러면 거절해도 이미 본 것이 된다. 이는 다른 종류의 `patch`가 본문 전문을 싣는 것과
  다른데(AC9), 그 차이의 근거는 **내용이 자격증명인지**이지 무엇을 하는 호출인지가 아니다.

  이전 판은 Secret 쓰기를 RBAC 미부여로 **금지**했다. 그 근거("Secret은 읽기만 연다")는
  결론을 다시 쓴 것이었고, 실제로 쓰기는 그 자체로 값을 유출하지 않는다. 자격증명
  로테이션은 실재하는 운영 필요이며, 막으면 운영자가 `kubectl`로 우회해 아무 기록도 남지
  않는다 — 읽기를 허용한 바로 그 논거다.
- **달성 가치**: V3
- **검증 방법**: `kind=Secret`의 `get`·`watch`·`create`·`update`·`patch`·`delete`가 모두
  미승인 시 거부되고 k8s 호출 카운트가 0이다. `list`는 승인 없이 동작한다. 쓰기 승인 요청의
  `context`에 키 이름과 바이트 수만 있고 값이 없다.

### AC17: 값이 새는 경로 봉쇄
- **설명**: 읽기를 허용한 이상 값이 **승인 없이 보이는 경로**와 **응답 밖에 남는 경로**를
  함께 막아야 한다.
  - **`resource_list`는 값을 담지 않는다** — 목록은 AC2의 Table 표현으로만 응답한다.
    Secret의 Table은 apiserver가 서버 사이드로 만들어 `NAME`/`TYPE`/`DATA`/`AGE`만 담으므로
    값이 애초에 전송되지 않는다. 원시 목록으로 떨어지는 폴백 경로를 두지 않는다.
  - **`watch`도 민감 종류 게이트를 탄다** — 변경 스트림은 객체 전문을 밀어 주므로 민감
    종류를 `watch`하면 `data`가 그대로 흘러나온다. `get`과 같은 노출이므로 같은 취급을
    받는다(AC6). 스트림이라는 이유로 금지하는 대신 게이트를 건다.
  - **스트림 서브리소스 넷은 모두 자격증명에 닿는다** — `exec`은
    `/var/run/secrets/**`를 읽고, `attach`는 그 위에서 도는 셸의 stdio를 잡고,
    `port_forward`와 `proxy`는 파드가 서빙하는 내부 API에 도달한다. 넷 다 RBAC로는 내용을
    가릴 수 없으므로 **게이트가 유일한 방어선**이고, 그래서 승인 `context`에 명령·페이로드·
    경로가 전문으로 들어가야 한다(`prd-approval-gate` AC3).
  - **승인된 값의 사후 확산** — `context`·감사 로그·에러 메시지 어디에도 값을 남기지
    않는다(`prd-approval-gate` AC10).
  - **Secret 쓰기** — 변경 verb는 게이트 대상이고, 그것이 전부다. 이전 판은 여기에
    "`k8s/rbac.yaml`이 `secrets`에 쓰기 verb를 주지 않으므로 권한 밖으로 떨어진다"를
    덧붙였는데, 그 2차 방어선은 없다(`rbac.yaml`은 `cluster-admin` 바인딩이다).
  - **⚠️ ServiceAccount 토큰은 게이트 밖이다** — 게이트는 이 서버의 코드 경로에 걸려
    있으므로 **apiserver를 직접 치는 호출에는 걸리지 않는다.** 그리고 위에 적었듯
    `resource_exec`은 `/var/run/secrets/**`를 읽는다 — 승인이 한 번 나면 그 응답에 SA
    토큰이 담기고, 그 토큰은 이제 `cluster-admin`이다. 이전 판에서는 같은 유출이
    `rbac.yaml`의 좁은 쌍 집합으로 상한이 걸렸다. **지금은 상한이 없다.**
    `resource_exec`·`resource_attach` 승인은 그 사실 위에서 판단한다.
- **달성 가치**: V3
- **검증 방법**: `kind=Secret`의 `resource_list`가 승인 없이 동작하되 값을 담지 않는다.
  원시 목록 폴백 경로가 코드에 존재하지 않는다. 스트림 도구 넷이 전부 미승인 시 거부된다.
  `watch=true` 직접 호출은 **이 서버를 거치지 않으므로 단정하지 않는다** — 이전 판의
  "403이다"는 `secrets`에 `watch` 미부여를 전제했고 그 전제가 사라졌다.

### AC18: 403의 정직한 보고
- **설명**: apiserver가 403을 주면 그대로 흘리지 않고, 권한 밖임을 밝히는 에러로 변환한다.
  에러 메시지는 **어떤 `(verb, resource)` 쌍이 막혔는지**를 담아 운영자가 곧바로 대조할 수
  있게 한다.

  **정상 배포에서 이 경로는 돌지 않는다.** `rbac.yaml`이 `cluster-admin` 바인딩이므로
  403이 나올 조합이 없다. 그래도 AC를 남기는 이유는 이 변환이 **바인딩이 없거나 좁혀진
  상태**(프리뷰 오버레이의 개명 실패, 부분 적용, 누군가 다시 좁힌 경우)에서만 도는 진단
  경로이기 때문이다 — 그때 재시도를 유도하는 모호한 실패 대신 원인을 말해야 한다.
  이전 판은 여기에 "권한 범위는 `k8s/rbac.yaml`이 단일 출처다"를 적었는데, 그 파일이 더는
  범위를 정하지 않으므로 지웠다.
- **달성 가치**: V3
- **검증 방법**: 403을 받은 호출이 재시도를 유도하지 않는 명시적 에러를 반환하고, 그
  메시지에 막힌 `(verb, resource)` 쌍이 담긴다. 403은 주입해서 관측한다 — 배포된 권한으로는
  자연 발생하지 않는다.

### AC20: 종류 해석과 미지원 종류 거부
- **설명**: `api_resources`는 discovery API로 클러스터가 실제로 제공하는 종류 목록
  (group/version/kind/plural/namespaced 여부)을 반환한다. 리소스 권한을 행사하지 않으므로
  쌍 표에 없다. 다른 도구들은 이 discovery 결과로 `kind`를 실제 리소스 경로로 해석하며,
  클러스터에 없는 종류는 추측해 호출하지 않고 거부한다. 거부 메시지에는 비슷한 이름의
  실존 종류를 함께 제시한다.
- **달성 가치**: V1
- **검증 방법**: 존재하지 않는 `kind`가 거부되고 후보가 제시된다. CRD를 설치하면
  `api_resources`에 나타나고 곧바로 `resource_list`로 조회된다.
