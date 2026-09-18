# 테스트 문서: resource_* (generic resource 도구군)

## 검증 대상 AC

- AC1: 좌표로 목록 조회 (PRD: resource-generic)
- AC2: 목록은 표 형식으로 반환 (PRD: resource-generic)
- AC3: 목록 절단과 이어보기 (PRD: resource-generic)
- AC4: 단건 조회와 잡음 제거 (PRD: resource-generic)
- AC5: 서브리소스 조회 (PRD: resource-generic)
- AC6: 변경 스트림 관측 (PRD: resource-generic)
- AC7: 생성 (PRD: resource-generic)
- AC8: 전체 교체 (PRD: resource-generic)
- AC9: 부분 수정 (PRD: resource-generic)
- AC10: 삭제 (PRD: resource-generic)
- AC11: 컬렉션 일괄 삭제 (PRD: resource-generic)
- AC12: 컨테이너 안에서 명령 실행 (PRD: resource-generic)
- AC13: 실행 중 컨테이너의 stdio에 접속 (PRD: resource-generic)
- AC14: 포트 포워드 (PRD: resource-generic)
- AC15: 프록시 (PRD: resource-generic)
- AC16: 민감 종류는 읽기도 쓰기도 승인 게이트를 거친다 (PRD: resource-generic)
- AC17: 값이 새는 경로 봉쇄 (PRD: resource-generic)
- AC18: 권한 경계의 정직한 보고 (PRD: resource-generic)
- AC20: 종류 해석과 미지원 종류 거부 (PRD: resource-generic)

## 픽스처

폐기되는 6종의 e2e가 쓰던 `tests/k8s/kind/test-deployment.yaml`(`workload-fixture`)과
`_workload.py::ensure_workload_fixture_baseline()`을 그대로 승계한다.

여기에 `tests/k8s/kind/resource-generic-fixture.yaml`을 더한다: Service·ConfigMap·Ingress
각 1, 목록 절단을 만들 ConfigMap 120개, CRD 1종과 그 인스턴스 1개, 크래시 루프 파드 1개,
다중 컨테이너 파드 1개, DaemonSet 1개, **HTTP 를 서빙하는 파드 1개 + 그 Service**
(포트 포워드·프록시 대상), **주기적으로 stdout 을 내보내며 stdin 을 읽는 파드 1개**
(attach 대상), 그리고 **값이 고유 난수 토큰인 Secret 1개**
(게이트와 값 봉쇄가 실제로 동작하는지 확인하려면 대상이 실재하고 그 값이 전 표면에서
검색 가능해야 한다).

이 파일은 시나리오가 전용 e2e로 착지할 때마다 그 시나리오가 요구하는 대상만 더하며 자란다.
2026-09-15 현재 담긴 것은 시나리오 1·3·5가 요구하는 몫이다 — 시나리오 1 몫으로
Service·ConfigMap·Ingress 각 2(하나만 좁히는 레이블을 달아 「셀렉터가 결과 수를 실제로 줄임」이
관측되게 한다)와 CRD 1종 + 그 인스턴스 2개, 시나리오 3 몫으로 **목록 절단을 만들 ConfigMap
120개**. 페이징 쪽에는 전용 레이블 `homelab-k3s-mcp.test/paging`을 단다 — 같은 네임스페이스에
위의 ConfigMap 2개와 apiserver가 넣는 `kube-root-ca.crt`가 이미 있어, 좁히지 않으면 모집단이
123이 되고 시나리오 3의 「2회차에 나머지 20건」이 성립하지 않는다. 시나리오 5 몫으로는
**다중 컨테이너 파드 1개**(`resource-generic-multi` — 정해진 줄 수를 찍고 상주하는 컨테이너
하나 + 조용한 컨테이너 하나)를 둔다. 크래시 루프 파드는 새로 세우지 않고 위 `test-deployment.yaml`의
`crashloop-fixture`를 쓴다 — 그것은 한 번만 죽고 계속 사는 형태로 이미 설계돼 있고(연속 크래시는
containerd가 직전 인스턴스 로그를 GC해 `previous=true` 읽기를 흔든다), 같은 것을 하나 더 세우면
그 판단이 두 벌이 된다. CRD와 인스턴스는 `resource-generic-samples.yaml`로 갈라 두었다 —
`kubectl apply`가 파일을 읽는 시점에 종류를 해석하므로 한 파일에 두면 아직 서지 않은 종류를
가리켜 파일 전체가 거부된다.

