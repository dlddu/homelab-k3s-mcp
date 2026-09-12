# 테스트 문서: resource_* (generic resource 도구군)

## 검증 대상 AC

- AC1: 좌표로 목록 조회 (PRD: resource-generic)
- AC2: 목록은 표 형식으로 반환 (PRD: resource-generic)
- AC3: 목록 절단과 이어보기 (PRD: resource-generic)
- AC4: 단건 조회의 잡음 제거 (PRD: resource-generic)
- AC5: 컨테이너 로그 조회 (PRD: resource-generic)
- AC6: 진단 스냅샷 (PRD: resource-generic)
- AC7: 대상 해석 (PRD: resource-generic)
- AC8: 종류 해석과 미지원 종류 거부 (PRD: resource-generic)
- AC9: 레플리카 설정과 레플리카 없는 종류 거부 (PRD: resource-generic)
- AC10: 롤링 재시작 (PRD: resource-generic)
- AC11: Secret 읽기는 승인 게이트를 거친다 (PRD: resource-generic)
- AC12: 값이 새는 경로 봉쇄 (PRD: resource-generic)
- AC13: 되돌리기 어려운 변경은 승인 게이트 경유 (PRD: resource-generic)
- AC14: 권한 경계의 정직한 보고 (PRD: resource-generic)
- AC15: 도구가 행사하지 않는 권한은 부여하지 않는다 (PRD: resource-generic)

## 픽스처

폐기되는 6종의 e2e가 쓰던 `tests/k8s/kind/test-deployment.yaml`(`workload-fixture`)과
`_workload.py::ensure_workload_fixture_baseline()`을 그대로 승계한다. 6종을 지우면서 그
픽스처까지 지우면 같은 것을 다시 세우게 된다.

여기에 종류 다양성과 배제 검증을 위한 픽스처를 더한다
(`tests/k8s/kind/resource-generic-fixture.yaml`): Service·ConfigMap·Ingress 각 1,
목록 절단을 만들 ConfigMap 120개, CRD 1종과 그 인스턴스 1개, 크래시 루프 파드 1개,
다중 컨테이너 파드 1개, 그리고 **값이 고유 난수 토큰인 Secret 1개**(게이트와 값 봉쇄가 실제로 동작하는지 확인하려면
대상이 실재하고 그 값이 전 표면에서 검색 가능해야 한다).

승인 게이트가 필요한 시나리오는 `test-approval-gate.md`의 gatekeeper 픽스처를 공유한다.

## 테스트 시나리오

### 시나리오 1: 임의 종류를 좌표로 조회한다
- **사전 조건**: 픽스처 적용
- **실행 단계**: `v1/Namespace`, `v1/Service`, `v1/ConfigMap`, `apps/v1/Deployment`,
  `networking.k8s.io/v1/Ingress`, 픽스처 CRD를 각각 `resource_list`로 조회.
  이어서 `labelSelector`로 좁혀 재조회. 클러스터 스코프 종류(`v1/Namespace`)에
  `namespace`를 붙여 호출
- **기대 결과**: 여섯 종류 모두 목록 반환. 셀렉터가 결과 수를 실제로 줄임.
  클러스터 스코프 + `namespace` 조합은 무시되지 않고 거부. 폐기된 `namespace_list`·
  `workload_list`가 덮던 범위가 이 한 도구로 재현됨
- **검증 AC**: AC1
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestResourceListResolvesArbitraryKinds`,
  `TestResourceListRejectsNamespaceOnClusterScoped`. 통합 `resource_generic_ac1.py`

### 시나리오 2: 목록은 표로 온다
- **사전 조건**: 동일
- **실행 단계**: 같은 Deployment 목록을 (a) 도구로 조회, (b) 객체 전문 JSON으로 직렬화해
  크기를 비교
- **기대 결과**: 도구 응답이 Table 형식(컬럼 헤더 + 행)이고 객체 전문보다 현저히 작음.
  `NAME`/`READY` 등 `kubectl get`에 준하는 컬럼이 보존됨
- **검증 AC**: AC2
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestResourceListUsesTableAccept`,
  `TestTableResponseIsSubstantiallySmaller`. 통합 `resource_generic_ac2.py`

