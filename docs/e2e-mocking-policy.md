# E2E 모킹 최소화 정책

이 레포의 E2E는 **kind 실클러스터에 `:ci` 이미지로 실서버를 배포하고, 의존 서비스도 실제
구현체를 띄운 하네스**를 대상으로 도는 것이 기본이다 — MinIO(S3 + STS), 단일노드 OpenSearch,
실 k8s 워크로드 픽스처, 그리고 AssumeRole → SigV4 경로를 관측 가능하게 하는 http-trace
기록 프록시.

**상류 API를 흉내 낸 in-cluster 스텁 서버와 그것을 가리키는 엔드포인트 재배선**, 그리고
**실서버·실 픽스처의 충실도를 낮추는 테스트 전용 스위치**는 **실환경으로 재현이 불가능한
경우에 한해서만** 허용하며, 아래 허용목록에 등재된 것만 인정한다. 편의(실환경 준비 회피,
어서션 단순화, 플레이키 무마)를 위한 모킹은 이 정책 위반이다.

이 문서가 **정책·허용목록·차단 원장의 SSOT**다. 정합 상태는 **허용목록에 등재된 지점의
집합 == 코드에 실재하는 지점의 집합**(불변식 1, 양방향 1:1)과 아래 **차단 요인의 인계·해소
계획 추적**(불변식 2)을 함께 요구한다. 미등재 모킹과 고아 등재뿐 아니라 인계가 유실되거나
해소 계획이 빈 차단 요인도 위반이다. `scripts/check_mock_policy.py`가 CI(`lint` 잡)에서
불변식 1과 차단 행의 계획 완비(B2)를 집행한다. 외부 인계와 해소 이력 판정의 경계는 아래에 적는다.

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
| `MCP_AUTH_DISABLED` | `GATE` | `tests/k8s/kind/kustomization.yaml` · `tests/k8s/kind/gatekeeper-variant.yaml` · `tests/k8s/kind/gate-broken-variant.yaml` | `tests/k8s/kind/auth-fixture.yaml` |
| `DISABLE_SECURITY_PLUGIN` | `GATE` | `tests/k8s/kind/opensearch.yaml` | `tests/k8s/kind/http-trace.yaml` |

<!-- /mock-exception-원장 -->

등재 상한: <!-- mock-exception-상한 -->4<!-- /mock-exception-상한 -->

상한은 허용목록의 행 수와 **정확히 같아야** 한다(체커 R6, 양방향). 예외를 늘리려면 같은 PR에서
이 숫자를 명시적으로 올려야 하고, 예외가 사라지면 같이 내려야 한다 — 예외는 늘지 않는 방향으로만
관리한다는 원칙을 리뷰에서 눈에 보이게 하기 위한 래칫이다. 이 문서에서 개수를 말하는 곳은
**허용목록 표와 이 상한 두 곳뿐**이고, 체커가 둘의 일치를 강제하므로 산문이 낡아 조용히 어긋날
수 없다. 그 전제(산문이 개수를 말하지 않는다) 자체도 체커 R7 이 집행한다 — 전제가 규율일
뿐이던 동안 실제로 산문 세 자리가 마커를 따라오지 못하고 조용히 거짓이 됐다.

### `github-mock` — `UPS`

**대상**: `api.github.com`의 App 엔드포인트 —
`POST /app/installations/<id>/access_tokens`(토큰 발급) ·
`GET /app/installations/<id>`(설치 권한과 설치 계정 `account.login` 조회) ·
`DELETE /installation/token`(발급 토큰 폐기) ·
`POST /repos/<owner>/<repo>/statuses/<sha>`(커밋 status 기록).
앞의 셋은 `github_app_installation_token` 의 AC5 경로가 거치는 상류 호출 전부이고, **넷 전부가**
`github_commit_status_create` 가 거치는 전부다 — 그 도구는 owner 를 호출자에게서 받지 않고
`GET /app/installations/<id>` 응답의 `account.login` 에서 읽은 뒤(`internal/github` 의
`installationOwner`) 좁은 토큰을 발급해 status 를 쓰고 그 토큰을 폐기한다. 그래서 설치 조회는
권한뿐 아니라 **계정 login 까지** 돌려주어야 한다.

status 기록은 `sha` 가 `f` 40자일 때만 GitHub 모양의 `422 Validation Failed` 를 돌려주고 그 밖의
`sha` 에는 `201` 과 생성된 status 를 돌려준다. 실패 갈래를 `/_admin/config` 노브가 아니라 **sha
로** 가른 것은 [`test-github-commit-status.md`](test-github-commit-status.md) 시나리오 2 가 성공
호출과 실패 호출을 **한 구성 상태에서** 차례로 한 뒤 두 발급 본문을 함께 읽기 때문이다 — 노브면
그 사이에 구성을 갈아야 하고, 그러면 한 창에서 두 본문을 재는 판정이 성립하지 않는다.