변경 verb 시나리오는 전부 승인이 필요하므로 `test-approval-gate.md`의 gatekeeper 픽스처를
공유하고, 승인은 forward-auth 헤더로 `PATCH /api/requests/{id}/approve`를 직접 호출해
대신한다.

## 테스트 시나리오

### 시나리오 1: 임의 종류를 좌표로 조회한다
- **사전 조건**: 픽스처 적용
- **실행 단계**: `v1/Namespace`, `v1/Service`, `v1/ConfigMap`, `apps/v1/Deployment`,
  `networking.k8s.io/v1/Ingress`, `v1/Event`, 픽스처 CRD를 각각 `resource_list`로 조회 →
  `labelSelector`로 좁혀 재조회 → `v1/Event`를
  `fieldSelector=involvedObject.name=⟨파드⟩`로 좁혀 조회 → 클러스터 스코프 종류에
  `namespace`를 붙여 호출
- **기대 결과**: 일곱 종류 모두 목록 반환. 셀렉터가 결과 수를 실제로 줄임. 이벤트가
  대상별로 좁혀짐(폐기된 `pod_describe`의 이벤트 부분이 이 경로로 대체됨).
  클러스터 스코프 + `namespace` 조합은 무시되지 않고 거부
- **검증 AC**: AC1
- **자동화**: 통합 `tests/integration/resource_generic_ac1.py`. Go 단위는 아직 없다 — 계획:
  `resource_test.go::TestListResolvesArbitraryKinds`,
  `TestListRejectsNamespaceOnClusterScoped`

### 시나리오 2: 목록은 표로 온다
- **사전 조건**: 동일
- **실행 단계**: 같은 Deployment 목록을 (a) 도구로 조회, (b) 객체 전문 JSON으로 직렬화해
  크기를 비교
- **기대 결과**: 도구 응답이 Table 형식(컬럼 헤더 + 행)이고 객체 전문보다 현저히 작음.
  `NAME`/`READY` 등 `kubectl get`에 준하는 컬럼이 보존됨
- **검증 AC**: AC2
- **자동화**: 통합 `tests/integration/resource_generic_ac2.py`. Go 단위는 아직 없다 — 계획:
  `resource_test.go::TestListUsesTableAccept`, `TestTableResponseIsSubstantiallySmaller`

### 시나리오 3: 절단과 이어보기
- **사전 조건**: ConfigMap 120개 픽스처
- **실행 단계**: `limit` 미지정으로 조회 → `continue` 토큰으로 재조회 → `limit=1000`으로 조회
- **기대 결과**: 1회차에 100건 + 절단 표시 + `continue` 토큰. 2회차에 나머지 20건.
  `limit=1000`은 클램프되지 않고 **거부**(시나리오 5의 `tailLines=5001`과 같은 처리)
- **검증 AC**: AC3
- **자동화**: 통합 `tests/integration/resource_generic_ac3.py`. 상한 처리는 Go 단위
  `internal/server/mcp_test.go::TestResourceListLimitDefaultsAndCeiling`이 이미 고정한다
  (기본 100 · 초과 거부) — 통합 쪽은 배포된 서버에서 같은 경계를 다시 재고, 거기에 더해
  `continue` 이어보기가 두 페이지로 전집을 덮는지까지 본다

### 시나리오 4: 단건 조회는 이름을 요구한다
- **사전 조건**: `kubectl apply`로 만들어 `last-applied-configuration`과 `managedFields`가
  모두 붙은 Deployment
- **실행 단계**: `resource_get`으로 조회 → `name` 없이 `labelSelector`만으로 호출
- **기대 결과**: `metadata.managedFields` 없음, `last-applied-configuration` 어노테이션
  없음, 나머지 `spec`/`status`는 온전. `name` 누락 호출은 거부되고 메시지가
  `resource_list`로 먼저 찾으라고 안내함 — 대상 해석 경로가 존재하지 않아야 verb 1:1이 유지됨
- **검증 AC**: AC4
- **자동화**: 통합 `tests/integration/resource_generic_ac4.py`. Go 단위는 아직 없다 — 계획:
  `resource_test.go::TestGetStripsNoise`, `TestGetRequiresName`

