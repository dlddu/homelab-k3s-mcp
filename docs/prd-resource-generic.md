# PRD: resource_* (generic resource 도구군)

특정 워크로드 종류에 묶이지 않고 **리소스 좌표**(`apiVersion` + `kind` + `namespace` + `name`)로
임의의 쿠버네티스 리소스를 다루는 도구군.

이 도구군은 기존 V1 도구 6종(`namespace_list`, `workload_list`, `workload_logs`,
`pod_describe`, `workload_restart`, `workload_scale`)을 **대체하고 폐기한다**. 그 6종은
Deployment/StatefulSet/DaemonSet과 Pod만 알았기 때문에 Service·Ingress·ConfigMap·PVC·CRD를
다루려면 매번 도구를 새로 만들어야 했다. 종류를 입력으로 받으면 그 반복이 끝난다.

## 달성 가치

- **V1: 자연어로 클러스터 운영** — `kubectl`이 다루는 리소스 표면 대부분을 자연어로 연다.
  6종이 덮던 범위는 전부 유지하면서(AC1·AC5·AC6·AC9·AC10), 종류 제약만 걷어낸다.
- **V3: 안전한 운영(Safe-by-default)** — 표면이 넓어진 만큼 경계를 함께 세운다.
  Secret은 **읽는 것조차** 사람 승인을 거치고(AC11), 그 값이 응답 밖으로 새지 않으며(AC12),
  되돌리기 어려운 변경도 승인을 거친다(AC13).

## 도구 개요

| 도구 | 행사하는 RBAC 권한 | 게이트 | 대체하는 도구 |
|------|---------------------|--------|----------------|
| `resource_list` | `list` on ⟨kind⟩ | — | `namespace_list`, `workload_list` |
| `resource_get` | `get` on ⟨kind⟩ | 읽기 게이트 종류만 | — |
| `resource_logs` | `get` on `pods/log` | — | `workload_logs` |
| `resource_describe` | `get` on ⟨kind⟩ + `list` on `events` | 읽기 게이트 종류만 | `pod_describe` |
| `api_resources` | discovery (리소스 권한 아님) | — | — |
| `resource_apply` | `patch` + `create` on ⟨kind⟩ | O | — |
| `resource_delete` | `delete` on ⟨kind⟩ | O | — |
| `resource_scale` | `update` on ⟨kind⟩`/scale` | — | `workload_scale` |
| `resource_restart` | `patch` on `apps/v1` 워크로드 | — | `workload_restart` |
| `resource_exec` | `create` on `pods/exec` | O | — |

이 표가 **RBAC의 입력이자 게이트의 입력**이다. `k8s/rbac.yaml`은 여기 적힌 쌍의 합집합과
정확히 같아야 하며, 그보다 넓으면 AC15 위반이고 좁으면 해당 도구가 AC14의 "권한 밖"으로
떨어진다. 도구 이름이 아니라 이 쌍으로 판정하는 이유는 `prd-approval-gate`의
"게이트 대상"에 적었다.

어노테이션: 읽기 5종은 `readOnlyHint=true`, `destructiveHint=false`.
변경 5종은 `readOnlyHint=false`, `destructiveHint=true`.

`resource_logs`·`resource_describe`·`resource_restart`를 따로 두는 이유는 이들이
`resource_get`/`resource_apply`로 대체되지 않기 때문이다. 로그는 `pods/log` 서브리소스라
객체 조회 경로에 없고, 진단은 객체와 이벤트를 합쳐야 의미가 생기며(따로 부르면 왕복이
세 번이다), 롤링 재시작은 파드 템플릿 어노테이션 패치이지 전체 매니페스트 재적용이 아니다.
재적용으로 흉내 내면 의도하지 않은 필드까지 되돌릴 위험이 있다.

## Acceptance Criteria