여기에 더해 **테스트 전용 관리 표면**을 같은 프로세스가 연다(`/_admin/requests` 로 받은 요청을
돌려주고 `/_admin/config` 로 응답 모드를 바꾼다). GitHub 에는 없는 경로이므로 상류 흉내가
아니라 픽스처의 노브이고, `/_admin/` 접두사 아래에만 있어 실 API 경로와 겹치지 않는다.
`AC5` 의 주장 대부분이 「일어나면 안 되는 요청」이라 **반환값이 아니라 요청 기록**이 판정
근거이고, 그 기록을 e2e 가 읽을 자리가 필요하다.

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

### 해소된 등재 — `dear-baby-fixture` (`IMG`, 2026-09-26 삭제)

기록으로만 남긴다. 이 자리에는 busybox 파드에 `/reset-user` 스텁 스크립트를 심은
`tests/k8s/kind/dear-baby-fixture.yaml` 이 `IMG` 로 등재돼 있었고, 사유는 「실 dear-baby 백엔드
이미지는 비공개라 CI에서 당길 수 없고, 뒤에 데이터베이스도 필요하다」, 소멸 조건은 「이미지를 CI에서
당길 수 있게 되면」이었다. **소멸 조건은 이미 도래해 있었고 사유 두 절은 실측으로 반증됐다**
(`tbm_homelab-k3s-mcp-e2e-mock-policy/rct_20260926-0004`):

- **이미지는 공개 패키지다.** 자격증명 없이 받은 익명 토큰으로 `ghcr.io/dlddu/dear-baby` 의
  태그 목록과 매니페스트·레이어를 그대로 받았다(대조군으로 없는 이름은 `DENIED` 를 돌려준다).
  이 레포는 이미 같은 org 의 실 이미지 셋을 당기고 있고 `imagePullSecrets` 는 0건이다.
- **그 이미지가 실 CLI 를 담고 있다.** 운영이 핀한 태그의 arm64 레이어에 `/reset-user` 가
  실재한다. 이 레포 CI 러너가 arm 이라 플랫폼도 맞는다.
- **「뒤에 필요한 데이터베이스」는 SQLite 파일 하나다.** `/reset-user` 의 기본 `DATABASE_URL` 이
  `file:/data/dear-baby.db` 이고, 서버가 기동할 때 마이그레이션을 돌린 뒤 `TEST_USER_EMAIL` /
  `TEST_USER_PASSWORD` 로 사용자 행을 멱등 시드한다 — 시나리오 1 이 리셋하는 그 사용자다.

즉 남아 있던 것은 *불가능*이 아니라 *미비*였고, 이 정책의 「실환경으로 준비 가능하면 어떤 카테고리도
쓸 수 없다」가 등재를 금지한다. 대체물은 `tests/k8s/kind/dear-baby.yaml` — 같은 네임스페이스에
운영과 같은 이미지·같은 redis 를 세우고, 도구는 스텁이 아니라 **실 `/reset-user` 바이너리**를
exec 한다. e2e 단정은 성공 출력 한 줄만 실 CLI 의 문면(`reset user <email>`)으로 바뀌었고
미발견 경로는 무수정으로 통과한다.

**이 해소가 등재를 늘리지 않았다는 것**: 새 픽스처의 redis·backend 는 둘 다 실 구현체라
이 문서가 세는 두 부류(상류 스텁 서버 · 충실도 저하 스위치) 어느 쪽도 아니다. 기동을 통과시키는
`AWS_REGION`·`AWS_S3_BUCKET` 더미 값은 이 e2e 가 밟는 경로(`pods/exec`)에 얹힌 스위치가 아니고
그 클라이언트는 생성만 되므로(assume-role 미설정) 치환이 아니다 — `gatekeeper-variant` 의
VAPID 더미와 같은 판정이다.

### `MCP_AUTH_DISABLED` — `GATE`

**대상**: 인증이 검증 대상이 아닌 e2e를 태우는 **배포들**의 인증 게이트. 커버하는 것은 이
등재가 선언한 세 지점이 넣는 `MCP_AUTH_DISABLED=1`이다.

| 지점 | 무엇을 태우는가 |
| --- | --- |
| `tests/k8s/kind/kustomization.yaml` | 기본 kind 배포. kind 오버레이의 configMapGenerator가 넣는다. |
| `tests/k8s/kind/gatekeeper-variant.yaml` | 승인 게이트 e2e 전용 배포 변형(`approval_gate_ac{2,4,9}.py`). Deployment의 `env`가 직접 넣는다 — env는 배포당이라 기본 배포의 값을 물려받을 수 없다. |
| `tests/k8s/kind/gate-broken-variant.yaml` | 승인 게이트 **오구성** e2e 전용 배포 변형 **셋**(`approval_gate_ac5.py`의 연결 실패 · `GATEKEEPER_BASE_URL` 미설정 · `GATEKEEPER_API_KEY` 미설정 경로). 검증 대상은 `gatekeeper.FromEnv()`의 오구성 분기이지 인증이 아니다. 세 Deployment의 `env`가 각각 직접 넣는다 — 같은 이유로 하나로 접을 수 없다. |