### 시나리오 3: 절단과 이어보기
- **사전 조건**: ConfigMap 120개 픽스처
- **실행 단계**: `limit` 미지정으로 조회 → `continue` 토큰으로 재조회.
  이어서 `limit=1000`으로 조회
- **기대 결과**: 1회차에 100건 + 절단 표시 + `continue` 토큰. 2회차에 나머지 20건.
  `limit=1000`은 500으로 강제 하향
- **검증 AC**: AC3
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestResourceListDefaultAndMaxLimit`.
  통합 `resource_generic_ac3.py`

### 시나리오 4: 단건 조회의 잡음 제거
- **사전 조건**: `kubectl apply`로 만들어 `last-applied-configuration`과 `managedFields`가
  모두 붙은 Deployment
- **실행 단계**: `resource_get`으로 조회
- **기대 결과**: `metadata.managedFields` 없음, `last-applied-configuration` 어노테이션 없음.
  `spec.replicas`·`status` 등 나머지는 온전
- **검증 AC**: AC4
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestResourceGetStripsNoise`.
  통합 `resource_generic_ac4.py`

### 시나리오 5: 로그 조회와 그 경계
- **사전 조건**: `workload-fixture` 기준선, 크래시 루프 파드, 다중 컨테이너 파드
- **실행 단계**: 정상 워크로드에서 로그 조회 → `tailLines=5`로 재조회 → `tailLines=5001`
  호출 → 크래시 루프 파드에 `previous=true` → 다중 컨테이너 파드에 `container` 없이 호출
- **기대 결과**: 최근 로그 반환. `tailLines=5`가 라인 수를 실제로 줄임. 5001은
  클램프되지 않고 **거부**. `previous=true`가 직전 인스턴스 로그를 반환(Running 파드가
  없어도 동작). `container` 누락이 거부되고 후보 컨테이너 이름이 제시됨
- **검증 AC**: AC5
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestResourceLogsTailBounds`,
  `TestResourceLogsPreviousInstance`. 통합 `resource_generic_ac5.py`
  (폐기되는 `workload_logs_ac{1,2,3,4}.py`의 단언을 승계한다)

### 시나리오 6: 진단 스냅샷과 이벤트 best-effort
- **사전 조건**: `workload-fixture` 기준선, 픽스처 Secret
- **실행 단계**: `resource_describe`로 파드 조회 → 이벤트 권한이 없는 배포 변형에서 재조회 →
  승인을 받아 `kind=Secret`으로 재조회
- **기대 결과**: 파드에서는 메타데이터·컨테이너 상태(state·reason·restart count·exit code)·
  conditions·최근 이벤트가 **한 응답에** 담김. 이벤트 권한이 없으면 빈 이벤트 배열로 정상
  반환되고 호출이 실패하지 않음. Secret에서는 **승인 후에도** 키 이름과 바이트 수만 나오고
  값은 나오지 않음 — 값을 보려면 `resource_get`을 따로 승인받아야 한다
- **검증 AC**: AC6
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestDescribeSecretOmitsValues`.
  통합 `resource_generic_ac6.py` (폐기되는 `pod_describe_ac{1,3}.py`의 단언을 승계한다)

### 시나리오 7: 대상 해석의 세 경로와 상호배타
- **사전 조건**: 동일
- **실행 단계**: `resource_describe`를 `name` / `labelSelector` /
  `workloadKind`+`workloadName`으로 각각 호출 → 두 경로를 동시에 지정해 호출.
  같은 조합을 `resource_logs`에도 반복
- **기대 결과**: 세 경로가 같은 파드로 해석됨. 두 경로 동시 지정은 거부. 두 도구가
  같은 해석 규칙을 공유함