### AC1: 좌표로 목록 조회
- **설명**: `resource_list`는 `apiVersion`+`kind`로 임의 종류의 목록을 조회한다.
  `namespace`를 생략하면 전 네임스페이스, 지정하면 해당 네임스페이스로 좁힌다.
  `labelSelector`·`fieldSelector`는 서버 사이드로 전달해 apiserver가 걸러낸 결과만 받는다.
  클러스터 스코프 리소스에 `namespace`가 오면 무시하지 않고 거부한다.
  `namespace_list`(`v1`/`Namespace`)와 `workload_list`(`apps/v1`의 세 종류)가 하던 일은
  이 도구의 인자 조합으로 표현된다.
- **달성 가치**: V1
- **검증 방법**: `v1/Namespace`, `v1/Service`, `apps/v1/Deployment`,
  `networking.k8s.io/v1/Ingress`, 임의 CRD에 대해 목록이 반환되고, 셀렉터가 결과를 실제로 좁힌다.

### AC2: 목록은 표 형식으로 반환
- **설명**: 목록 응답은 apiserver의 Table 표현
  (`Accept: application/json;as=Table;v=1;g=meta.k8s.io`)을 사용해 `kubectl get`과 같은
  컬럼 형태로 반환한다. 객체 전문을 JSON으로 덤프하지 않는다. 목록 하나가 어시스턴트의
  컨텍스트를 채워버리면 그다음 판단을 할 여지가 없어지기 때문이다.
- **달성 가치**: V1
- **검증 방법**: 동일 목록에 대해 Table 응답이 객체 전문 대비 현저히 작고, 컬럼 헤더와 행이
  보존된다.

### AC3: 목록 절단과 이어보기
- **설명**: `limit`은 기본 100, 상한 500으로 강제한다. apiserver가 `continue` 토큰을 주면
  응답에 그대로 실어 다음 페이지를 이어 볼 수 있게 한다. 절단이 일어났다는 사실을 응답에
  명시해, 목록이 전부인 것처럼 오인되지 않게 한다.
- **달성 가치**: V1, V3
- **검증 방법**: 항목이 `limit`보다 많은 종류를 조회하면 절단 표시와 `continue` 토큰이 함께
  오고, 그 토큰으로 다음 페이지가 조회된다.

### AC4: 단건 조회의 잡음 제거
- **설명**: `resource_get`은 객체 전문을 반환하되 `metadata.managedFields`와
  `kubectl.kubernetes.io/last-applied-configuration` 어노테이션을 제거한다. 둘은 객체 크기의
  대부분을 차지하면서 운영 판단에 기여하지 않는다.
- **달성 가치**: V1
- **검증 방법**: 반환된 객체에 위 두 필드가 없고, 나머지 `spec`/`status`는 온전하다.

### AC5: 컨테이너 로그 조회
- **설명**: `resource_logs`는 대상 파드의 컨테이너 로그를 반환한다. `workload_logs`가 가지던
  성질을 그대로 유지한다 — `tailLines`는 기본 200, 허용 1–5000이고 초과 값은 클램프하지 않고
  **거부**한다. `previous=true`는 종료된 직전 컨테이너 인스턴스의 로그를 반환하므로 Running
  파드가 없는 크래시 루프에서도 동작한다. 컨테이너가 둘 이상이면 `container`가 필요하며,
  `timestamps`·`sinceSeconds`로 형식과 시간 범위를 조절한다.
- **달성 가치**: V1
- **검증 방법**: 정상 워크로드에서 최근 로그가 반환되고, `tailLines`가 라인 수를 바꾸며,
  5001은 거부된다. 크래시 루프 파드에서 `previous=true`가 직전 인스턴스 로그를 반환한다.
  다중 컨테이너 파드에서 `container` 누락이 거부된다.

### AC6: 진단 스냅샷
- **설명**: `resource_describe`는 대상 객체의 요약과 최근 이벤트를 **한 응답에** 담아
  반환한다. 파드가 대상이면 메타데이터, 컨테이너 상태(state·reason·restart count·exit code),
  conditions를 담아 `pod_describe`의 성질을 그대로 유지한다. 이벤트 조회 권한이 없으면 빈
  이벤트 배열로 동작하며 실패하지 않는다(best-effort).

  파드 외의 종류도 대상이 된다. **Secret이 대상이면 키 이름과 각 값의 바이트 수만 반환하고
  값 자체는 담지 않는다**(`kubectl describe secret`과 같은 동작). 승인을 받아도 이 도구는
  값을 내주지 않는다 — 값이 필요하면 `resource_get`을 따로 승인받는다. 무엇이 들어 있는지
  확인하는 일과 값을 꺼내는 일을 다른 승인으로 가르기 위해서다.