세 지점은 **같은 완화**(`:ci` 이미지의 인증 게이트를 내린다)이고 같은 대체 검증을 공유하므로 한
행으로 등재한다. 배포가 늘었다고 예외가 는 것이 아니다 — 이 절은 행을 늘리지도 상한을
올리지도 않는다. 현재값은 위 허용목록 표와 상한 마커가 말한다.

**실환경 불가 사유**: 인증을 켜면 모든 도구 호출에 유효한 자격증명이 필요해져, **인증이 검증
대상이 아닌** 나머지 도구 e2e가 인증 설정에 종속된다. 게이트를 내려야 도구 동작을 관측할 수 있다.
승인 게이트 변형도 같다 — 그 파일들의 검증 대상은 gatekeeper 왕복이지 인증이 아니다.

**대체 검증**: `tests/k8s/kind/auth-fixture.yaml` — 같은 `:ci` 이미지를 **인증을 켠 채**
(`MCP_AUTH_DISABLED`를 일부러 두지 않고 `MCP_API_KEYS`에 단일 키만) 띄우는 별도 배포 변형.
`.github/workflows/ci.yml`의 `auth-variant` 그룹이 이 배포를 대상으로 돌며 인증 게이트와
"미설정 시 graceful 거부"를 관측한다. 즉 게이트를 켠 검증이 실재하므로 이 완화는 어떤 AC도
가리지 않는다.

이 대체 검증은 **세 지점 모두에 유효하다.** 게이트를 내려 가려지는 성질은 「이 `:ci` 이미지가
자격증명 없는/잘못된 요청을 거부하는가」이고, 그것은 **이미지의 성질**이지 배포 변형의 성질이
아니다 — `internal/server/server.go`의 같은 코드 경로가 모든 변형에서 돈다. 따라서 게이트를 켠
배포가 **하나라도** 실재하면 그 성질은 되찾아지며, 변형마다 auth 픽스처를 복제할 필요가 없다.
역으로 게이트 완화를 **새로** 넣는 배포는 이 행의 지점 열에 자기를 추가해야 하고, 그 배포의
검증 대상이 인증이 아님을 위 표에 적어야 한다.

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

## 차단 원장 (불변식 2)

차단 요인은 E2E가 어떤 경로를 밟지 못하는 원인이다. **승인된 모킹이 아니며**, 여기에 적는
행은 새 스텁을 허용하지 않는다. 차단 행 수는 허용목록 상한에 더하지 않는다.

- **B1 인계 수신**: 이 정책의 다른 절이나 다른 reconciler 모델의 task가 이 모델에 넘긴
  실제 차단 요인마다 행을 둔다. 단순한 소관 언급과 이미 해소된 이력은 새 인계로 세지 않는다.
- **B2 계획 완비**: 해소 방향·선행·재검토를 각각 셀에 적는다. 선행이 없으면
  `없음`이라고 적고, 그러면 그 행은 착수 가능한 부채다. 빈 값이나 `미정`은 계획이 아니다.
- **B3 변화 추적**: 증가한 행은 B1·B2를 확인하고, 줄어든 행은 실제 해소 증거를 확인한다.
  행 수가 그대로면 재검토 날짜·사건의 도래를 확인한다. 수신 누락, 계획 미비, 착수 가능,
  재검토 도래, 증거 없는 삭제 중 하나라도 있으면 reconciler가 조치 대상으로 판단한다.
  선행 대기만 남으면 행과 재검토 시점을 잔여 부채로 계속 기록한다.
- **B4 소관 고정**: 원장에 올라온 차단 요인의 해소 주체는 **항상 이 모델**
  (`tbm_homelab-k3s-mcp-e2e-mock-policy`)이다. 다른 모델·사람·외부로 소관을 넘기는 것은
  조건 없이 금지이고, **그래서 원장에 소관 칸을 두지 않는다** — 적을 자리가 없으면 넘길
  수도 없다. 받는 인계(B1)는 허용되지만 내보내는 인계는 없다. 선행을 만드는 다른 소관이
  있으면 그것은 소관이 아니라 `선행` 셀에 적는다.

### 기계 판독 형식

마커 안에는 아래 6열 표만 둔다. 진짜 차단 요인이 없을 때도 헤더·구분선·마커는 유지한다.

- `ID`: 중복 없는 영문 소문자·숫자·하이픈 식별자.
- `출처`: `tbm_<model>/rct_<task>` 또는 이 문서의 `policy:#anchor`.
- `등록일`: `YYYY-MM-DD`. 최초 등록일을 보존한다.
- `해소 방향`: `real-environment`(실환경 대체) 또는 `exception-registration`(실환경 불가
  증거를 확보한 뒤 허용 카테고리 등재로 해소). 후자도 **등재 승인 자체가 아니다**.