### 시나리오 5: 서브리소스 조회
- **사전 조건**: `workload-fixture` 기준선, 크래시 루프 파드, 다중 컨테이너 파드
- **실행 단계**: `subresource=log`로 로그 조회 → `tailLines=5` → `tailLines=5001` →
  크래시 루프 파드에 `previous=true` → 다중 컨테이너 파드에 `container` 없이 호출 →
  `subresource=scale`로 현재 레플리카 조회
- **기대 결과**: 최근 로그 반환. `tailLines=5`가 라인 수를 실제로 줄임. 5001은
  클램프되지 않고 **거부**. `previous=true`가 직전 인스턴스 로그를 반환(Running 파드가
  없어도 동작). `container` 누락이 거부되고 후보 이름이 제시됨. `subresource=scale`이
  현재 레플리카를 반환. 모든 호출이 `get` verb만 행사하므로 승인 요청이 생기지 않음
- **검증 AC**: AC5
- **자동화**: 통합 `tests/integration/resource_generic_ac5.py` — 위 여섯 단계와 마지막 절
  (「승인 요청이 생기지 않음」)을 전부 단정하고, 폐기된 `workload_logs_ac{1,2,3,4}.py`의 단언을
  승계했다. 그 파일들이 단정하지 못한 채 남겼던 「컨테이너가 둘 이상이면 `container`가
  필요하다」는 다중 컨테이너 픽스처가 서면서 이 파일에서 닫혔다. `container` 누락 축은 Go
  단위도 함께 선다:
  `internal/k8s/resource_test.go::TestPodLogsKeepsTheApiserversContainerCandidates`와
  그 대조군 `TestPodLogsForbiddenStillReportsTheMissingGrant`·
  `TestPodLogsFallsBackWhenTheBodyIsNotAStatus`

### 시나리오 6: 변경 스트림 관측
- **사전 조건**: `workload-fixture` 기준선, 픽스처 Secret, kind 실물 gatekeeper
- **실행 단계**: Deployment 를 `watchSeconds=10` 으로 관측하면서 별도 경로로 레플리카 변경 →
  `watchSeconds=61` 호출 → `resourceVersion` 을 주고 재관측 → `kind=Secret` 으로 미승인 호출 →
  승인 후 재호출
- **기대 결과**: `MODIFIED` 이벤트가 창 안에서 관측되고 창이 닫히면 응답이 돌아옴.
  61은 거부. `resourceVersion` 이후 변경만 옴. Secret 은 미승인 시 거부되고 승인 후에는
  이벤트가 오되 수·창 상한이 그대로 적용됨
- **검증 AC**: AC6
- **자동화**: (미작성) — 통합 `resource_generic_ac6.py` 는 아직 없다. Go 단위 둘은 계획한
  이름 그대로 섰다: `internal/mcp/resource_test.go::TestWatchWindowBounds`(기본 10 · 60 통과 ·
  61·0·비정수 거부, 그리고 거부마다 k8s 호출 0)와
  `::TestWatchOnGatedKindRequiresApproval`(`kind=Secret` 은 미승인 시 거부되고 k8s 호출 0,
  승인 후 실행되며, 평범한 종류는 승인 요청 자체가 생기지 않는다). 곁에
  `::TestWatchCarriesTheResumePoint`(`resourceVersion`·셀렉터 통과)와
  `internal/server/mcp_test.go::TestResourceWatchReturnsEventsWithoutDumpingObjects`
  (이벤트는 `structuredContent` 에, 텍스트는 이벤트당 한 줄)가 있다.
  **이 수준이 못 보는 것**은 실 apiserver 가 창 안에서 실제로 `MODIFIED` 를 밀어 주는지와
  이벤트 수 상한이 실물 스트림에서 닫히는지이며, 그것이 통합 파일이 남아 있는 이유다.

### 시나리오 7: 생성은 덮어쓰지 않는다
- **사전 조건**: kind 실물 gatekeeper
- **실행 단계**: 신규 ConfigMap 매니페스트로 `resource_create`(승인) → 같은 이름으로 재호출
  (승인) → Deployment + Service 2문서 매니페스트로 호출 → 같은 2문서 매니페스트를 이름만
  바꿔 다시 호출하되 **둘째 문서의 승인을 거절** → 또 한 번 호출하되 둘 다 승인하고
  **둘째 문서의 이름을 이미 존재하는 것으로** 두어 실행 단계에서 409가 나게 함