- **달성 가치**: V1, V3
- **검증 방법**: 파드 스냅샷에 위 필드가 모두 담긴다. 이벤트 권한이 없는 상황에서도 스냅샷이
  (빈 이벤트로) 정상 반환된다. Secret 대상에서는 키 이름과 바이트 수가 나오고 값은 나오지
  않는다.

### AC7: 대상 해석
- **설명**: `resource_logs`와 `resource_describe`는 `name` / `labelSelector` /
  `workloadKind`+`workloadName` 중 **정확히 하나**로 파드를 해석한다. 둘 이상을 함께 주면
  거부한다. 셀렉터·워크로드 경로는 첫 Running 파드를 우선하되, Running이 없으면 매칭 파드를
  사용한다(AC5의 크래시 루프 동작이 여기에 의존한다).
- **달성 가치**: V1
- **검증 방법**: 세 경로가 각각 같은 파드로 해석된다. 두 경로를 동시에 지정하면 거부된다.

### AC8: 종류 해석과 미지원 종류 거부
- **설명**: `api_resources`는 discovery API로 클러스터가 실제로 제공하는 종류 목록
  (group/version/kind/plural/namespaced 여부)을 반환한다. 다른 도구들은 이 discovery 결과로
  `kind`를 실제 리소스 경로로 해석하며, 클러스터에 없는 종류는 추측해 호출하지 않고 거부한다.
  거부 메시지에는 비슷한 이름의 실존 종류를 함께 제시한다.
- **달성 가치**: V1
- **검증 방법**: 존재하지 않는 `kind`가 거부되고 후보가 제시된다. CRD를 설치하면
  `api_resources`에 나타나고 곧바로 `resource_list`로 조회된다.

### AC9: 레플리카 설정과 레플리카 없는 종류 거부
- **설명**: `resource_scale`은 `spec.replicas`를 지정한 값으로 설정하며 0으로의 스케일다운도
  허용한다. 음수와 누락은 거부한다. DaemonSet처럼 레플리카 개념이 없는 종류는 거부하고,
  거부 메시지가 권한이나 존재 여부가 아니라 **그 종류에 레플리카가 없다는 사실**을 밝힌다.
  `workload_scale`의 성질을 그대로 유지한다.
- **달성 가치**: V1, V3
- **검증 방법**: 지정 레플리카 수가 반영되고 `replicas=0`도 적용된다. 음수·누락이 거부된다.
  DaemonSet 요청이 레플리카 부재를 사유로 거부된다.

### AC10: 롤링 재시작
- **설명**: `resource_restart`는 파드 템플릿에
  `kubectl.kubernetes.io/restartedAt` 어노테이션을 패치해 롤링 재시작을 유발한다. 전체
  매니페스트를 재적용하지 않으므로 **그 어노테이션 외의 어떤 필드도 바뀌지 않으며**,
  특히 레플리카 수가 보존된다. `workload_restart`의 성질을 그대로 유지한다.
- **달성 가치**: V1, V3
- **검증 방법**: 재시작 후 어노테이션 타임스탬프가 갱신되고 파드가 교체되며,
  `spec.replicas`와 나머지 스펙이 호출 전과 동일하다.

