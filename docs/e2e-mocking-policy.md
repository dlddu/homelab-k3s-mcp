# E2E 모킹 최소화 정책

이 레포의 E2E는 **kind 실클러스터에 `:ci` 이미지로 실서버를 배포하고, 의존 서비스도 실제
구현체를 띄운 하네스**를 대상으로 도는 것이 기본이다 — MinIO(S3 + STS), 단일노드 OpenSearch,
실 k8s 워크로드 픽스처, 그리고 AssumeRole → SigV4 경로를 관측 가능하게 하는 http-trace
기록 프록시.

**상류 API를 흉내 낸 in-cluster 스텁 서버와 그것을 가리키는 엔드포인트 재배선**, 그리고
**실서버·실 픽스처의 충실도를 낮추는 테스트 전용 스위치**는 **실환경으로 재현이 불가능한
경우에 한해서만** 허용하며, 아래 허용목록에 등재된 것만 인정한다. 편의(실환경 준비 회피,
어서션 단순화, 플레이키 무마)를 위한 모킹은 이 정책 위반이다.

이 문서가 **정책과 허용목록의 SSOT**다. 정합 상태는 **허용목록에 등재된 지점의 집합 ==
코드에 실재하는 지점의 집합**(양방향 1:1)이며, 미등재 모킹도 코드에 없는 고아 등재도
모두 위반이다. `scripts/check_mock_policy.py`가 CI(`lint` 잡)에서 이 대조를 집행한다.

## 무엇을 "모킹"으로 세는가 (범위 경계)

이 레포는 의존 서비스의 상당수를 **실제 구현체로** 띄운다. 실 구현체를 띄운 것은 모킹이
아니라 실환경 하네스이며, 다음 둘만 모킹으로 센다.

1. **상류 스텁 서버** — 상류 API의 응답 모양만 흉내 낸 in-cluster 서버와, 그것을 가리키는
   엔드포인트 재배선.
2. **충실도 저하 스위치** — 실서버·실 픽스처를 띄우되 인증·보안 계층을 꺼서 실제 배포와
   다른 구성으로 만드는 테스트 전용 설정.

모킹이 **아닌** 것: MinIO·OpenSearch·workload·auth 픽스처처럼 실제 구현체가 도는 것 자체
(그 위에 얹힌 충실도 저하 스위치만 2번으로 센다), 그리고 http-trace 프록시 — 동작을
치환하지 않고 **관측만 하므로** 모킹이 아니라 이 정책이 권장하는 대안 쪽이다.

`internal/`의 Go 단위 테스트(`*_test.go`의 httptest 서버·fakes)는 대상이 아니다. 그 층위에서는
모킹이 정상이며, 허용목록의 스캔 범위(`tests/`·`.github/workflows/`) 밖이다.

## 허용 예외 카테고리 (이 밖은 불허)

| 코드 | 이름 | 요건 |
| --- | --- | --- |
| `UPS` | 상류 부재 | 실 상류가 외부 SaaS·유료 계정·비공개 자격증명을 요구해 CI 클러스터 안에서 구동할 수 없다. 실 상류를 CI에서 띄우거나 안전하게 호출할 수 있으면 이 카테고리를 쓸 수 없다. |
| `IMG` | 구성요소 부재 | 실 구성요소의 이미지·데이터를 CI에서 확보할 수 없어 최소 스텁으로 대체한다. 이미지가 공개되거나 CI에서 당길 수 있게 되면 이 카테고리는 소멸하고 실 구성요소로 대체해야 한다. |
| `GATE` | 게이트 완화 | 검증 대상이 아닌 인증·보안 계층을 꺼야 대상 동작이 관측 가능하다. 그 계층이 가리는 성질을 되찾는 **대체 검증 산출물**을 반드시 함께 등재해야 하며(아래 「대체 검증」 열), 그 산출물이 사라지면 CI가 실패한다. |

명시적 불허: 실 저장소가 이미 떠 있는데도 응답을 흉내 내는 데이터 모킹, 플레이키 회피,
미구현 도구의 우회, 어서션 단순화를 위한 치환.

## 표기 규약