- **기대 결과**: 1회차 생성 성공. 2회차는 409로 거부되고 기존 객체가 변경되지 않음 —
  생성 승인이 갱신까지 하지 않음. 2문서 매니페스트는 **승인 요청 2건**을 만듦.
  **거절 케이스는 아무것도 만들지 않음** — 승인을 전부 받은 뒤에 첫 객체를 만들므로
  첫째 문서의 객체조차 생기지 않고 호출 전체가 거부된다. **부분 실패 케이스는 되돌리지
  않음** — 첫째 문서의 객체는 그대로 남고(되돌리려면 승인 없는 `delete`가 필요하다),
  응답이 만들어진 것·실패한 것·시도하지 않은 것을 각각 좌표로 보고한다
- **검증 AC**: AC7
- **자동화**: Go 단위 `internal/k8s/create_test.go::TestCreateDoesNotOverwrite`는
  namespaced/core·cluster/core·namespaced/group 세 경로의 실제 HTTP POST와 409를 검증한다.
  `TestCreateNeverRetriesOrEchoesAPIValues`는 Retry-After가 있어도 호출 1회, 403 쌍 표기,
  API 오류 본문의 민감 값 비노출을 확인한다. `internal/mcp/create_test.go`의
  `TestMultiDocCreateRequiresApprovalPerDoc`·`TestMultiDocCreateRejectionCreatesNothing`·
  `TestMultiDocCreateStopsAndReportsOnFailure`가 승인 전량 선행, 거절/만료/오류 시 호출 0,
  부분 실패의 좌표 보고와 미소비 승인을 검증한다. 같은 파일에서 파싱 전체 선행,
  승인 재사용 거부, 민감 값 마스킹, 자동 승인 표시도 검증한다.
  **통합 `resource_generic_ac7.py`는 아직 미작성**이다. 위 테스트는 dispatcher와 HTTP
  경계를 검증하며 실 apiserver·gatekeeper 동작을 입증하지 않는다. 실물 gatekeeper
  픽스처와 전용 E2E 후속은 `doc-tracker.md`의 시나리오 7 행에 남긴다.

### 시나리오 8: 전체 교체와 스케일
- **사전 조건**: 동일, `workload-fixture` 기준선, DaemonSet 픽스처
- **실행 단계**: `subresource=scale`로 replicas=3 → 0 → 1 (각 승인) → replicas=-1 →
  replicas 누락 → `kind=DaemonSet`으로 호출 → 서브리소스 없이 매니페스트 전체 교체
- **기대 결과**: 3·0·1이 그대로 반영됨. 음수와 누락은 거부(승인 요청조차 만들지 않음).
  DaemonSet은 **레플리카 부재**를 사유로 거부. 전체 교체가 PUT으로 반영됨
- **검증 AC**: AC8
- **자동화**: Go 단위 둘은 계획한 이름 그대로 서 있다 —
  `internal/mcp/resource_test.go::TestUpdateScaleBounds`(3·0·1 반영 + 거부 일곱, 그중
  음수·누락은 **승인 요청이 만들어지기 전에** 거부된다),
  `internal/k8s/resource_test.go::TestUpdateScaleRejectsReplicalessKind`(사유가 권한도
  존재 여부도 아닌 **레플리카 부재**임을 단언한다). 전체 교체 경로는
  `internal/mcp/resource_test.go::TestUpdateReplacesWithTheCallersManifest` 가 덮는다.
  레플리카가 실제로 그 수로 수렴하는지와 파드가 그에 맞춰 뜨고 지는지는 apiserver 의
  거동이라 단위 층이 볼 수 없고 통합 쪽 몫이다.
  통합은 `tests/integration/resource_generic_ac8.py` — 승인 댄스 뒤 3·0·1 수렴을 status
  폴링으로 잡고, 음수·누락 거부는 「새 승인 요청 0건」과 나란히 고정하며, DaemonSet 거부는
  승인 뒤 사유 문면으로 잰다. `workload_scale_ac{1,2}.py` 는 #72 에서 폐기됐으므로 승계할
  파일은 없고 단언은 새로 저작했다