- **검증 AC**: AC7
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestTargetResolutionIsExclusive`.
  통합 `resource_generic_ac7.py` (폐기되는 `pod_describe_ac2.py`의 단언을 승계한다)

### 시나리오 8: 종류 해석
- **사전 조건**: 픽스처 CRD 설치 전/후 두 상태
- **실행 단계**: `api_resources` 조회 → 존재하지 않는 `kind=Deploymnt`(오타)로
  `resource_list` → CRD 설치 후 `api_resources` 재조회 및 해당 종류 list
- **기대 결과**: 오타 호출이 거부되고 후보로 `Deployment`가 제시됨. CRD 설치 후
  `api_resources`에 나타나고 곧바로 조회됨(재기동 불필요)
- **검증 AC**: AC8
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestUnknownKindSuggestsCandidates`.
  통합 `resource_generic_ac8.py`

### 시나리오 9: 레플리카 설정과 DaemonSet 거부
- **사전 조건**: 승인된 게이트, `workload-fixture` 기준선
- **실행 단계**: `resource_scale`을 replicas=3 → 0 → 1 순으로 호출하며 각 단계 후
  `spec.replicas` 확인 → replicas=-1 호출 → replicas 누락 호출 → `kind=DaemonSet` 호출
- **기대 결과**: 3·0·1이 그대로 반영됨. 음수와 누락은 거부. DaemonSet은 거부되고
  사유가 권한이나 존재 여부가 아니라 **레플리카 부재**임이 메시지에 드러남
- **검증 AC**: AC9
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestResourceScaleBounds`,
  `TestResourceScaleRejectsReplicalessKind`. 통합 `resource_generic_ac9.py`
  (폐기되는 `workload_scale_ac{1,2}.py`의 단언을 승계한다)

### 시나리오 10: 롤링 재시작이 나머지를 건드리지 않는다
- **사전 조건**: 승인된 게이트, `workload-fixture`를 replicas=2로 세팅
- **실행 단계**: 호출 전 스펙 전문을 뜬다 → `resource_restart` 호출 → 파드 교체 대기 →
  스펙 전문을 다시 떠 대조
- **기대 결과**: `kubectl.kubernetes.io/restartedAt` 타임스탬프가 갱신되고 파드가 교체됨.
  `spec.replicas`가 2로 보존되고, 그 어노테이션 외 **어떤 필드도 달라지지 않음**
- **검증 AC**: AC10
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestResourceRestartPatchesOnlyAnnotation`.
  통합 `resource_generic_ac10.py` (폐기되는 `workload_restart_ac1.py`의 단언을 승계한다)

### 시나리오 11: Secret 읽기는 승인을 거친다
- **사전 조건**: 픽스처 Secret 1개가 실재, kind 실물 gatekeeper
- **실행 단계**: `kind=Secret`으로 `resource_get` 호출(미승인 대기) → 거절 →
  다시 호출하고 승인 → 같은 절차를 `resource_describe`로 반복 →
  `kind=ConfigMap`으로 `resource_get` 호출. 별도로 `k8s/rbac.yaml`을 정적 검사
- **기대 결과**: 미승인·거절 시 거부되고 k8s 호출 카운트 0. 승인 후 `get`이 값을,
  `describe`가 키 이름과 바이트 수를 반환. ConfigMap은 승인 요청을 만들지 않고 바로 조회됨
  (게이트가 종류로 좁혀져 있음). `rbac.yaml`의 `secrets` 규칙이 `get`·`list`만 담고
  `create`·`update`·`patch`·`delete`를 담지 않음
- **검증 AC**: AC11
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestReadGatedKindsRequireApproval`,
  `TestNonGatedKindsSkipGatekeeper`. 정적 `scripts/check_rbac_secret_verbs.py`.
  통합 `resource_generic_ac11.py`

### 시나리오 12: 값이 새는 경로가 막혀 있다
- **사전 조건**: 값이 고유 난수 토큰인 Secret, kind 실물 gatekeeper
- **실행 단계**: (a) `kind=Secret`으로 `resource_list` 호출(승인 없이). (b)
  `RESOURCE_READ_GATED_KINDS`에 픽스처 CRD를 추가하고 그 종류로 get.
  (c) `resource_exec`으로 `cat /var/run/secrets/kubernetes.io/serviceaccount/token` 시도.
  (d) Secret 매니페스트로 `resource_apply`(승인 후).
  (e) 서버의 ServiceAccount 토큰으로 `GET /api/v1/secrets?watch=true` 를 직접 호출
- **기대 결과**: (a) 승인 없이 성공하되 응답이 `NAME`/`TYPE`/`DATA`/`AGE` 컬럼만 담고
  **난수 토큰이 등장하지 않음**. (b) 승인 없이는 거부 — 게이트 종류가 설정으로 확장됨.
  (c) 게이트 미승인 상태에서 실행되지 않고, 승인 요청 `context`에 명령 전문이 그대로
  노출되어 운영자가 보고 거절할 수 있음. (d) 게이트는 통과하지만 apiserver 권한 밖이라
  AC14의 에러로 떨어짐(RBAC가 Secret 쓰기 verb를 주지 않음). (e) apiserver가 403 —
  `watch` 가 부여돼 있지 않아 게이트를 우회하는 스트림 경로가 존재하지 않음
- **검증 AC**: AC12
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestSecretListCarriesNoValues`,
  `TestNoRawListFallbackForGatedKinds`, `TestReadGatedKindsConfigurable`.
  통합 `resource_generic_ac12.py`(watch 403 확인 포함)

