# 테스트 문서: resource_* (generic resource 도구군)

## 검증 대상 AC

- AC1: 좌표로 목록 조회 (PRD: resource-generic)
- AC2: 목록은 표 형식으로 반환 (PRD: resource-generic)
- AC3: 목록 절단과 이어보기 (PRD: resource-generic)
- AC4: 단건 조회와 잡음 제거 (PRD: resource-generic)
- AC5: 서브리소스 조회 (PRD: resource-generic)
- AC6: 생성 (PRD: resource-generic)
- AC7: 전체 교체 (PRD: resource-generic)
- AC8: 부분 수정 (PRD: resource-generic)
- AC9: 삭제 (PRD: resource-generic)
- AC10: 컨테이너 안에서 명령 실행 (PRD: resource-generic)
- AC11: 실행 중 컨테이너의 stdio에 접속 (PRD: resource-generic)
- AC12: 포트 포워드 (PRD: resource-generic)
- AC13: 프록시 (PRD: resource-generic)
- AC14: Secret 읽기는 승인 게이트를 거친다 (PRD: resource-generic)
- AC15: 값이 새는 경로 봉쇄 (PRD: resource-generic)
- AC16: 권한 경계의 정직한 보고 (PRD: resource-generic)
- AC17: RBAC는 도구 표의 쌍 집합과 정확히 같다 (PRD: resource-generic)
- AC18: 종류 해석과 미지원 종류 거부 (PRD: resource-generic)

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
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestListResolvesArbitraryKinds`,
  `TestListRejectsNamespaceOnClusterScoped`. 통합 `resource_generic_ac1.py`

### 시나리오 2: 목록은 표로 온다
- **사전 조건**: 동일
- **실행 단계**: 같은 Deployment 목록을 (a) 도구로 조회, (b) 객체 전문 JSON으로 직렬화해
  크기를 비교
- **기대 결과**: 도구 응답이 Table 형식(컬럼 헤더 + 행)이고 객체 전문보다 현저히 작음.
  `NAME`/`READY` 등 `kubectl get`에 준하는 컬럼이 보존됨
- **검증 AC**: AC2
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestListUsesTableAccept`,
  `TestTableResponseIsSubstantiallySmaller`. 통합 `resource_generic_ac2.py`

### 시나리오 3: 절단과 이어보기
- **사전 조건**: ConfigMap 120개 픽스처
- **실행 단계**: `limit` 미지정으로 조회 → `continue` 토큰으로 재조회 → `limit=1000`으로 조회
- **기대 결과**: 1회차에 100건 + 절단 표시 + `continue` 토큰. 2회차에 나머지 20건.
  `limit=1000`은 500으로 강제 하향
- **검증 AC**: AC3
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestListDefaultAndMaxLimit`.
  통합 `resource_generic_ac3.py`

### 시나리오 4: 단건 조회는 이름을 요구한다
- **사전 조건**: `kubectl apply`로 만들어 `last-applied-configuration`과 `managedFields`가
  모두 붙은 Deployment
- **실행 단계**: `resource_get`으로 조회 → `name` 없이 `labelSelector`만으로 호출
- **기대 결과**: `metadata.managedFields` 없음, `last-applied-configuration` 어노테이션
  없음, 나머지 `spec`/`status`는 온전. `name` 누락 호출은 거부되고 메시지가
  `resource_list`로 먼저 찾으라고 안내함 — 대상 해석 경로가 존재하지 않아야 verb 1:1이 유지됨
- **검증 AC**: AC4
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestGetStripsNoise`,
  `TestGetRequiresName`. 통합 `resource_generic_ac4.py`

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
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestGetLogTailBounds`,
  `TestGetLogPreviousInstance`, `TestSubresourceGetIsUngated`.
  통합 `resource_generic_ac5.py` (폐기되는 `workload_logs_ac{1,2,3,4}.py`의 단언을 승계한다)

### 시나리오 6: 생성은 덮어쓰지 않는다
- **사전 조건**: kind 실물 gatekeeper
- **실행 단계**: 신규 ConfigMap 매니페스트로 `resource_create`(승인) → 같은 이름으로 재호출
  (승인) → Deployment + Service 2문서 매니페스트로 호출
- **기대 결과**: 1회차 생성 성공. 2회차는 409로 거부되고 기존 객체가 변경되지 않음 —
  생성 승인이 갱신까지 하지 않음. 2문서 매니페스트는 **승인 요청 2건**을 만듦
- **검증 AC**: AC6
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestCreateDoesNotOverwrite`,
  `TestMultiDocCreateRequiresApprovalPerDoc`. 통합 `resource_generic_ac6.py`