### 시나리오 9: 부분 수정과 롤링 재시작
- **사전 조건**: 동일, `workload-fixture`를 replicas=2로 세팅
- **실행 단계**: `patchType`을 `merge`·`strategic`·`json`·`apply`로 각각 호출(각 승인) →
  호출 전 스펙 전문을 뜬 뒤 `restartedAt` 어노테이션을 얹는 `strategic` 패치(승인) →
  파드 교체 대기 → 스펙 전문 재대조
- **기대 결과**: 네 `patchType`이 각각 동작하고 `apply`는 `fieldManager`를 반영함.
  재시작 후 어노테이션 타임스탬프가 갱신되고 파드가 교체되며, `spec.replicas`가 2로
  보존되고 그 어노테이션 외 **어떤 필드도 달라지지 않음**. 재시작 패치도 다른 패치와 똑같이
  승인 요청을 만듦 — 내용으로 예외를 주지 않음
- **검증 AC**: AC9
- **자동화**: Go 단위 셋은 계획한 이름 그대로 서 있다 —
  `internal/mcp/resource_test.go::TestPatchTypes`(네 `patchType` + 인자 거부 여섯),
  `TestRestartPatchTouchesOnlyAnnotation`(서비스에 도달하는 호출자 데이터 본문은 유지한다; API 경계에는 승인 조건만 추가된다),
  `TestRestartPatchIsGatedLikeAnyPatch`. 파드 교체와 `spec.replicas` 보존은 apiserver 의
  거동이라 단위 층이 볼 수 없고 통합 쪽 몫이다.
  통합은 `tests/integration/resource_generic_ac9.py` — 네 patchType 각각의 승인·적용,
  재시작 패치 뒤 파드 교체(uid 기준), `spec.replicas` 보존, 그 어노테이션 외 무변경을 spec
  전문 비교로 잰다. `workload_restart_ac1.py` 는 #72 에서 폐기됐으므로 승계할 파일은 없고
  단언은 새로 저작했다
  추가 회귀: `internal/k8s/patch_precondition_test.go`는 네 형식의 HTTP PATCH 경로·매체형·
  승인 조건·manager 보존, 409/422/429/503/403의 단일 요청과 오류 비누출,
  JSON Patch의 앞뒤 조건 실패, metadata 조건 변조 거부와 큰 정수 보존을 검증한다.
  `internal/mcp/patch_precondition_test.go`는 승인 상태 전달·호출자 위조값 무시,
  누락/변경 상태의 0쓰기, 충돌 시 1승인·1쓰기·재읽기 없음과 호출 간 격리를 검증한다.
  이들은 Go/HTTP 경계 회귀이며 실물 apiserver·gatekeeper 통합 증거를 대신하지 않는다.

### 시나리오 10: 삭제는 단건만
- **사전 조건**: 동일
- **실행 단계**: 픽스처 ConfigMap 하나를 `resource_delete`(승인) → `gracePeriodSeconds=0`으로
  파드 삭제(승인) → 이름 없이 `labelSelector`만으로 호출 시도
- **기대 결과**: 지정 객체만 사라지고 같은 레이블의 다른 객체는 남음. `gracePeriodSeconds`가
  반영됨. 셀렉터 전용 호출 경로가 존재하지 않아 인자 검증에서 거부됨
