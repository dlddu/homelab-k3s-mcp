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
- AC11: Secret 전면 배제 (PRD: resource-generic)
- AC12: Secret 우회 경로 차단 (PRD: resource-generic)
- AC13: 변경 동사는 승인 게이트 경유 (PRD: resource-generic)
- AC14: 권한 경계의 정직한 보고 (PRD: resource-generic)

## 픽스처

폐기되는 6종의 e2e가 쓰던 `tests/k8s/kind/test-deployment.yaml`(`workload-fixture`)과
`_workload.py::ensure_workload_fixture_baseline()`을 그대로 승계한다. 6종을 지우면서 그
픽스처까지 지우면 같은 것을 다시 세우게 된다.

여기에 종류 다양성과 배제 검증을 위한 픽스처를 더한다
(`tests/k8s/kind/resource-generic-fixture.yaml`): Service·ConfigMap·Ingress 각 1,
목록 절단을 만들 ConfigMap 120개, CRD 1종과 그 인스턴스 1개, 크래시 루프 파드 1개,
다중 컨테이너 파드 1개, 그리고 **의도적으로 배치하는 Secret 1개**(배제가 실제로
동작하는지 확인하려면 대상이 실재해야 한다).

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
- **사전 조건**: `workload-fixture` 기준선
- **실행 단계**: `resource_describe`로 파드 조회 → 이벤트 권한이 없는 배포 변형에서 재조회
- **기대 결과**: 메타데이터·컨테이너 상태(state·reason·restart count·exit code)·conditions·
  최근 이벤트가 **한 응답에** 담김. 이벤트 권한이 없으면 빈 이벤트 배열로 정상 반환되고
  호출이 실패하지 않음
- **검증 AC**: AC6
- **자동화**: (미작성) — 계획: 통합 `resource_generic_ac6.py`
  (폐기되는 `pod_describe_ac{1,3}.py`의 단언을 승계한다)

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

### 시나리오 11: Secret은 어떤 동사로도 닿지 않는다
- **사전 조건**: 픽스처 Secret 1개가 실재
- **실행 단계**: `kind=Secret`으로 `resource_list`, `resource_get`, `resource_delete`,
  그리고 Secret 매니페스트로 `resource_apply`를 호출. 별도로 `k8s/rbac.yaml`을 정적 검사
- **기대 결과**: 네 호출 모두 거부. 거부 사유가 권한 부족이 아니라 **정책상 배제**임이
  메시지에 드러남. `rbac.yaml`에 `secrets` 리소스 규칙이 존재하지 않음.
  `resource_apply`는 게이트에 승인 요청조차 만들지 않고 거부(승인해도 통과할 수 없는 것을
  사람에게 묻지 않는다)
- **검증 AC**: AC11
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestSecretDeniedForEveryVerb`,
  `TestSecretDenialPrecedesGatekeeper`. 정적 `scripts/check_rbac_no_secrets.py`.
  통합 `resource_generic_ac11.py`

### 시나리오 12: 우회 경로도 막힌다
- **사전 조건**: 동일
- **실행 단계**: (a) Deployment + Secret이 함께 담긴 다중 문서 매니페스트로
  `resource_apply`. (b) `RESOURCE_DENIED_KINDS`에 픽스처 CRD를 추가하고 그 종류로 list·get.
  (c) `resource_exec`으로 `cat /var/run/secrets/kubernetes.io/serviceaccount/token` 시도
- **기대 결과**: (a) Deployment 부분도 적용되지 않고 **전체 거부**. (b) 두 호출 모두 거부.
  (c) 게이트 미승인 상태에서 실행되지 않으며, 승인 요청의 `context`에 그 명령 전문이
  그대로 노출되어 운영자가 보고 거절할 수 있음
- **검증 AC**: AC12
- **자동화**: (미작성) — 계획: Go 단위
  `resource_test.go::TestMultiDocManifestRejectedWhenAnyDocIsSecret`,
  `TestDeniedKindsConfigurable`. 통합 `resource_generic_ac12.py`

### 시나리오 13: 변경 동사는 게이트를 지난다
- **사전 조건**: 가짜 k8s 서비스(호출 카운터)
- **실행 단계**: `resource_apply`·`resource_delete`·`resource_scale`·`resource_restart`·
  `resource_exec`을 게이트 미승인 상태에서 각각 호출
- **기대 결과**: 다섯 도구 모두 에러, k8s 호출 카운트 0.
  `test-approval-gate.md` 시나리오 1·5와 동일한 판정이 이 다섯 도구에 대해 성립.
  폐기된 `workload_scale`·`workload_restart`가 열어 두던 무승인 경로가 남아 있지 않음
- **검증 AC**: AC13
- **자동화**: (미작성) — 계획: Go 단위
  `resource_test.go::TestAllMutatingResourceToolsAreGated`(표 기반 5종).
  통합 `resource_generic_ac13.py`

### 시나리오 14: 권한 밖은 권한 밖이라고 말한다
- **사전 조건**: RBAC에 없는 종류(예: `rbac.authorization.k8s.io/v1/ClusterRole`)의 변경
- **실행 단계**: 승인을 받은 뒤 해당 종류로 `resource_apply`
- **기대 결과**: apiserver 403을 그대로 흘리지 않고, 이 서버에 부여된 권한 밖임을 밝히는
  에러를 반환. 메시지가 다른 경로 재시도를 유도하지 않음
- **검증 AC**: AC14
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestForbiddenIsTranslated`.
  통합 `resource_generic_ac14.py`