허용된 모킹 지점에는 **그 ID 토큰이 나타나는 줄 바로 앞**에 사유 주석을 단다(YAML·Python
모두 `#` 주석).

```
# mock-exception: <CODE> — <사유>
```

체커는 주석 **다음 비어 있지 않은 줄**에서 그 ID 토큰을 찾고, `<CODE>`가 허용목록 행의
카테고리와 같은지 본다. 같은 항목이 아래 허용목록에도 있어야 한다 — 주석만 있고 미등재이거나,
등재만 있고 코드에 없으면 위반이다.

> ⚠️ `.github/workflows/ci.yml`의 `run: |` 블록 **안**에는 이 주석을 넣지 말 것. 백슬래시로
> 이어진 줄 사이에 `#`를 끼우면 뒤따르는 인자들이 통째로 셸 주석이 되어 시크릿이 조용히 잘못
> 만들어진다. 배선은 스텁 정의 파일 쪽 주석 하나로 대표하고, 워크플로의 재배선 위치는 아래
> 산문에 적는다.

> ℹ️ 이 문서의 경로(`e2e-mocking-policy.md`)는 `<이름>-mock` 패턴에 `e2e-mock` 으로 걸린다.
> 체커는 `-mock` 뒤에 영문·숫자가 이어지면 잡지 않아 오탐하지 않지만, 정합성 모델의 as-is
> **지문**은 그 구분 없이 토큰을 뽑으므로 위 주석들 때문에 지문 목록에 `e2e-mock` 이 나타난다.
> 실재하는 모킹이 아니라 **등재를 가리키는 주석**이니 새 스텁으로 오해하지 말 것.

**모킹 지점 = 스텁·스위치를 정의하는 파일**이다. 그 스텁을 소비하기만 하는 곳(테스트의 어서션,
롤아웃 대기, 실패 시 진단 덤프)은 지점이 아니라 참조이며 주석을 달지 않는다. 다만 미등재 탐지
(R2)는 스캔 범위 전체에 걸리므로, **새 스텁 이름이 어디에 나타나든** 등재 없이는 CI를 통과할 수
없다.

## 허용목록 (등재된 모킹 지점)

<!-- mock-exception-원장 -->

| ID | 카테고리 | 모킹 지점 | 대체 검증 |
| --- | --- | --- | --- |
| `github-mock` | `UPS` | `tests/k8s/kind/github-mock.yaml` | — |
| `grafana-mock` | `UPS` | `tests/k8s/kind/grafana-mock.yaml` | — |
| `dear-baby-fixture` | `IMG` | `tests/k8s/kind/dear-baby-fixture.yaml` | — |
| `MCP_AUTH_DISABLED` | `GATE` | `tests/k8s/kind/kustomization.yaml` | `tests/k8s/kind/auth-fixture.yaml` |
| `DISABLE_SECURITY_PLUGIN` | `GATE` | `tests/k8s/kind/opensearch.yaml` | `tests/k8s/kind/http-trace.yaml` |

<!-- /mock-exception-원장 -->

등재 상한: <!-- mock-exception-상한 -->5<!-- /mock-exception-상한 -->

상한은 허용목록의 행 수와 **정확히 같아야** 한다(체커 R6, 양방향). 예외를 늘리려면 같은 PR에서
이 숫자를 명시적으로 올려야 하고, 예외가 사라지면 같이 내려야 한다 — 예외는 늘지 않는 방향으로만
관리한다는 원칙을 리뷰에서 눈에 보이게 하기 위한 래칫이다. 이 문서에서 개수를 말하는 곳은
**허용목록 표와 이 상한 두 곳뿐**이고, 체커가 둘의 일치를 강제하므로 산문이 낡아 조용히 어긋날
수 없다.

### `github-mock` — `UPS`

**대상**: `api.github.com`의 installation-token 엔드포인트
(`POST /app/installations/<id>/access_tokens`).