### 시나리오 7: 전체 교체와 스케일
- **사전 조건**: 동일, `workload-fixture` 기준선, DaemonSet 픽스처
- **실행 단계**: `subresource=scale`로 replicas=3 → 0 → 1 (각 승인) → replicas=-1 →
  replicas 누락 → `kind=DaemonSet`으로 호출 → 서브리소스 없이 매니페스트 전체 교체
- **기대 결과**: 3·0·1이 그대로 반영됨. 음수와 누락은 거부(승인 요청조차 만들지 않음).
  DaemonSet은 **레플리카 부재**를 사유로 거부. 전체 교체가 PUT으로 반영됨
- **검증 AC**: AC7
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestUpdateScaleBounds`,
  `TestUpdateScaleRejectsReplicalessKind`. 통합 `resource_generic_ac7.py`
  (폐기되는 `workload_scale_ac{1,2}.py`의 단언을 승계한다)

### 시나리오 8: 부분 수정과 롤링 재시작
- **사전 조건**: 동일, `workload-fixture`를 replicas=2로 세팅
- **실행 단계**: `patchType`을 `merge`·`strategic`·`json`·`apply`로 각각 호출(각 승인) →
  호출 전 스펙 전문을 뜬 뒤 `restartedAt` 어노테이션을 얹는 `strategic` 패치(승인) →
  파드 교체 대기 → 스펙 전문 재대조
- **기대 결과**: 네 `patchType`이 각각 동작하고 `apply`는 `fieldManager`를 반영함.
  재시작 후 어노테이션 타임스탬프가 갱신되고 파드가 교체되며, `spec.replicas`가 2로
  보존되고 그 어노테이션 외 **어떤 필드도 달라지지 않음**. 재시작 패치도 다른 패치와 똑같이
  승인 요청을 만듦 — 내용으로 예외를 주지 않음
- **검증 AC**: AC8
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestPatchTypes`,
  `TestRestartPatchTouchesOnlyAnnotation`, `TestRestartPatchIsGatedLikeAnyPatch`.
  통합 `resource_generic_ac8.py` (폐기되는 `workload_restart_ac1.py`의 단언을 승계한다)

### 시나리오 9: 삭제는 단건만
- **사전 조건**: 동일
- **실행 단계**: 픽스처 ConfigMap 하나를 `resource_delete`(승인) → `gracePeriodSeconds=0`으로
  파드 삭제(승인) → 이름 없이 `labelSelector`만으로 호출 시도
- **기대 결과**: 지정 객체만 사라지고 같은 레이블의 다른 객체는 남음. `gracePeriodSeconds`가
  반영됨. 셀렉터 전용 호출 경로가 존재하지 않아 인자 검증에서 거부됨
- **검증 AC**: AC9
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestDeleteIsSingleObjectOnly`.
  통합 `resource_generic_ac9.py`

### 시나리오 10: 컨테이너 안에서 명령 실행
- **사전 조건**: kind 실물 gatekeeper, `workload-fixture` 기준선, 다중 컨테이너 파드
- **실행 단계**: `resource_exec`으로 `echo` 실행(승인) → stderr 를 내는 명령 실행 →
  다중 컨테이너 파드에 `container` 없이 호출 → `yes` 처럼 무한 출력하는 명령 실행
- **기대 결과**: stdout 과 stderr 가 구분되어 반환됨. `container` 누락이 거부되고 후보
  이름이 제시됨. 무한 출력은 바이트·시간 상한에서 잘리고 **잘렸음이 응답에 표시**됨
- **검증 AC**: AC10
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestExecStreamsAndCaps`.
  통합 `resource_generic_ac10.py`