- `선행`: 구체적인 산출물·증거와 그 소관, 또는 `없음`.
- `재검토`: 등록일부터 90일 이내의 `YYYY-MM-DD`, 또는 관측 가능한 사건 하나:
  `file:<레포 상대 경로>`(파일 등장), `task:tbm_<model>/rct_<task>`(그 task의 성공 종료),
  `pr:<owner>/<repo>#<번호>`(그 PR 머지). 사건의 실제 도래와 날짜 경과는 B3 판정이다.

<!-- mock-blocker-원장 -->

| ID | 출처 | 등록일 | 해소 방향 | 선행 | 재검토 |
| --- | --- | --- | --- | --- | --- |

<!-- /mock-blocker-원장 -->

### `approval-gate-ac5-http-failures` 해소 순서

인계 근거는 [원본 task의 첫 시도 실행 지침](https://github.com/dlddu/reconciler/blob/dcdb9df5046cfa272d88a7fab76cf2899334174e/data/reconciliation-tasks/tbm_homelab-k3s-mcp-scenario-e2e.json)의
「범위 밖」 2번이다. [PR #92](https://github.com/dlddu/homelab-k3s-mcp/pull/92)는 문서 근거를
고친 슬라이스이며 이 주입 수단을 구현하지 않았다. 현재 인계는 [doc-tracker](doc-tracker/index.md)의
`test-approval-gate.md#시나리오 5` 구현 대기 행에도 남아 있다.

1. ~~`tbm_homelab-k3s-mcp-scenario-e2e`가 실물 gatekeeper 이미지·kind 픽스처를 확보한다.~~
   **충족 (2026-09-18).** [PR #106](https://github.com/dlddu/homelab-k3s-mcp/pull/106)이
   실물 `tests/k8s/kind/gatekeeper-fixture.yaml`(`ghcr.io/dlddu/gatekeeper:sha-762fafe` +
   SQLite PVC)과 배포 변형 `gatekeeper-variant.yaml`, 전용 e2e `approval_gate_ac{2,4,7,8,9}.py`를
   착지시켰다. **`approval_gate_ac5.py`는 여전히 없다** — 착지분에 AC5는 포함되지 않았다.
   `internal/gatekeeper/gatekeeper.go::randomExternalID`는 여전히 호출마다 난수 ID를 생성하므로,
   단순히 같은 도구를 두 번 부르는 것으로 409 재현이 된다고 가정하지 않는다.
2. ~~그 소관이 실제 승인 클라이언트를 통과하는 409·5xx 경로의 재현 가능성을 조사하고 요청·응답과
   대상 리소스 미변경 증거를 남긴다. 직접 gatekeeper API에 오류를 만든 것만으로 MCP의
   fail-closed 검증을 대신하지 않는다. 인계문에 적힌 「실물로 만들 수 없다」는 말은
   **검증할 제약**이며 모든 실환경 주입법이 불가능하다는 입증으로 취급하지 않는다.~~
   **충족 (2026-09-26).** 인계문의 「실물로 만들 수 없다」는 **반증됐다** — 아래
   「2번 관측 로그」. 실물 gatekeeper 가 자기 핸들러로 409·500 을 내고, 그 응답이 실제
   승인 클라이언트(`internal/gatekeeper.Client.Authorize`)를 통과해 거부로 수렴함을 관측했다.
   선행 셀은 이로써 `없음` 이 되고 이 행은 **이 모델이 착수할 3번**만 남긴다.
3. ~~이 모델이 그 증거를 받아 실환경 대체를 확정한다. 불가능하면 이유·대안·최소 치환 범위와
   `UPS`·`IMG`·`GATE` 요건을 대조해 별도 등재 판단을 한다. 허용 카테고리에 맞지 않는
   편의용 주입 스텁은 등재하지 않는다. 난수 ID를 고정하는 제품 스위치도 이 행이 허용하지 않는다.~~
   **충족 (2026-09-26).** 판정은 아래 「실환경 주입 판정」 절이다. 결론만 적으면 —
   마커 한정 `BEFORE INSERT` 주입은 **실환경 대체로 확정**한다(`해소 방향` = `real-environment` 유지).
   이 정책이 세는 두 모킹 부류 어느 쪽도 아니고 「응답을 흉내 내는 데이터 모킹」에도 걸리지 않으므로
   **허용목록에 행을 늘리지 않고 등재 상한도 올리지 않는다**. 단 무조건 허용이 아니라 그 절의 조건
   C1~C5 안에서만이며, 난수 ID 고정 같은 제품 개작은 여전히 불허다. 이로써 이 행에 남은 것은
   4번뿐이라 그 시점의 관례대로 **`소관` 셀을 `tbm_homelab-k3s-mcp-scenario-e2e` 로 넘겼다** —
   행과 재검토 날짜는 유지했다. ⚠️ 그 소관 이전은 **지금 규약에서는 금지**이고(B4) 원장에는
   소관 칸 자체가 없다 — 위 문장은 그때 무엇을 했는지의 보관 기록이지 따라 할 관례가 아니다.
4. ~~`tbm_homelab-k3s-mcp-scenario-e2e`가 [시나리오 5](test-approval-gate.md)의 여덟 경로와
   읽기 대조군을 완성한다. 실제 대체물 또는 승인된 등재와 검증 증거가 착지한 뒤에만 이 행을
   해소하며, 행 삭제 PR에 그 근거를 연결한다. 주입을 쓰는 경우 아래 「실환경 주입 판정」의
   조건 C1~C5를 만족하는 범위에서만 쓴다.~~
   **충족 (2026-09-26, `tbm_homelab-k3s-mcp-scenario-e2e/rct_20260926-0001`) — 이 행은 해소됐고
   위 표에서 뺐다.** B3가 요구하는 해소 증거는 셋이다:

   - **대체물이 실재한다**: `tests/integration/approval_gate_ac5.py` 가 여덟 경로
     (`REJECTED`·만료·판정 없음·409·5xx·연결 실패·`GATEKEEPER_BASE_URL` 미설정·
     `GATEKEEPER_API_KEY` 미설정)와 읽기 대조군을 덮는다. 주입기는
     `tests/k8s/kind/gatekeeper-injector.yaml`, 미설정·불통 배포는
     `tests/k8s/kind/gate-broken-variant.yaml` 이다.
   - **등재를 늘리지 않았다**: 그 슬라이스는 허용목록에 행을 더하지 않았고 상한도 올리지
     않았다(`scripts/check_mock_policy.py` 의 등재/상한 출력이 그 PR 에서 불변이었다).
     3번 판정대로 주입은 이 정책이 세는 모킹이 아니므로 등재 대상이 아니다. 이 항목이
     기록하는 것은 *그 슬라이스의 델타가 0이었다*는 사실이지 그때의 절대 수치가 아니다.
   - **C1~C5 안에 있다**: C1 응답은 실물 핸들러·실물 고유 인덱스가 만든다(테스트는 409·500 을
     *관측*할 뿐 문자열을 짓지 않는다) · C2 트리거 WHEN 절은
     `instr(NEW.context, '<대상 이름>') > 0` 이고 폴링 `BEGIN EXCLUSIVE` 는 쓰지 않았다 ·
     C3 제품 경로(`internal/`·`cmd/`·`main.go`·`k8s/`)와 `gatekeeper-fixture.yaml` 접촉 0줄 ·
     C4 주입기가 arm 때 프로브로 트리거 발화를 확인하고 테스트가 케이스마다·실행 끝에
     `triggers: [] · shadow_rows: 0` 을 스스로 단언한다 · C5 거부가 대상 ConfigMap 을 바꾸지
     않았음을 여덟 경우 모두 클러스터에서 읽어 확인한다.

   「2번 관측 로그」가 4번의 첫 확인 항목으로 넘긴 미관측 사항(주입기가 `gatekeeper-pvc` 를
   함께 마운트할 수 있는가, uid 1001 로 DB·저널에 쓸 수 있는가)은 이 슬라이스가 닫았다 — kind 는
   단일 노드라 ReadWriteOnce PVC 를 같은 노드의 두 파드가 함께 마운트하고, 주입기는 픽스처와
   같은 uid·fsGroup 1001 로 돈다.

실물 픽스처가 먼저 착지하면 그때 재검토하고, 아직 없더라도 표의 날짜에 다시 본다.
이 원장을 받는 슬라이스는 E2E 구현 대기 행을 해제하거나 새 모킹을 승인하지 않는다.

**재검토 기록 (2026-09-18, `tbm_homelab-k3s-mcp-e2e-mock-policy/rct_20260918-0001`)** — 위 1번의
사건이 도래해 표의 날짜(`2026-10-15`)보다 먼저 재검토했다. 판정: **행을 유지한다.**
선행의 절반(실물 픽스처)만 충족됐고 나머지 절반(409·5xx 재현 가능성의 관측 로그)은 미착지라
`해소 방향`(`real-environment`)을 확정할 증거가 아직 없다. 선행 셀은 남은 절반만 남기도록
갱신했고, **재검토 날짜는 옮기지 않았다** — 날짜는 사건이 오지 않을 때의 backstop이고, 미룰수록
조용한 영구 면제에 가까워진다.

> ⚠️ **선행 미해소가 만드는 위험(소관 = `tbm_homelab-k3s-mcp-scenario-e2e`)**:
> [`test-approval-gate.md` 시나리오 5](test-approval-gate.md)의 **사전 조건이 아직
> 「가짜 k8s 서비스(호출 카운터), gatekeeper 스텁을 경로별로 구성」**이라 적는다. 실물
> gatekeeper 픽스처가 착지한 지금 그 문면대로 스텁을 만들면 **이 정책의 불변식 1을 새로
> 깬다**(등재 없는 새 모킹). 위 2번을 수행하는 슬라이스는 그 사전 조건 문면을 실물 기반으로
> 먼저 고치거나, 실물로 409·5xx를 낼 수 없다는 증거를 들고 이 원장으로 돌아와야 한다.
> 그 문서의 소관은 자매 모델이므로 이 행은 위험만 인계하고 문면을 직접 고치지 않는다.
>
> **해소 (2026-09-26)** — 2번을 수행한 슬라이스가 그 사전 조건을 실물 기반으로 고쳤다(단위는
> `httptest`, E2E 는 실물 + 3번 판정 대기 중인 주입 수단).

**2번 관측 로그 (2026-09-26)** — 3번 판정의 입력이다. **이 절은 등재도 승인도 아니다.**

*gatekeeper 쪽 사실(`dlddu/gatekeeper` @`762fafe`, 픽스처가 핀한 `sha-762fafe` 와 같은 커밋).*
`POST /api/requests` 의 409 는 `store.isUniqueViolation`(에러 문자열 `UNIQUE constraint failed`)
에서만 나오고, 그 제약은 `Request_externalId_key` 고유 인덱스뿐이다. 그 밖의 저장소 오류는 전부
`httpx.InternalError` → 500 이다. 저장소는 SQLite(rollback 저널, `busy_timeout=5000`, 연결 1개)이고
픽스처에서는 `gatekeeper-pvc` 의 `/app/data/gatekeeper.db` 다. MCP 쪽 `randomExternalID` 는
16바이트 난수라 MCP 가 스스로 충돌을 만들 수는 없다 — 이 부분의 인계문은 참이다.

*수단.* 실물 DB 파일에 **다른 프로세스가** `BEFORE INSERT` 트리거를 건다. 조건은
`instr(NEW.context, '<마커>') > 0` 로, `tests/integration/_gatekeeper.py` 의 마커 격리를 그대로
따르므로 다른 레인의 요청에는 걸리지 않는다. 제품 코드·빌드·배포 env 는 바꾸지 않는다.

- **409** — 트리거가 **같은 `externalId`** 의 그림자 행을 먼저 INSERT 한다. 본 INSERT 는 진짜
  `UNIQUE constraint failed: Request.externalId` 로 실패하고 실물 핸들러가
  `409 {"error":"Request with this externalId already exists"}` 를 낸다. 문장 단위 롤백으로
  그림자 행도 남지 않는다.
- **5xx(생성)** — 트리거가 `RAISE(ABORT, …)` 한다 → 실물 `InternalError` 500.
- **5xx(폴링, 선택)** — 생성 뒤 다른 연결이 `BEGIN EXCLUSIVE` 를 `busy_timeout` 보다 오래 쥔다 →
  단건 조회가 `SQLITE_BUSY` 500. **전역 잠금**이라 병렬 레인 전부를 막으므로 격리 레인 없이는
  쓰지 않는다. AC5 의 「5xx」 는 생성 경로로 충족된다.

```sql
-- 409 (마커 한정, 그림자 행은 문장 롤백으로 사라진다)
CREATE TRIGGER e2e_collide BEFORE INSERT ON "Request"
WHEN instr(NEW.context, '<marker>') > 0
BEGIN
  INSERT INTO "Request"(id, externalId, context, requesterName, status, updatedAt)
  VALUES (NEW.id || '-shadow', NEW.externalId, 'e2e-shadow', 'e2e-shadow', 'PENDING', NEW.updatedAt);
END;
-- 5xx
CREATE TRIGGER e2e_fail BEFORE INSERT ON "Request"
WHEN instr(NEW.context, '<marker>') > 0
BEGIN SELECT RAISE(ABORT, 'e2e injected storage failure'); END;
```

*관측(로컬).* 같은 커밋의 gatekeeper 백엔드를 빌드해 기동하고, 이 레포의
`gatekeeper.Client.Authorize` 를 그대로 불렀다. 트리거는 서버가 도는 중에 별도 프로세스(Python
`sqlite3`)가 걸었고 즉시 반영됐다.

| 경우 | 클라이언트가 본 거부 | gatekeeper 액세스·오류 로그 | 잔여 행 |
| --- | --- | --- | --- |
| 409 | `unreachable` · `approval request id collided (409)` | `POST /api/requests - 409` | 0 |
| 5xx(생성) | `unreachable` · `approval backend returned 500` | `[ERROR] … constraint failed: e2e injected storage failure (1811)` · `- 500` | 0 |
| 5xx(폴링) | `unreachable` · `… 500 while polling` (requestID 유지) | `[ERROR] GET … database is locked (5) (SQLITE_BUSY)` · 5.0s | — |
| 격리 대조 | 다른 마커는 `201` 로 생성되고 `timeout` 으로 끝남 | `- 201` | — |

*대상 리소스 미변경에 대해.* 로컬 관측은 클라이언트가 `Decision` 없이 거부를 돌려준다는 데까지다.
그 거부가 k8s 호출 전에 끊긴다는 것은 `internal/mcp` 디스패처의 구조(게이트 오류는 핸들러 실행
전에 반환)이고, **클러스터에서 대상 객체가 그대로임을 관측하는 것은 4번의 e2e 몫**이다. kind 에서의
실행(주입기가 `gatekeeper-pvc` 를 함께 마운트할 수 있는지, uid 1001 로 DB·저널 디렉터리에 쓸 수
있는지)도 아직 관측하지 않았다 — 4번 슬라이스의 첫 확인 항목이다.

*3번이 판정할 것(사실만 적는다).* 주입은 상류 스텁 서버도 아니고 실서버·픽스처 구성을 낮추는
스위치도 아니다 — 응답은 실물 핸들러와 실물 고유 인덱스가 만든다. 반면 테스트가 실물 저장소의
상태(스키마 객체)를 조작한다는 점에서 「데이터 모킹」 불허 조항과의 관계는 이 모델이 판정한다.
주입기 Pod 이미지는 하네스에 이미 있는 `python:3.12-slim` 이면 족하다(stdlib `sqlite3`).
난수 ID 를 고정하는 제품 스위치는 필요 없다.

### 실환경 주입 판정 (3번 결과, 2026-09-26)

해소 순서 3번의 판정이다. 입력은 위 「2번 관측 로그」이고, 판정 주체는
`tbm_homelab-k3s-mcp-e2e-mock-policy/rct_20260925-0002` 다.

**판정: 마커 한정 `BEFORE INSERT` 주입은 이 정책이 세는 모킹이 아니며, 「데이터 모킹」 불허
조항에도 걸리지 않는다. 따라서 `해소 방향` 은 `real-environment` 로 확정하고, 허용목록에는
아무 행도 등재하지 않는다(등재도 상한도 불변).** 단 무조건 허용이 아니라 아래 조건
C1~C5 안에서만이다.

**근거 — 이 문서의 문면과 1:1 대조.**

1. **부류 1(상류 스텁 서버)이 아니다.** 상류 API의 응답 모양을 흉내 낸 in-cluster 서버가 없고
   엔드포인트 재배선도 없다. 요청은 픽스처가 핀한 실물 `ghcr.io/dlddu/gatekeeper:sha-762fafe`
   가 받는다.
2. **부류 2(충실도 저하 스위치)가 아니다.** 끄는 인증·보안 계층이 없고 제품 코드·이미지·빌드
   플래그·배포 env 를 바꾸지 않는다. 대비하면 `MCP_AUTH_DISABLED`·`DISABLE_SECURITY_PLUGIN` 은
   **배포 구성 자체**를 실제와 다르게 만들지만, 주입은 배포 구성을 그대로 두고 저장소의 상태만
   건드린다.
3. **「실 저장소가 이미 떠 있는데도 응답을 흉내 내는 데이터 모킹」에 걸리지 않는다.** 이 조항의
   판별식은 **누가 응답을 만드는가**다. 409 의 status·본문은 실물 `store.isUniqueViolation`
   (`Request_externalId_key` 고유 인덱스)을 타고 실물 핸들러가 만들고, 500 은 실물
   `httpx.InternalError` 가 만든다. 주입이 만드는 것은 **응답이 아니라 전제**(고유 인덱스 충돌 ·
   저장소 오류)다. 테스트는 응답 문자열을 쓰지도 읽어 흉내 내지도 않는다.
4. **이 문서가 이미 「모킹 아님」으로 분류한 것들과 같은 축이다.** http-trace 는 동작을 치환하지
   않고 관측만 하므로 모킹이 아니고, MinIO·OpenSearch 는 실제 구현체가 도는 것 자체라 모킹이
   아니다. 주입은 관측이 아니라 개입이지만 **치환이 아니다** — 실물 구성요소가 자기 오류 분기를
   실제로 타게 만드는 결함 주입이다.
5. **편의 목적이 아니다.** 불허 사유로 열거된 셋(실환경 준비 회피 · 어서션 단순화 · 플레이키
   무마) 어디에도 해당하지 않는다. 실물 픽스처를 띄운 위에서 실물 오류 경로를 밟기 위한 수단이고,
   유일한 대안은 난수 ID 를 고정하는 제품 개작인데 그것은 3번이 명시로 금지했다.

**조건 C1~C5 — 판정의 범위. 4번이 따라야 하는 스펙이다.** 하나라도 벗어나면 위 3번의 근거가
그대로 뒤집혀 「응답을 흉내 내는 데이터 모킹」이 되므로, 그때는 이 판정이 아니라 새 판정이 필요하다.

- **C1 응답 출처**: status·본문은 실물 핸들러와 실물 제약이 만든다. 트리거가 응답 문자열을 만들거나
  HTTP 계층을 흉내 내면 이 판정 밖이다.
- **C2 마커 한정**: 조건은 `instr(NEW.context, '<마커>') > 0` 처럼 그 레인의 마커에만 걸린다.
  무조건 트리거는 이 판정 밖이며, 폴링 5xx 의 `BEGIN EXCLUSIVE` 는 전역 잠금이라 격리 레인 없이
  쓰지 않는다(AC5 의 「5xx」 는 생성 경로로 충족된다).
- **C3 제품 무변경**: 제품 코드·이미지·빌드 플래그·배포 env 를 바꾸지 않는다. 난수 ID 를 고정하는
  제품 스위치는 이 행이 허용하지 않는다.
- **C4 잔여 0**: 주입한 스키마 객체는 그 테스트 안에서 만들고 지운다. 잔여 트리거·잔여 행 0 을
  테스트가 스스로 확인한다.
- **C5 실환경 관측**: 거부가 대상 리소스를 바꾸지 않았음을 **클러스터에서** 관측한다. 로컬 관측만으로
  이 경로를 완료로 적지 않는다.

**등재하지 않는 이유 — 세 카테고리 요건 대조.** `UPS` 는 실 상류를 CI 에서 띄울 수 없을 때만
쓸 수 있는데 gatekeeper 는 이미 CI 클러스터 안에서 실물로 돈다. `IMG` 는 실 구성요소의 이미지·
데이터를 확보할 수 없을 때인데 이미지가 이미 있고 픽스처가 핀하고 있다. `GATE` 는 끄는 인증·보안
계층과 그것을 되찾는 대체 검증 산출물을 요구하는데 끄는 계층이 없어 대체 검증 대상도 없다. 셋 다
불성립이고, 「카테고리 판정이 애매하면 제거 쪽으로 기운다」는 원칙도 같은 방향이다 — 이 판정은
예외를 하나도 늘리지 않는다.

**집행의 한계 — 이 계열을 어디서 다시 판정하는가.** 주입은 `<이름>-mock`·`mock-exception:`·알려진
두 스위치 중 아무 토큰도 코드에 남기지 않으므로, R2·R4 와 정합성 모델의 as-is 지문은 이 계열에
**구조적으로 침묵**한다. 불변식 1 이 이것을 잡아 줄 것으로 기대하지 말 것. 대신 판정 지점은 불변식 2
안에 있다 — 이 행은 4번이 착지할 때까지 남고, 행 삭제 PR 은 해소 증거를 연결해야 하며(증거 없는
삭제는 B3 위반), 사건이 오지 않으면 `2026-10-15` 재검토가 backstop 이다. 주입기를 코드로 착지시키는
PR 의 리뷰가 C1~C5 를 대조하는 자리다.

### 집행 경계

CI의 B2는 표 구조·필수 셀·해소 방향·재검토 형식과 90일 상한을 검사하고, 소관 칸이 없는 것(B4)도
열 집합으로 집행한다. 외부 task를 자동으로 가져오지 않으므로 **B1 인계 전수 발견**, 선행의 실제 충족, 날짜·사건 도래와
삭제 행의 해소 증거를 평가하는 **B3 자동화는 후속 범위**다. 그 판정은 reconciler 모델이
정책과 다른 모델 task, 이전 관측을 대조해 수행한다. B2 통과를 전체 부채 해소로 읽지 않는다.

## 충돌 시 기본 방향

"실환경 우선"이 스펙이다.

- **미등재 모킹** → 카테고리에 해당하면 등재 + 주석, 아니면 실 구현체 픽스처(MinIO·OpenSearch가
  선례다) · kind 실 워크로드 · http-trace를 통한 관측 · 게이트를 켠 별도 배포 변형으로 대체해
  모킹을 **제거**한다.
- **고아 등재** → 허용목록에서 지운다(상한도 같이 내린다).
- **차단 요인** → 해소 계획이 비었으면 채우고, 착수 가능하면 **이 모델의 task 가 직접**
  착수한다. 소관을 다른 모델·사람에게 넘기는 것으로 닫지 않는다(B4).
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
| R7 | 허용목록 표와 상한 마커 **밖**의 산문이 등재 수·상한을 숫자로 말하지 않는다(개수를 말하는 자리는 그 두 곳뿐이라는 위 보증의 전제를 집행한다). |
| B2 | 차단 원장 마커·표·ID·출처·해소 방향·선행·재검토가 위 형식을 만족하고 **소관 칸이 없다**(B4). 날짜 재검토는 등록일부터 90일 이내다. 행 수는 R6 상한에 포함하지 않는다. |

체커 회귀 검증: `python3 -m unittest discover -s scripts -p test_check_mock_policy.py`.
준비 단계와 머지 전·후 검증에서 별도로 실행하며, 완전한 대기 행과 빈 원장, 잘못된 마커·셀·날짜·사건을 대조한다.
CI의 기존 `E2E mocking policy` 단계는 위 체커로 실제 원장의 B2를 집행한다.
회귀 스위트의 CI 자동 실행 배선은 별도 워크플로 변경 범위다.