### 시나리오 13: 게이트가 필요한 변경만 게이트를 지난다
- **사전 조건**: 가짜 k8s 서비스(호출 카운터)
- **실행 단계**: 게이트 미승인 상태에서 (a) `resource_apply`·`resource_delete`·
  `resource_exec`을 각각 호출, (b) `resource_scale`·`resource_restart`를 각각 호출
- **기대 결과**: (a) 세 도구 모두 에러, k8s 호출 카운트 0. (b) 두 도구 모두 **정상 수행**되고
  k8s 서비스에 도달하며 `destructiveHint=true`는 유지됨 — 승인 없이 레플리카를 0으로 줄이고
  파드를 교체할 수 있다는 것이 이 설계의 알려진 대가다(`prd-approval-gate`의 "게이트 대상이
  아닌 것과 그 이유")
- **검증 AC**: AC13
- **자동화**: (미작성) — 계획: Go 단위
  `resource_test.go::TestGatedMutatingToolsBlocked`(표 기반 3종),
  `TestScaleAndRestartBypassGate`. 통합 `resource_generic_ac13.py`

### 시나리오 14: 권한 밖은 권한 밖이라고 말한다
- **사전 조건**: RBAC에 없는 종류(예: `rbac.authorization.k8s.io/v1/ClusterRole`)의 변경
- **실행 단계**: 승인을 받은 뒤 해당 종류로 `resource_apply`
- **기대 결과**: apiserver 403을 그대로 흘리지 않고, 이 서버에 부여된 권한 밖임을 밝히는
  에러를 반환. 메시지가 다른 경로 재시도를 유도하지 않음
- **검증 AC**: AC14
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestForbiddenIsTranslated`.
  통합 `resource_generic_ac14.py`

### 시나리오 15: RBAC 가 도구 표와 정확히 같다
- **사전 조건**: `k8s/rbac.yaml` 과 도구별 권한 선언
- **실행 단계**: 정적 검사로 (a) `rbac.yaml` 의 `(verb, resource[/subresource])` 쌍 집합과
  도구 표의 합집합을 양방향 대조 → (b) 금지 목록
  (`watch` · `/scale` 밖의 `update` · `deletecollection` · `pods/attach` ·
  `pods/portforward` · `*/proxy` · `secrets` 의 쓰기 verb)이 `rbac.yaml` 에 없는지 확인 →
  (c) `rbac.yaml` 에 `watch` 를 한 줄 넣은 변형으로 재실행
- **기대 결과**: (a) 양방향 어긋남 0 — 도구가 쓰지 않는 권한도, 도구에 모자란 권한도 없음.
  (b) 금지 목록 전부 부재. (c) 변형이 실패로 잡힘(검사가 실제로 물린다는 증거).
  현 `rbac.yaml` 이 워크로드에 주고 있던 `watch` 는 코드에 `Watch()` 호출이 없는 죽은
  권한이므로 이 검사가 곧바로 잡는다
- **검증 AC**: AC15
- **자동화**: (미작성) — 계획: 정적 `scripts/check_rbac_matches_tools.py`
  (뮤테이션 1건 포함). 통합 `resource_generic_ac15.py`
