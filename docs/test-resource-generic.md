# 테스트 문서: resource_* (generic resource 도구군)

## 검증 대상 AC

- AC1: 좌표로 목록 조회 (PRD: resource-generic)
- AC2: 목록은 표 형식으로 반환 (PRD: resource-generic)
- AC3: 목록 절단과 이어보기 (PRD: resource-generic)
- AC4: 단건 조회의 잡음 제거 (PRD: resource-generic)
- AC5: Secret 전면 배제 (PRD: resource-generic)
- AC6: Secret 우회 경로 차단 (PRD: resource-generic)
- AC7: 변경 동사는 승인 게이트 경유 (PRD: resource-generic)
- AC8: 종류 해석과 미지원 종류 거부 (PRD: resource-generic)
- AC9: 권한 경계의 정직한 보고 (PRD: resource-generic)

## 픽스처

기존 `tests/k8s/kind/test-deployment.yaml`에 더해, 종류 다양성을 확보할 픽스처를 추가한다
(`tests/k8s/kind/resource-generic-fixture.yaml`): Service·ConfigMap·Ingress 각 1,
목록 절단을 만들 ConfigMap 120개, CRD 1종과 그 인스턴스 1개, 그리고 **의도적으로 배치하는
Secret 1개**(배제가 실제로 동작하는지 확인하려면 대상이 실재해야 한다).

## 테스트 시나리오

### 시나리오 1: 임의 종류를 좌표로 조회한다
- **사전 조건**: 픽스처 적용
- **실행 단계**: `v1/Service`, `v1/ConfigMap`, `apps/v1/Deployment`,
  `networking.k8s.io/v1/Ingress`, 픽스처 CRD를 각각 `resource_list`로 조회.
  이어서 `labelSelector`로 좁혀 재조회. 클러스터 스코프 종류(`v1/Namespace`)에
  `namespace`를 붙여 호출
- **기대 결과**: 다섯 종류 모두 목록 반환. 셀렉터가 결과 수를 실제로 줄임.
  클러스터 스코프 + `namespace` 조합은 무시되지 않고 거부
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

### 시나리오 5: Secret은 어떤 동사로도 닿지 않는다
- **사전 조건**: 픽스처 Secret 1개가 실재
- **실행 단계**: `kind=Secret`으로 `resource_list`, `resource_get`, `resource_delete`,
  그리고 Secret 매니페스트로 `resource_apply`를 호출. 별도로 `k8s/rbac.yaml`을 정적 검사
- **기대 결과**: 네 호출 모두 거부. 거부 사유가 권한 부족이 아니라 **정책상 배제**임이
  메시지에 드러남. `rbac.yaml`에 `secrets` 리소스 규칙이 존재하지 않음.
  `resource_apply`는 게이트에 승인 요청조차 만들지 않고 거부(승인해도 통과할 수 없는 것을
  사람에게 묻지 않는다)
- **검증 AC**: AC5
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestSecretDeniedForEveryVerb`,
  `TestSecretDenialPrecedesGatekeeper`. 정적 `scripts/check_rbac_no_secrets.py`.
  통합 `resource_generic_ac5.py`

### 시나리오 6: 우회 경로도 막힌다
- **사전 조건**: 동일
- **실행 단계**: (a) Deployment + Secret이 함께 담긴 다중 문서 매니페스트로
  `resource_apply`. (b) `RESOURCE_DENIED_KINDS`에 픽스처 CRD를 추가하고 그 종류로 list·get.
  (c) `resource_exec`으로 `cat /var/run/secrets/kubernetes.io/serviceaccount/token` 시도
- **기대 결과**: (a) Deployment 부분도 적용되지 않고 **전체 거부**. (b) 두 호출 모두 거부.
  (c) 게이트 미승인 상태에서 실행되지 않으며, 승인 요청의 `context`에 그 명령 전문이
  그대로 노출되어 운영자가 보고 거절할 수 있음
- **검증 AC**: AC6
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestMultiDocManifestRejectedWhenAnyDocIsSecret`,
  `TestDeniedKindsConfigurable`. 통합 `resource_generic_ac6.py`(exec context 노출 확인 포함)

### 시나리오 7: 변경 동사는 게이트를 지난다
- **사전 조건**: 가짜 k8s 서비스(호출 카운터)
- **실행 단계**: `resource_apply`·`resource_delete`·`resource_scale`·`resource_exec`을
  게이트 미승인 상태에서 각각 호출
- **기대 결과**: 네 도구 모두 에러, k8s 호출 카운트 0.
  `test-approval-gate.md` 시나리오 1·5와 동일한 판정이 이 네 도구에 대해 성립
- **검증 AC**: AC7
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestAllMutatingResourceToolsAreGated`(표 기반 4종).
  통합 `resource_generic_ac7.py`

### 시나리오 8: 종류 해석
- **사전 조건**: 픽스처 CRD 설치 전/후 두 상태
- **실행 단계**: `api_resources` 조회 → 존재하지 않는 `kind=Deploymnt`(오타)로
  `resource_list` → CRD 설치 후 `api_resources` 재조회 및 해당 종류 list
- **기대 결과**: 오타 호출이 거부되고 후보로 `Deployment`가 제시됨. CRD 설치 후
  `api_resources`에 나타나고 곧바로 조회됨(재기동 불필요)
- **검증 AC**: AC8
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestUnknownKindSuggestsCandidates`.
  통합 `resource_generic_ac8.py`

### 시나리오 9: 권한 밖은 권한 밖이라고 말한다
- **사전 조건**: RBAC에 없는 종류(예: `rbac.authorization.k8s.io/v1/ClusterRole`)의 변경
- **실행 단계**: 승인을 받은 뒤 해당 종류로 `resource_apply`
- **기대 결과**: apiserver 403을 그대로 흘리지 않고, 이 서버에 부여된 권한 밖임을 밝히는
  에러를 반환. 메시지가 다른 경로 재시도를 유도하지 않음
- **검증 AC**: AC9
- **자동화**: (미작성) — 계획: Go 단위 `resource_test.go::TestForbiddenIsTranslated`.
  통합 `resource_generic_ac9.py`