### 시나리오 11: 실행 중 컨테이너 stdio 접속
- **사전 조건**: 동일, stdout 을 주기 출력하며 stdin 을 읽는 파드
- **실행 단계**: `resource_attach`로 `readSeconds=3` 접속(승인) → `readSeconds=31` 호출 →
  `stdin`을 실어 접속(승인)
- **기대 결과**: 3초 동안의 출력이 반환됨(새 프로세스가 뜨지 않고 기존 프로세스 스트림임을
  파드 로그로 확인). `readSeconds=31`은 거부. `stdin`이 대상 프로세스에 전달되어 그
  반응이 출력에 나타남
- **검증 AC**: AC11
- **자동화**: (미작성) — 계획: 통합 `resource_generic_ac11.py`

### 시나리오 12: 포트 포워드는 단발 왕복이다
- **사전 조건**: 동일, HTTP 를 서빙하는 파드
- **실행 단계**: `resource_port_forward`로 해당 포트에 요청 페이로드 전송(승인) →
  같은 승인 id 로 두 번째 왕복 시도 → 읽기 상한을 넘기는 응답을 내는 경로로 재호출
- **기대 결과**: 1회차에 응답이 반환되고 호출이 끝나면 터널이 닫힘. 같은 승인으로 두 번째
  왕복이 불가능(1승인 1실행이 터널에도 적용됨). 상한 초과가 잘리고 표시됨.
  승인 요청 `context` 에 **포트와 페이로드 전문**이 담김
- **검증 AC**: AC12
- **자동화**: (미작성) — 계획: 통합 `resource_generic_ac12.py`

### 시나리오 13: 프록시는 Pod·Service 만
- **사전 조건**: 동일, HTTP 파드와 그 Service
- **실행 단계**: `kind=Pod`로 `GET`(승인) → `kind=Service`로 `POST`(승인) →
  `kind=Node`로 호출 → `GET` 을 승인 없이 호출
- **기대 결과**: `GET`은 `get`, `POST`는 `create` 쌍으로 각각 게이트를 탐.
  `kind=Node`가 거부되고 사유가 **kubelet 도달**임을 밝힘. 승인 없는 `GET`도 거부됨 —
  프록시의 `get`은 객체 조회와 달라 게이트 밖이 아님. `context` 에 메서드·경로·본문이 담김
- **검증 AC**: AC13
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestProxyRejectsNode`,
  `TestProxyGetIsGated`. 통합 `resource_generic_ac13.py`

### 시나리오 14: Secret 읽기는 승인을 거친다
- **사전 조건**: 픽스처 Secret, kind 실물 gatekeeper
- **실행 단계**: `kind=Secret`으로 `resource_get` 호출(미승인 대기) → 거절 → 재호출 후 승인 →
  `kind=ConfigMap`으로 `resource_get` 호출
- **기대 결과**: 미승인·거절 시 거부되고 k8s 호출 카운트 0. 승인 후 값이 반환됨.
  ConfigMap 은 승인 요청을 만들지 않고 바로 조회됨 — 게이트가 종류로 좁혀져 있음
- **검증 AC**: AC14
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestReadGatedKindsRequireApproval`,
  `TestNonGatedKindsSkipGatekeeper`. 통합 `resource_generic_ac14.py`