- **검증 AC**: AC10
- **자동화**: Go 단위는 계획한 이름 그대로 서 있다 —
  `internal/mcp/resource_test.go::TestDeleteIsSingleObjectOnly`(좌표·`gracePeriodSeconds`
  가 클러스터 층에 닿는 것 + 거부 일곱. 셀렉터 둘·`fieldSelector`·`subresource` 는
  **승인 요청이 만들어지기 전에** 거부된다 — 「셀렉터 전용 호출 경로가 존재하지 않는다」의
  단위 층 표현이다). 그 거부 단언들이 공허해지지 않는다는 대조군은
  `TestDeleteIsRefusedWithoutApproval` 이 선다(게이트를 아예 타지 않는 도구는 「게이트가
  안 불렸다」를 전부 통과한다). 광고 표면에 셀렉터 인자가 없다는 것은
  `internal/server/mcp_test.go::TestToolsListAdvertisesResourceTools` 가 단언한다.
  지정 객체만 사라지고 같은 레이블의 다른 객체가 남는지, `gracePeriodSeconds` 가 실제로
  반영되는지는 apiserver 의 거동이라 단위 층이 볼 수 없고 통합 쪽 몫이다.
  통합은 `tests/integration/resource_generic_ac10.py` — 거부 셋이 승인 요청을 만들지 않음을
  PENDING 카운트로 고정하고, 지정 객체만 사라짐·같은 레이블의 다른 객체 생존을 잰다.
  `gracePeriodSeconds=0` 의 반영은 SIGTERM 을 무시하는 일회용 파드로 잰다 — 기본 유예가
  흘렀다면 30초를 버텼을 행동이 즉시 SIGKILL 로 끝나는 것이 그 값이 apiserver 에 닿았음의
  관측이다

### 시나리오 11: 컨테이너 안에서 명령 실행
- **사전 조건**: kind 실물 gatekeeper, `workload-fixture` 기준선, 다중 컨테이너 파드
- **실행 단계**: `resource_exec`으로 `echo` 실행(승인) → stderr 를 내는 명령 실행 →
  다중 컨테이너 파드에 `container` 없이 호출 → `yes` 처럼 무한 출력하는 명령 실행
- **기대 결과**: stdout 과 stderr 가 구분되어 반환됨. `container` 누락이 거부되고 후보
  이름이 제시됨. 무한 출력은 바이트·시간 상한에서 잘리고 **잘렸음이 응답에 표시**됨
- **검증 AC**: AC11
- **자동화**: Go 단위 `resource_test.go::TestExecStreamsAndCaps` 착지(2026-09-17 —
  stdout·stderr 구분 전달과 좌표·컨테이너·명령 전달, 인자 사전 거부(클러스터 호출 0),
  상한 잘림·시간 상한 표시, AC6의 승인 뒤 재생성 거부). 통합 `resource_generic_ac11.py`
  는 실물 gatekeeper 픽스처 대기.

### 시나리오 12: 컬렉션 일괄 삭제
- **사전 조건**: kind 실물 gatekeeper, 같은 레이블을 단 ConfigMap 5개와 다른 레이블 2개
- **실행 단계**: `labelSelector` 로 5개를 겨냥해 호출 → 승인 요청 `context` 수집 → 승인 →
  `namespace` 없이 호출 → 0건을 가리키는 셀렉터로 호출 → 승인과 실행 사이에 같은 레이블의
  ConfigMap 을 하나 더 만들고 승인
- **기대 결과**: 5개만 사라지고 다른 레이블 2개는 남음. `context` 에 **대상 수 5와 이름
  목록**이 담김. `namespace` 누락은 거부. 0건 셀렉터는 승인 요청을 만들지 않고 알림.
  대상이 늘어난 마지막 케이스는 실행이 거부됨