**실환경 불가 사유**: 실 상류는 GitHub의 공개 SaaS이고, 실제 응답을 받으려면 살아 있는 GitHub
App의 private key·installation id가 필요하다. CI 클러스터 안에 GitHub를 띄울 수 없고, 실 API를
호출하면 매 PR마다 외부 의존과 실 자격증명이 생긴다. 스텁은 App JWT 서명 → 토큰 교환이라는
**클라이언트 경로 전체**를 실제로 굴리고 GitHub 모양의 결정적 응답만 돌려준다.

**배선**: `.github/workflows/ci.yml`이 이 매니페스트를 배포하고
`GITHUB_API_BASE_URL=http://github-mock.github-mock.svc.cluster.local`로 엔드포인트를 재배선한다.

**소멸 조건**: 실 GitHub App 자격증명을 CI에서 안전하게 쓸 수 있게 되거나, 상류를 CI 안에서
구동할 수 있게 되면 이 등재는 소멸하고 실 상류로 대체해야 한다.

### `grafana-mock` — `UPS`

**대상**: Grafana Cloud access-policy 토큰 엔드포인트(`POST /api/v1/tokens`).

**실환경 불가 사유**: 실 상류는 Grafana Cloud(유료 계정 + 발급자 토큰)다. CI 클러스터 안에서
구동할 수 없고, 실 계정을 쓰면 매 PR이 실 토큰을 발급하게 된다. 스텁은 Bearer 발급자 토큰과
정책 id·이름·RFC3339 만료를 담은 요청을 받아 Grafana 모양으로 되돌려주므로 클라이언트 경로가
그대로 검증된다.

**배선**: `.github/workflows/ci.yml`이 이 매니페스트를 배포하고
`GRAFANA_API_URL=http://grafana-mock.grafana-mock.svc.cluster.local/api`로 재배선한다.

**소멸 조건**: Grafana Cloud를 CI에서 안전하게 호출할 수 있게 되면 소멸한다.

### `dear-baby-fixture` — `IMG`

**대상**: dear-baby 백엔드 파드. `dear_baby_reset_user` 도구는 그 파드에 `exec`로
`/reset-user <email>`을 실행한다.

**실환경 불가 사유**: 실 dear-baby 백엔드 이미지는 비공개라 CI에서 당길 수 없고, 뒤에 데이터베이스도
필요하다. 픽스처는 busybox에 실 CLI의 성공/미발견 종료코드를 흉내 내는 스텁 스크립트를 심어,
도구가 실제로 쓰는 경로(Kubernetes `pods/exec` 스트림)는 **실물 그대로** 굴린다 — 치환된 것은
파드 안의 실행 파일 한 개뿐이다.

**소멸 조건**: dear-baby 이미지를 CI에서 당길 수 있게 되면(공개되거나 pull 자격증명이 생기면)
실 이미지로 대체해야 한다.

### `MCP_AUTH_DISABLED` — `GATE`

**대상**: 기본 kind 배포의 인증 게이트. 커버하는 것은 kind 오버레이의 configMapGenerator
(`tests/k8s/kind/kustomization.yaml`)가 넣는 `MCP_AUTH_DISABLED=1`이다.

**실환경 불가 사유**: 인증을 켜면 모든 도구 호출에 유효한 자격증명이 필요해져, **인증이 검증
대상이 아닌** 나머지 도구 e2e가 인증 설정에 종속된다. 게이트를 내려야 도구 동작을 관측할 수 있다.

**대체 검증**: `tests/k8s/kind/auth-fixture.yaml` — 같은 `:ci` 이미지를 **인증을 켠 채**
(`MCP_AUTH_DISABLED`를 일부러 두지 않고 `MCP_API_KEYS`에 단일 키만) 띄우는 별도 배포 변형.
`.github/workflows/ci.yml`의 `auth-variant` 그룹이 이 배포를 대상으로 돌며 인증 게이트와
"미설정 시 graceful 거부"를 관측한다. 즉 게이트를 켠 검증이 실재하므로 이 완화는 어떤 AC도
가리지 않는다.

**소멸 조건**: 기본 배포에서도 인증을 켜고 모든 e2e가 자격증명을 들고 돌 수 있게 정리되면 소멸한다.

### `DISABLE_SECURITY_PLUGIN` — `GATE`

**대상**: OpenSearch 픽스처(`tests/k8s/kind/opensearch.yaml`)의 security plugin.