### AC11: Secret 읽기는 승인 게이트를 거친다
- **설명**: `v1/Secret`을 비롯한 **읽기 게이트 종류**(`RESOURCE_READ_GATED_KINDS`, 기본
  `v1/Secret`)를 대상으로 하는 `resource_get`·`resource_describe`는
  `prd-approval-gate`의 게이트를 통과해야만 실행된다. 미승인 시 쿠버네티스 API를 호출하지
  않는다.

  이는 이전 설계(모든 동사에서 Secret 거부)를 대체한다. 거부는 안전하지만 운영자가 결국
  `kubectl`로 돌아가게 만들어 도구를 우회하게 했다. 승인을 걸면 같은 일을 하면서 **누가
  언제 무엇을 읽었는지가 남는다**(`prd-approval-gate` AC8).

  **대가로 방어층이 하나 줄어든다.** 이 AC를 구현하려면 `k8s/rbac.yaml`이 `secrets`에
  `get`을 부여해야 하므로, 이전처럼 "도구가 뚫려도 apiserver가 403으로 막는" 2중화가
  성립하지 않는다. 도구 레이어의 게이트 판정이 유일한 경계다. 그래서 AC1의 "디스패처
  단계에서 강제"와 `prd-approval-gate` AC5의 fail-closed가 이 AC의 전제 조건이다.
- **달성 가치**: V3
- **검증 방법**: `kind=Secret`의 `resource_get`·`resource_describe`가 미승인 시 거부되고
  k8s 호출 카운트가 0이다. 승인 후에는 `get`이 값을, `describe`가 키 이름과 바이트 수를
  반환한다. `k8s/rbac.yaml`의 `secrets` 규칙이 `get`·`list`로만 한정되고
  `create`·`update`·`patch`·`delete`를 포함하지 않는다.

### AC12: 값이 새는 경로 봉쇄
- **설명**: 읽기를 허용한 이상 값이 **승인 없이 보이는 경로**와 **응답 밖에 남는 경로**를
  함께 막아야 한다.
  - **`resource_list`는 값을 담지 않는다** — 목록은 AC2의 Table 표현으로만 응답한다.
    Secret의 Table은 apiserver가 서버 사이드로 만들어 `NAME`/`TYPE`/`DATA`/`AGE`만 담으므로
    값이 애초에 전송되지 않는다. 원시 목록으로 떨어지는 폴백 경로를 두지 않는다 — 그 폴백이
    곧 무승인 일괄 열람이 된다.
  - **`watch`는 어떤 도구도 행사하지 않는다** — 변경 스트림은 객체 전문을 밀어 주므로
    읽기 게이트 종류를 `watch`하면 `data`가 그대로 흘러나온다. 게이트는 단발 `get`을
    통제할 뿐 스트림을 통제하지 못하므로, 이 경로는 게이트가 아니라 **RBAC 미부여**로
    막는다(AC15). 현재 `k8s/rbac.yaml`은 워크로드에 `watch`를 주고 있으나 코드에 `Watch()`
    호출이 없다 — 죽은 권한이며 함께 제거한다.
  - **`resource_exec`을 통한 마운트 시크릿 열람** — exec는 컨테이너 파일시스템에 닿으므로
    `/var/run/secrets/**`를 읽을 수 있다. 이 경로는 `exec` 동사 게이트가 담당하며, 게이트
    `context`에 명령 전문이 반드시 들어가야 한다(`prd-approval-gate` AC3).
  - **승인된 값의 사후 확산** — `context`·감사 로그·에러 메시지 어디에도 값을 남기지
    않는다(`prd-approval-gate` AC10).
  - **Secret 쓰기** — `resource_apply`·`resource_delete`는 종류와 무관하게 이미 게이트
    대상이므로 별도 규칙이 필요 없다. 다만 `k8s/rbac.yaml`이 `secrets`에 쓰기 verb를
    주지 않으므로 Secret 생성·수정·삭제는 AC14의 "권한 밖" 에러로 떨어진다.
- **달성 가치**: V3
- **검증 방법**: `kind=Secret`의 `resource_list`가 승인 없이 동작하되 값을 담지 않는다.
  원시 목록 폴백 경로가 코드에 존재하지 않는다. `exec` 호출이 게이트 없이 실행되지 않는다.