- **검증 AC**: AC12
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestDeleteCollectionRequiresNamespace`,
  `TestDeleteCollectionContextCarriesTargets`, `TestDeleteCollectionZeroMatchSkipsApproval`.
  통합 `resource_generic_ac12.py`

### 시나리오 13: 실행 중 컨테이너 stdio 접속
- **사전 조건**: 동일, stdout 을 주기 출력하며 stdin 을 읽는 파드
- **실행 단계**: `resource_attach`로 `readSeconds=3` 접속(승인) → `readSeconds=31` 호출 →
  `stdin`을 실어 접속(승인)
- **기대 결과**: 3초 동안의 출력이 반환됨(새 프로세스가 뜨지 않고 기존 프로세스 스트림임을
  파드 로그로 확인). `readSeconds=31`은 거부. `stdin`이 대상 프로세스에 전달되어 그
  반응이 출력에 나타남
- **검증 AC**: AC13
- **자동화**: Go 단위 `internal/mcp/attach_test.go::TestAttachJoinsTheRunningProcess`·
  `TestAttachRefusalsCostNoApproval`·`TestAttachContextCarriesTheStdin` 착지(2026-09-18 —
  좌표·컨테이너·읽기 창 전달, `readSeconds` 미지정 시 기본 5, `readSeconds=31`·`0`·비정수의
  **클러스터 호출 0·승인 요청 0** 거부, stdin 전달과 빈 문자열이 부재와 구분됨, 상한 잘림 표시,
  쌍이 `create on pods/attach`, 승인 `context` 가 stdin 을 그대로 실음). 통합
  `resource_generic_ac13.py` 는 아직 저작되지 않았다 — 선행이던 실물 gatekeeper 픽스처는
  PR #106 으로 main 에 착지해 더는 차단 요인이 아니고, 남은 것은 파일 저작뿐이다. **파드 로그로
  「새 프로세스가 뜨지 않았다」를 확인하는 절과 stdin 에 대한 대상 프로세스의 반응은 단위로
  관측할 수 없어 그쪽 몫이다**

### 시나리오 14: 포트 포워드는 단발 왕복이다
- **사전 조건**: 동일, HTTP 를 서빙하는 파드
- **실행 단계**: `resource_port_forward`로 해당 포트에 요청 페이로드 전송(승인) →
  같은 승인 id 로 두 번째 왕복 시도 → 읽기 상한을 넘기는 응답을 내는 경로로 재호출
- **기대 결과**: 1회차에 응답이 반환되고 호출이 끝나면 터널이 닫힘. 같은 승인으로 두 번째
  왕복이 불가능(1승인 1실행이 터널에도 적용됨). 상한 초과가 잘리고 표시됨.
  승인 요청 `context` 에 **포트와 페이로드 전문**이 담김
- **검증 AC**: AC14
- **자동화**: (미작성) — 계획: 통합 `resource_generic_ac14.py`

### 시나리오 15: 프록시는 경로를 숨기지 않는다
- **사전 조건**: kind 실물 gatekeeper, HTTP 파드와 그 Service, 노드 1개
- **실행 단계**: `kind=Pod`로 `GET`(승인) → `kind=Service`로 `POST`(승인) →
  `kind=Node`로 `/healthz` `GET`(승인) → `kind=Node`로 `/exec/⟨ns⟩/⟨pod⟩/⟨c⟩?command=id`
  호출하고 승인 요청 `context` 수집 후 승인 → `GET`을 승인 없이 호출
- **기대 결과**: 네 경우 모두 메서드에 대응하는 쌍(`get`/`create`)으로 게이트를 탐.
  `/healthz`와 `/exec`이 **같은 쌍**으로 표현되어 쌍만으로는 구별되지 않음. `/exec` 호출의
  `context`에 **경로 전문과 고권한 표시**가 함께 담기고, 승인하면 실제로 실행됨 —
  경로 허용목록이 없음을 확인한다. 승인 없는 `GET`도 거부됨
- **검증 AC**: AC15
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestProxyAllPathsGated`,
  `TestProxyKubeletHighPowerPathFlagged`. 통합 `resource_generic_ac15.py`

### 시나리오 16: 민감 종류는 읽기도 쓰기도 승인을 거친다
- **사전 조건**: 값이 고유 난수 토큰인 Secret, kind 실물 gatekeeper
- **실행 단계**: `kind=Secret`으로 `get`·`watch`·`create`·`update`·`patch`·`delete`를
  각각 미승인 호출 → `create`를 승인 흐름까지 태우고 `context` 수집 → 승인 후 값 확인 →
  `kind=Secret`으로 `list` 호출(승인 없이) → `kind=ConfigMap`으로 `get`
- **기대 결과**: 여섯 verb 모두 미승인 거부, k8s 호출 카운트 0. `create`의 `context`에
  **키 이름과 바이트 수만** 있고 난수 토큰이 없음. 승인 후 Secret이 생성되고 값이 반영됨.
  `list`는 승인 없이 동작. ConfigMap `get`은 승인 요청을 만들지 않음
- **검증 AC**: AC16
- **자동화**: 둘 중 하나가 섰다 —
  `internal/mcp/resource_test.go::TestSecretWriteContextRedactsValues` 가 민감 종류 쓰기의
  `context` 에 키 이름과 바이트 수만 남는 것을 단언하고, 같은 파일의
  `TestOrdinaryWriteKeepsItsBodyInTheContext` 가 그 대조군이다(전부 가리는 구현도 막는다).
  `TestGatedKindsGateEveryVerbButList` 는 **(미작성)** —
  `get`·`watch`·`create`·`update`·`patch`·`delete` 여섯 verb가 모두 등록돼 있다.
  create의 값 가림·생성 값 보존은 `internal/mcp/create_test.go`의 Go 테스트로 검증한다.
  통합 `resource_generic_ac16.py`도 **(미작성)**이며 실물 gatekeeper 픽스처가 선행이다.
  현재의 행별 근거·담당·해제 조건은 `doc-tracker.md`의 구현 대기 표를 따른다.