**실환경 불가 사유**: 이 픽스처가 대신하는 실 구성요소는 **AWS OpenSearch Serverless**이고,
그 요청 인증은 AWS 관리형 front door의 **SigV4**다. 반면 `opensearchproject/opensearch` 이미지의
security plugin은 basic auth·JWT·TLS 인증서 계열이라 **SigV4를 검증하지 못한다.** 켜면 데모
설정과 인증서가 붙고 서버가 basic auth로 갈아타야 하는데, 그것은 프로덕션에 **없는** 메커니즘이라
충실도가 오히려 내려가고 `opensearch-*/AC`가 검증하는 SigV4 경로 자체가 사라진다. 즉 "게이트를 켠
별도 배포 변형"이 원리적으로 존재할 수 없다. 이 계층 자체(상류가 무서명 요청을 거부하는가)를
검증하는 우리 쪽 AC도 없다 — 상류의 동작이지 이 제품의 표면이 아니다.

**대체 검증**: `tests/k8s/kind/http-trace.yaml` — 이 완화가 가리는 성질은 "실 상류라면 무서명
요청을 거부했을 것"이다. 픽스처가 서명·무서명을 똑같이 받아 주므로 응답으로는 접근 경로를
구분할 수 없고, 그래서 기록 프록시가 **어떤 자격증명이 각 요청에 서명했는지**를 관측해
`opensearch-{search/AC3,document-put/AC4,document-delete/AC4}`·`aws-config-get/AC2`가 응답이
아니라 그 기록을 단언한다. 접근 경로를 직접 관측하는 쪽이 상류의 거부에 의존하는 것보다 강한
검증이다.

**소멸 조건**: SigV4를 실제로 검증하는 실환경 대체물(AWS 관리형 엔드포인트를 CI에서 안전하게
쓰거나, SigV4 검증 프런트를 픽스처에 세우는 것)이 생기면 소멸한다. 그 전까지 `http-trace.yaml`이
사라지면 이 등재의 근거가 무너지므로 체커가 CI를 실패시킨다.

## 충돌 시 기본 방향

"실환경 우선"이 스펙이다.

- **미등재 모킹** → 카테고리에 해당하면 등재 + 주석, 아니면 실 구현체 픽스처(MinIO·OpenSearch가
  선례다) · kind 실 워크로드 · http-trace를 통한 관측 · 게이트를 켠 별도 배포 변형으로 대체해
  모킹을 **제거**한다.
- **고아 등재** → 허용목록에서 지운다(상한도 같이 내린다).
- **카테고리 판정이 애매하면 제거 쪽으로 기운다.** 예외는 늘지 않는 방향으로만 관리한다.

## 집행 (체커)

`scripts/check_mock_policy.py` — python3 표준 라이브러리 전용, 클러스터 불필요. CI의 `lint`
잡에서 돈다. 스캔 범위는 `git ls-files -- tests .github/workflows`에서 `*_test.go`를 뺀 것으로,
정합성 모델 `tbm_homelab-k3s-mcp-e2e-mock-policy`의 as-is 지문 범위와 같다.

| 규칙 | 내용 |
| --- | --- |
| R1 | 허용목록의 카테고리는 `UPS`·`IMG`·`GATE` 중 하나다. |
| R2 | 스캔 범위에서 발견된 모킹 토큰이 전부 허용목록의 ID다(미등재 = 실패). |
| R3 | 등재된 ID가 코드에 실재하고, 선언한 모킹 지점 파일이 존재하며 그 ID를 담는다(고아 = 실패). |
| R4 | 모킹 지점마다 `# mock-exception: <CODE>` 주석이 있고 다음 비어 있지 않은 줄이 그 ID를 담으며 `<CODE>`가 행의 카테고리와 같다. 역으로 스캔 범위의 모든 `mock-exception:` 주석도 같은 조건을 만족한다. |
| R5 | `GATE` 행은 대체 검증 경로를 선언하고 그 경로가 실재한다(`UPS`·`IMG`는 `—`). |
| R6 | 등재 상한 == 허용목록 행 수(양방향). |