### AC13: 되돌리기 어려운 변경은 승인 게이트 경유
- **설명**: `resource_apply`·`resource_delete`·`resource_exec`은 `prd-approval-gate`가
  정의한 게이트를 통과해야만 실행된다. 게이트 미통과 시 쿠버네티스 API를 호출하지 않는다.

  `resource_scale`·`resource_restart`는 **게이트를 타지 않는다**. 가역적 조작이고 가장 자주
  쓰는 동작이라, 여기에 승인을 걸면 운영자가 `AUTO_APPROVE`를 켜 게이트 전체가 무력화된다
  (근거는 `prd-approval-gate`의 "게이트 대상이 아닌 것과 그 이유"). 두 도구는
  `destructiveHint=true`를 유지하지만 실행을 막지는 않으므로, **승인 없이 서비스를 멈출 수
  있다**는 점이 이 설계의 알려진 대가다.
- **달성 가치**: V3
- **검증 방법**: `prd-approval-gate` AC1·AC5의 검증을 이 세 도구에 대해 수행한다.
  같은 조건에서 `resource_scale`·`resource_restart`가 승인 없이 정상 동작한다.

### AC14: 권한 경계의 정직한 보고
- **설명**: RBAC가 허용하지 않는 조합(예: 이 서버에 부여되지 않은 종류의 `patch`)은
  apiserver의 403을 그대로 흘리지 않고, 이 서버에 부여된 권한 밖임을 밝히는 에러로
  변환한다. 에러 메시지는 **어떤 `(verb, resource)` 쌍이 없어서 막혔는지**를 밝혀,
  운영자가 `k8s/rbac.yaml`에서 곧바로 대조할 수 있게 한다.

  Secret에 쓰기 verb를 주지 않는 것도 여기서 강제되며(AC12), 그 결과 Secret
  생성·수정·삭제 시도는 이 에러로 떨어진다.
- **달성 가치**: V3
- **검증 방법**: 권한 밖 호출이 재시도를 유도하지 않는 명시적 에러를 반환하고, 그 메시지에
  누락된 `(verb, resource)` 쌍이 담긴다.

### AC15: 도구가 행사하지 않는 권한은 부여하지 않는다
- **설명**: `k8s/rbac.yaml`은 위 도구 표의 `(verb, resource[/subresource])` 쌍 합집합과
  정확히 같다. 넓으면 아무도 쓰지 않는 권한이 공격 표면으로만 남고, 좁으면 도구가
  AC14로 떨어진다. 특히 다음은 **어떤 경우에도 부여하지 않는다**.

  | 부여하지 않는 것 | 이유 |
  |------------------|------|
  | `watch` (전 종류) | 어떤 도구도 행사하지 않는다. 읽기 게이트 종류에 대해서는 게이트를 통째로 우회하는 경로다(AC12) |
  | `update` (`/scale` 외) | `resource_apply`는 Server-Side Apply(`patch`)를 쓴다. 전체 교체(PUT) 경로는 필요 없다 |
  | `deletecollection` | 한 번의 호출이 몇 개를 지우는지 승인 화면이 말할 수 없다. 행사하는 도구가 없으므로 주지 않는다 |
  | `pods/attach` | `exec`과 같은 권능인데 게이트 표에 없다 |
  | `pods/portforward` | 파드 네트워크로 임의 TCP 터널을 연다. 네트워크 정책을 우회한다 |
  | `nodes/proxy`·`pods/proxy`·`services/proxy` | kubelet API에 직접 닿아 이 서버의 다른 모든 경계를 무의미하게 만든다 |
  | `secrets`의 쓰기 verb (`create`/`update`/`patch`/`delete`) | Secret은 읽기만 연다(AC11) |

  `secrets`에는 `get`만 준다. `list`는 Table 표현이 apiserver에서 만들어지지만 그 변환에도
  `list` 권한이 필요하므로 함께 부여하되, **원시 목록 폴백 경로를 두지 않는 것**(AC12)이
  값 노출을 막는 유일한 장치임을 명시한다.
- **달성 가치**: V3
- **검증 방법**: 정적 검사가 `k8s/rbac.yaml`의 `(verb, resource)` 쌍 집합을 도구 표의
  합집합과 대조해 양방향으로 어긋남을 잡는다. 위 표의 항목이 하나라도 `rbac.yaml`에
  나타나면 실패한다.