### 시나리오 17: 값이 새는 경로가 막혀 있다
- **사전 조건**: 값이 고유 난수 토큰인 Secret, 그 토큰을 서빙하는 HTTP 파드
- **실행 단계**: (a) `kind=Secret`으로 `resource_list`(승인 없이) → (b)
  `RESOURCE_GATED_KINDS`에 픽스처 CRD를 추가하고 그 종류로 `resource_get` →
  (c) 스트림 넷을 각각 미승인으로 호출 — `exec`으로 `cat /var/run/secrets/.../token`,
  `attach`로 접속, `port_forward`로 토큰 서빙 포트, `proxy`로 그 경로 →
  (c') `kind=Node` 프록시의 `/exec` 으로 같은 파일을 읽어 그 호출의 쌍을 기록 →
  (d) 서버의 SA 토큰으로 `GET /api/v1/secrets?watch=true` 직접 호출
- **기대 결과**: (a) 승인 없이 성공하되 `NAME`/`TYPE`/`DATA`/`AGE` 컬럼만 담고 **난수 토큰이
  등장하지 않음**. (b) 승인 없이는 거부. (c) **넷 다** 미승인 거부, k8s 호출 카운트 0.
  각 승인 요청 `context` 에 명령·페이로드·경로가 전문으로 노출되어 운영자가 보고 거절할 수
  있음. (c') 쌍이 `create nodes/proxy` 로만 기록되어 **민감 종류 판정에 걸리지 않음** —
  쌍으로 막을 수 없는 경로임을 시나리오로 못박는다. 막는 것은 `context` 의 경로 노출뿐임.
  (d) `watch` 는 이제 부여되어 403 이 아니라 **민감 종류 게이트**에서 거부 — `watch` 미부여로 스트림 우회 경로 없음
- **검증 AC**: AC17
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestSecretListCarriesNoValues`,
  `TestNoRawListFallbackForGatedKinds`, `TestAllStreamSubresourcesGated`(표 기반 4종).
  통합 `resource_generic_ac17.py`(watch 403 확인 포함)

### 시나리오 18: 권한 밖은 권한 밖이라고 말한다
- **사전 조건**: RBAC 에 없는 종류(예: `rbac.authorization.k8s.io/v1/ClusterRole`)
- **실행 단계**: 승인을 받은 뒤 해당 종류로 `resource_patch`
- **기대 결과**: apiserver 403을 그대로 흘리지 않고, 부여된 권한 밖임을 밝히는 에러를
  반환하며 그 메시지에 누락된 `(verb, resource)` 쌍이 담김
- **검증 AC**: AC18
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestForbiddenIsTranslated`.
  통합 `resource_generic_ac18.py`

### 시나리오 20: 종류 해석
- **사전 조건**: 픽스처 CRD 설치 전/후 두 상태
- **실행 단계**: `api_resources` 조회 → 존재하지 않는 `kind=Deploymnt`(오타)로
  `resource_list` → CRD 설치 후 `api_resources` 재조회 및 해당 종류 list
- **기대 결과**: 오타 호출이 거부되고 후보로 `Deployment`가 제시됨. CRD 설치 후
  `api_resources`에 나타나고 곧바로 조회됨(재기동 불필요). `api_resources`는 리소스 권한을
  행사하지 않으므로 RBAC 대조 대상이 아님
- **검증 AC**: AC20
- **자동화**: Go 단위 `resource_test.go::TestUnknownKindSuggestsCandidates`(후보 산출).
  통합 `tests/integration/resource_generic_ac20.py` — 전용 프로브 CRD 를 테스트가 런타임에
  세우고 지운다(픽스처로 두면 설치 「전」 상태가 관측되지 않는다). 「재기동 불필요」는 서버
  파드의 `uid`·`startTime` 이 그대로임으로 잰다