### 시나리오 15: 값이 새는 경로가 막혀 있다
- **사전 조건**: 값이 고유 난수 토큰인 Secret, 그 토큰을 서빙하는 HTTP 파드
- **실행 단계**: (a) `kind=Secret`으로 `resource_list`(승인 없이) → (b)
  `RESOURCE_READ_GATED_KINDS`에 픽스처 CRD를 추가하고 그 종류로 `resource_get` →
  (c) 스트림 넷을 각각 미승인으로 호출 — `exec`으로 `cat /var/run/secrets/.../token`,
  `attach`로 접속, `port_forward`로 토큰 서빙 포트, `proxy`로 그 경로 →
  (d) 서버의 SA 토큰으로 `GET /api/v1/secrets?watch=true` 직접 호출
- **기대 결과**: (a) 승인 없이 성공하되 `NAME`/`TYPE`/`DATA`/`AGE` 컬럼만 담고 **난수 토큰이
  등장하지 않음**. (b) 승인 없이는 거부. (c) **넷 다** 미승인 거부, k8s 호출 카운트 0.
  각 승인 요청 `context` 에 명령·페이로드·경로가 전문으로 노출되어 운영자가 보고 거절할 수
  있음. (d) 403 — `watch` 미부여로 스트림 우회 경로 없음
- **검증 AC**: AC15
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestSecretListCarriesNoValues`,
  `TestNoRawListFallbackForGatedKinds`, `TestAllStreamSubresourcesGated`(표 기반 4종).
  통합 `resource_generic_ac15.py`(watch 403 확인 포함)

### 시나리오 16: 권한 밖은 권한 밖이라고 말한다
- **사전 조건**: RBAC 에 없는 종류(예: `rbac.authorization.k8s.io/v1/ClusterRole`)
- **실행 단계**: 승인을 받은 뒤 해당 종류로 `resource_patch`
- **기대 결과**: apiserver 403을 그대로 흘리지 않고, 부여된 권한 밖임을 밝히는 에러를
  반환하며 그 메시지에 누락된 `(verb, resource)` 쌍이 담김
- **검증 AC**: AC16
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestForbiddenIsTranslated`.
  통합 `resource_generic_ac16.py`

### 시나리오 17: RBAC 가 도구 표와 정확히 같다
- **사전 조건**: `k8s/rbac.yaml` 과 도구별 쌍 선언
- **실행 단계**: 정적 검사로 (a) `rbac.yaml`의 쌍 집합과 도구 표를 양방향 대조 →
  (b) 금지 목록(`watch`·`deletecollection`·`nodes/proxy`·`secrets`의 쓰기 verb)이 없는지
  확인 → (c) `rbac.yaml`에 `watch` 를, 그리고 `nodes/proxy` 를 각각 넣은 변형 2건으로 재실행
- **기대 결과**: (a) 양방향 어긋남 0 — `pods/attach`·`pods/portforward`·`pods/proxy`·
  `services/proxy` 는 도구가 생겼으므로 **있어야 정상**. (b) 금지 목록 전부 부재.
  (c) 변형 2건 모두 실패로 잡힘. 현 `rbac.yaml`이 워크로드에 주고 있던 `watch` 는 코드에
  `Watch()` 호출이 없는 죽은 권한이므로 이 검사가 곧바로 잡는다
- **검증 AC**: AC17
- **자동화**: (미작성) — 계획: 정적 `scripts/check_rbac_matches_tools.py`(뮤테이션 2건 포함).
  통합 `resource_generic_ac17.py`

### 시나리오 18: 종류 해석
- **사전 조건**: 픽스처 CRD 설치 전/후 두 상태
- **실행 단계**: `api_resources` 조회 → 존재하지 않는 `kind=Deploymnt`(오타)로
  `resource_list` → CRD 설치 후 `api_resources` 재조회 및 해당 종류 list
- **기대 결과**: 오타 호출이 거부되고 후보로 `Deployment`가 제시됨. CRD 설치 후
  `api_resources`에 나타나고 곧바로 조회됨(재기동 불필요). `api_resources`는 리소스 권한을
  행사하지 않으므로 RBAC 대조 대상이 아님
- **검증 AC**: AC18
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestUnknownKindSuggestsCandidates`.
  통합 `resource_generic_ac18.py`
