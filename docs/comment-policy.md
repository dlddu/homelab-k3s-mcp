# 주석 비중복성 정책

이 레포의 주석은 **코드·저장소 문서·PR·커밋 메시지 어느 쪽으로도 복원되지 않는 지식**만
담는다. 읽는 사람이 다른 곳에서 확인할 수 있는 내용을 주석이 되풀이하면 그것은 중복이다.

되풀이된 주석은 값이 없을 뿐 아니라 **위험하다**. 원본(코드·PRD·테스트 문서)만 고쳐지고
주석은 남으면, 주석은 조용히 거짓이 된다. 주석은 컴파일되지도 테스트되지도 않으므로
아무도 그 거짓을 알려주지 않는다.

이 문서가 **정책의 SSOT**다. 무엇을 남기고 무엇을 지우는지, 그리고 한 주석을 놓고 어떻게
판정하는지를 여기서 정한다. 정합 상태는 **범위 안의 주석 중 복원 가능한 내용을 담은 것이
없음**이다.

> ⚠️ **CI 게이트는 「이 주석이 중복인가」를 판정하지 않는다.** 그 판정은 아래 판정 절차대로
> 사람이 한다. `scripts/check_comment_policy.py`가 보는 것은 **판정 이력 원장의 무결성**이다 —
> 등재된 범위의 주석이 판정 이후 바뀌지 않았는지, 합계가 행들과 맞는지. 바뀌었다면 그 범위는
> 재판정 대상이고 게이트가 그것을 막아 세운다. `check_mock_policy.py`·`check_ac_mapping.py`는
> 주석을 *파싱해 다른 것을* 검사할 뿐이다.

## 복원 경로는 넷이다

한 주석이 지워도 되는지는 **그 내용을 다른 데서 확인할 수 있는가**로만 결정한다. 확인처는
넷이고, 이 중 **어디서든** 확인되면 주석의 자리가 아니다.

| # | 복원 경로 | 이 레포에서의 실체 |
| --- | --- | --- |
| ① | **코드 자체** | 시그니처·타입·상수·바로 아래 줄. 인터페이스가 이미 문서화한 계약을 구현체가 되풀이하는 것도 여기에 해당한다. |
| ② | **저장소 문서** | `docs/prd-*.md`(도구 개요·AC), `docs/test-*.md`(테스트 시나리오), `docs/e2e-mocking-policy.md`(모킹 허용목록), `docs/doc-tracker.md`(문서↔구현 원장), `README.md` |
| ③ | **PR 제목·본문·리뷰 코멘트** | 왜 이렇게 바꿨는지, 무엇을 검토했는지 |
| ④ | **커밋 메시지** | 어떤 결정이 언제 뒤집혔는지 |

③·④는 `git log`·`gh pr view`로 항상 도달할 수 있다. "나중에 찾기 번거로우니 주석으로
옮겨 둔다"는 이 정책이 인정하지 않는 사유다.

## 유지 대상

아래 셋은 중복 판정의 대상이 아니다. 나머지는 모두 판정 대상이다.

### 1. 기계가 읽는 주석 — 정책 대상이 아니다

지우면 CI가 깨지거나 툴체인이 동작을 바꾼다. **판정에서 제외하고, 정합성 모델의 as-is
지문에서도 제외한다.**

| 토큰 | 누가 읽는가 |
| --- | --- |
| `검증 AC:` | `tests/integration/run_all.py`가 모듈 docstring에서 파싱하고, `tests/integration/check_ac_mapping.py`가 AC↔E2E 1:1을 검사한다 |
| `# mock-exception:` | `scripts/check_mock_policy.py`가 `docs/e2e-mocking-policy.md`의 허용목록과 대조한다 |
| `#!` (shebang) | 인터프리터. 대부분 파일 첫 줄이고, `tests/k8s/kind/dear-baby-fixture.yaml`에는 임베드된 스크립트의 것이 하나 있다 |
| `# noqa` | 린터. `tests/k8s/kind/http-trace.yaml`의 임베드 Python에 붙어 있다 |
| `//go:` · `nolint` | Go 툴체인. 지금은 쓰이지 않지만 생기는 즉시 제외 대상이다 |

건수를 적지 않는 이유는 아래 **범위** 절과 같다 — 파일 하나가 늘면 낡는 수치이고, 최신값이
필요하면 게이트가 출력한다.

`검증 AC:`는 **Python 모듈 docstring 안**에 있다 — `#` 주석이 아니다. 그 docstring은
기계 판독 자리이므로 **통째로 유지**하고, 그 안에서 `docs/prd-*.md`·`docs/test-*.md`를
되풀이하는 산문만 판정 대상이다.

### 2. doc 주석의 최소치 — 관례가 요구하는 만큼

- **Go**: exported 식별자는 **식별자 이름으로 시작하는 1줄** doc 주석을 유지한다. 패키지
  주석도 유지한다. `// Error is the error type returned by Service.`처럼 선언을 그대로
  옮겨 적기만 하는 1줄이라도 **지우지 않는다** — 그 1줄은 Go 관례가 요구하는 자리이고,
  `go doc`의 출력이며, 이 정책이 없애려는 것은 관례가 아니라 *본문의 중복*이다.
- **Python**: 모듈 docstring을 유지한다(위 1번과 겹친다).
- **관례를 채우는 최소 doc과 그 아래 이어지는 본문은 다르다.** 판정 대상은 **본문**이다 —
  1줄 뒤에 이어지는 문단이 시그니처나 `docs/prd-*.md`를 되풀이하면 그 문단이 제거 대상이다.

### 3. 복원 불가능한 지식 — 이 정책이 지키려는 것

- **상류 API의 문서화되지 않은 동작** — GitHub App·Grafana Cloud·OpenSearch·AWS STS·
  k8s API가 문서에 적어 두지 않은 실제 거동
- **수치의 근거** — 왜 35초인지, 왜 512Mi인지. 값 자체는 코드에 있지만 *근거*는 어디에도
  없다
- **실패 모드의 함정** — 무엇이 조용히 통과하는가, 무엇이 플레이키했고 왜 그 형태를
  피했는가
- **관측된 사실** — 날짜·환경이 붙은 것(`observed on-cluster 2026-07-23` 류)
- **픽스처가 실환경과 갈리는 지점** — kind 하네스가 프로덕션과 다른 곳과 그 이유
- **구조적 보장의 이유** — "여기서 거부하면 요청이 아예 나가지 않는다" 같은, 코드 형태만
  봐서는 *왜 그 자리인지* 보이지 않는 것

## 제거 대상 — 이 레포에서 실제로 관측된 세 유형

### ① 선언 재진술

시그니처·타입이 이미 말한 것을 산문으로 옮긴 것.

```go
// PutResult is the outcome of a PutDocument call. Result is the OpenSearch
// index result: "created" or "updated".          <- 2번째 문장은 PRD가 이미 말한다
```

1줄 doc은 남기고 **본문만** 지운다.

### ② 문서·AC 재진술

`docs/prd-*.md`·`docs/test-*.md`가 이미 말한 것을 주석이 되풀이하는 것. 테스트 파일 상단의
AC 목록, 테스트 이름을 산문으로 옮긴 doc 주석이 여기 해당한다.

```go
// MaxSearchSize is the hard cap on the search result size. Requests above
// it are rejected, not clamped.        <- prd-opensearch-search.md AC2 그대로
```

다만 **"이 어서션이 왜 중요한가"는 재진술이 아니다.** AC 번호를 옮겨 적은 것과, 그 AC가
왜 이 형태로만 관측 가능한지를 적은 것은 다르다. 후자는 유지 대상 3번이다.

### ③ 자기 파일 안의 중복

같은 파일에서 이미 말한 것을 사용 지점에서 다시 말하는 것. 패키지 주석과 함수 안 주석,
인터페이스 메서드 doc과 구현체 doc 사이에서 자주 생긴다.

```go
// Package opensearch ... The base credentials come from the default AWS
// credential chain (the instance profile in production); ...
...
    // Base credentials: the default chain (instance profile in production).
    baseCfg, err := sdkconfig.LoadDefaultConfig(ctx, loadOpts...)
```

## 판정 절차

한 주석을 놓고 순서대로 묻는다. 먼저 걸리는 곳에서 멈춘다.

1. **기계가 읽는가?** → 유지(대상 아님).
2. **관례가 요구하는 최소 doc(Go exported 1줄·패키지 주석·Python 모듈 docstring)인가?** → 유지.
3. **복원 경로 ①~④ 중 어디서든 확인되는가?**
   - 확인된다 → **지운다.**
   - 확인되지 않는다 → 유지.
4. **"원본이 부실해서" 확인되지 않는 것인가?** → 주석을 남기지 말고 **원본을 고친다**.
   PRD가 빠뜨렸으면 PRD에 쓰고, 이름이 나쁘면 이름을 고치고, 결정의 이유면 커밋 메시지·PR에
   남긴다. 그러고 나서 주석을 지운다.
5. **3에서 판단이 갈리는가?** → **남긴다.** 아래.

### 애매하면 남긴다

이 정책은 제거 쪽으로 기울지 않는다. 비용이 비대칭이기 때문이다.

- 중복 주석을 남겨 두는 비용: 몇 줄. 언젠가 낡아 거짓이 될 위험.
- 유일한 지식을 담은 주석을 지우는 비용: **그 지식이 어디에도 남지 않는다.** 복구 불가.

판단이 갈린 주석은 보존하고, 갈렸다는 사실을 정합성 task에 남긴다. 다음 판정의 입력이 된다.

### 충돌하면 주석이 틀린 것으로 본다

주석이 코드·문서·이력과 어긋나면 **주석을 틀린 쪽으로 놓는다** — 나머지 셋은 검증되지만
주석은 아무도 검증하지 않기 때문이다. 어긋난 주석은 제거 근거가 오히려 강해진다.

## 범위

**대상**: `main.go` · `internal/` · `tests/` · `scripts/`.

**범위 밖**: 라이선스 헤더, 생성 코드, `docs/`의 마크다운, `k8s/`의 운영 매니페스트,
`.github/workflows`.

> **현재 수치는 이 문서에 적지 않는다.** 판정 대상 주석이 지금 몇 줄인지, 언어별로 어떻게
> 갈리는지는 `python3 scripts/check_comment_policy.py`가 매 CI에서 출력한다. 한때 이 자리에
> 「2026-09-04 기준 848줄 / 64파일」이라 적혀 있었는데, 그 값은 **이 문서가 머지되기도 전에**
> 형제 PR 하나로 거짓이 됐다(848 → 963). 아무도 검증하지 않는 자리에 현재형 수치를 두면 그
> 수치는 낡는다 — 이 정책이 주석에 대해 말하는 것과 정확히 같은 이유다. 낡을 수 있는 수치는
> 지우고, 기계가 강제하는 수치(아래 판정 이력의 줄 수·지문)만 남긴다.

이 정책이 보지 않는 것:

- **문서·커밋 메시지 자체의 품질.** 다만 "주석을 지우려면 원본을 고쳐야 한다"는 판정이
  그 원본을 고치는 작업을 낳을 수 있고, 그건 정상이다(판정 절차 4번).
- **주석의 정확성**(코드와 맞는가). 이 정책은 중복만 본다 — 다만 되풀이된 주석이 낡아
  틀려 있으면 그것은 제거 근거를 강화한다.

## 추적

정합성 모델 `tbm_homelab-k3s-mcp-comment-redundancy`가 이 축을 추적한다.

- **to-be** = 이 문서의 blob hash. 정책이 바뀔 때만 변한다.
- **as-is** = 범위 안에서 주석 줄만 추출·정규화·정렬한 sha256 지문. 트리 해시가 아니므로
  주석과 무관한 코드 변경에는 반응하지 않는다(자매 모델 `tbm_homelab-k3s-mcp-docs-impl`과의
  중복 트리거 회피).
- **게이트** = `scripts/check_comment_policy.py`(CI `fmt + vet` 잡). 아래 판정 이력의 범위별
  줄 수·지문을 **as-is와 글자 그대로 같은 추출·정규화**로 재측정해 대조한다. 추출 정의가 모델과
  갈리면 게이트가 재는 것과 감지가 보는 것이 달라지므로, 고칠 때는 모델 정의와 함께 고칠 것.

### 지문의 사각지대

as-is 지문은 `^[[:space:]]*(//|#)` 에 걸리는 줄만 본다. 따라서 아래는 **판정 대상이지만
지문에는 보이지 않는다** — 이 정책은 적용되지만, 늘어나도 재감지가 트리거되지 않는다.

- Python docstring 본문 (줄 시작이 `#`가 아니다)
- Go 블록 주석 `/* */` (현재 0건)
- 줄 끝 주석 (`x := 1 // 이유`)

패턴을 넓히는 것은 이런 주석이 늘어날 때 별도 task에서 다룬다.

## 판정 이력

정책이 실제로 적용된 범위를 여기에 누적한다. 등재되지 않은 범위는 **아직 판정받은 적이
없다**는 뜻이다.

각 행은 범위와 함께 **그 범위 주석의 줄 수와 지문**을 담는다(지문 = as-is와 같은 방식으로
추출·정규화·정렬한 `경로:주석` 목록 sha256의 앞 12자리). `scripts/check_comment_policy.py`가
매 CI에서 재측정해 대조하므로 **등재된 범위의 주석을 바꾼 PR은 같은 PR에서 다시 판정하고 그
행을 갱신해야 한다.** 범위를 파일 단위로 적는 것도 그래서다 — 한때 이 표는 범위를
`internal/sessionplatform/`처럼 **패키지 이름으로** 적었고, 그 뒤 그 패키지에 들어온 주석 96줄이
「판정 완료」로 위장된 채 남았다. 등재는 그 범위를 **그때 그 내용으로** 판정했다는 뜻이지,
그 이름 아래 앞으로 올 것까지 판정했다는 뜻이 아니다.

<!-- 판정-원장 -->

| 판정일 | 범위 | 주석 줄 | 지문 | 결과 |
| --- | --- | ---: | --- | --- |
| 2026-09-04 | `internal/opensearch/opensearch.go` · `internal/opensearch/opensearch_test.go` | 52 | `d9c561b69bcc` | 제거 7줄 — ① 선언 재진술 ② `docs/prd-opensearch-search.md` AC2 재진술 ③ 패키지 주석과의 자기 중복. 판정 근거는 `rct_20260904-0001` |
| 2026-09-04 | `internal/sessionplatform/sessionplatform.go` · `internal/sessionplatform/sessionplatform_test.go` | 172 | `3bdaa24398db` | 1차 판정(`rct_20260904-0001`, 제거 1줄) 뒤 `session_read`·`session_write` 구현이 들여온 **96줄을 재판정**(`rct_20260904-0004`). 제거 18줄은 전부 ③ 자기 파일 중복 — `ReadSession`·`WriteSession`의 doc 본문이 같은 파일의 `errKind` 상수 doc과 `WriteResult` doc을 되풀이했다. 제거 블록에서 유일하게 복원되지 않던 한 절은 `kindTooLarge` doc으로 접었다(+1줄). 테스트 doc 주석 51줄은 「이 어서션이 왜 이 형태인가」라 전부 유지 |
| 2026-09-04 | `internal/auth/auth.go` · `internal/auth/auth_test.go` | 41 | `347cb1098477` | 제거 37줄 — ② `README.md` 「Authentication」 절이 그대로 담은 서술(`FromEnv` doc 13줄: 두 자격증명 경로·env 이름·「둘 다 없으면 기동 거부」, `OAuthConfigured`·`APIKeyCount`·`RequireBearer` 의 본문)과 ② `docs/prd-platform-auth-safety.md`·`docs/test-platform-auth-safety.md` 재진술(테스트 상단 AC 목록 10줄 — AC8 을 「OAuth 선택화」라 적어 PRD 의 「인증 방식 구성 유연성」과 이미 갈려 있었다, 시나리오 7 doc 3줄). `matchAPIKey` 의 「early return 을 두지 않는 이유」와 `verify` 의 「API 키 전용 모드에서 nil JWKS 를 건드리기 전에 거부한다」는 복원 경로가 없어 유지. 판정 근거는 `rct_20260904-0006` |
| 2026-09-04 | `internal/server/server.go` · `internal/server/mcp_test.go` · `internal/server/auth_routing_test.go` · `internal/server/health_test.go` · `internal/server/fakes_test.go` | 62 | `02f4aee7322d` | 제거 3줄 — ① `App` doc 본문이 바로 아래 라우팅 분기(`authCfg == nil`, `OAuthConfigured()`)와 README 를 되풀이. `mcp_test.go` 52줄은 전부 「이 어서션이 왜 이 형태인가」(vacuous 통과 방지 · 커서와 증분의 구분 · 상류 wording 을 고정한 이유)라 유지. `silentPaths` 의 「kubelet 프로브가 로그를 덮는다」도 유지. `fakes_test.go` 는 주석 0줄 — 등재해 두면 이후 유입이 R2 에 걸린다 |
| 2026-09-04 | `internal/k8s/client.go` · `internal/k8s/service.go` · `internal/k8s/types.go` | 37 | `f99fe262f503` | 제거 4줄 — ③ `firstPodMatching` doc 이 같은 폴백 규칙을 바로 아래 `pickPod` doc 과 `Service.WorkloadLogs` 인터페이스 doc 에 이어 세 번째로 되풀이(2줄), ① `ParseWorkloadKind` 의 comma-ok 설명과 `Unavailable` 의 사용처 설명(각 1줄). 「Events 는 best-effort(RBAC 로 실패해도 나머지는 나간다)」·「정렬은 결정성 때문」·「crash loop 뒤에도 Previous 가 되도록 Running 이 아닌 파드로 폴백」은 유지 |
| 2026-09-04 | `internal/mcp/mcp.go` · `internal/mcp/describe.go` · `internal/mcp/toolslist.go` | 32 | `ff00be23a5b5` | 제거 0줄 — 세션 3종의 doc 은 「빈 목록과 도달 불가를 구분한다」·「스냅샷 세션은 pod 을 생략한다(빈 이름으로 있는 척하지 않는다)」·「쓰기는 도착만으로 대상을 깨우므로 인자 검증이 요청보다 앞선다」처럼 복원 경로 넷 어디에도 없는 구조적 이유다. 경계 사례는 `sessionFields` 의 「세 도구가 세션을 같은 모양으로 보고한다」 한 절 — 호출부로 복원되지만 *제약*의 선언이라 「애매하면 남긴다」로 유지 |
| 2026-09-04 | `internal/grafana/grafana.go` · `internal/grafana/grafana_test.go` · `internal/grafana/exposure_test.go` | 37 | `f48e36feba66` | 제거 0줄(문장 1개 삭제, 줄 바꿈에 흡수돼 줄 수 불변) — `exposure_test.go` 의 「This previously had no automated coverage.」는 ③④(PR·커밋)로 복원되므로 삭제. `defaultAPIBase` 의 「`GRAFANA_API_URL` 은 `.../api` 베이스여야 한다」, `tokenName` 의 「Grafana Cloud 가 정책 내 유일 이름을 요구한다」, 「발급자 토큰을 실제로 썼는지 먼저 단언하지 않으면 비노출 검사가 무의미해진다」는 전부 유지 |
| 2026-09-04 | `internal/awsconfig/awsconfig.go` · `internal/awsconfig/awsconfig_test.go` | 24 | `1b5e509876c7` | 제거 1줄 — ③ `LoadDefaultConfig` 위의 「Base credentials: the default chain (instance profile in production).」 이 패키지 주석의 같은 문장을 되풀이. 정책 §제거 대상 ③ 이 예시로 든 형태 그대로이고, 자매 파일 `internal/opensearch/opensearch.go` 가 1차 판정에서 같은 결론을 받았다. `AWS_CONFIG_S3_ENDPOINT`/MinIO 단락(STS 와 S3 를 한 포트에 얹는다, 스모크 전용)은 픽스처가 실환경과 갈리는 지점이라 유지 |
| 2026-09-04 | `internal/github/github.go` · `internal/github/github_test.go` | 19 | `3e94443eb4d1` | 제거 0줄(문장 1개 삭제, 줄 수 불변) — 「This previously had no automated coverage.」 삭제. 나머지는 exported 1줄 doc(정책이 유지 예시로 든 `// Error is the error type returned by Service.` 포함)과 「앱 JWT 를 되돌려주면 그 자체가 파생 서명 자료의 유출」이라 유지 |
| 2026-09-04 | `main.go` · `internal/version/version.go` | 4 | `28fb0cecbc29` | 제거 0줄 — 넷 다 Command·패키지 doc 과 exported 상수의 1줄 doc 이라 관례가 요구하는 최소치다 |
| 2026-09-04 | `scripts/check_comment_policy.py` · `scripts/check_mock_policy.py` | 22 | `3b56892dcb7f` | 제거 0줄 — `ID_TOKEN_RE` 의 「`(?![A-Za-z0-9_])` 가 없으면 정책 문서 경로 자체가 미등재 모킹으로 잡힌다」는 함정이고, 「모델 as-is 스크립트와 글자 그대로 같아야 한다」는 교차 제약이다. `# R1 —` 류 규칙 앵커는 모듈 docstring 의 재진술에 가깝지만 100줄짜리 `main()` 의 항해 표지라 「애매하면 남긴다」로 유지 |
| 2026-09-05 | `tests/k8s/kind/session-platform.yaml` | 75 | `20eaf535b30c` | 제거 6줄 — ③ 자기 파일 중복 둘. `data-plane` ServiceAccount 위 4줄은 머리말이 이미 말한 것(그 SA 가 없으면 세션 파드가 뜨지 않는다 · 프로덕션 ClusterRoleBinding 을 재현하지 않는 이유)의 되풀이이고, `DATA_PLANE_IMAGE` 위 2줄은 머리말의 「한 워크플로가 쌍을 발행하므로 같은 agent 계약을 쓴다」를 다시 적은 것이다. 머리말 나머지는 전부 유지 — 왜 stand-in 이 아니라 실 컴포넌트인가, `CRIU_ENABLED`·`DATA_PLANE_CLAUDE_CODE_IMAGE` 를 비워 두는 것이 게이트 완화가 아닌 이유, 268 MiB 를 테스트 전에 미리 로드하는 이유(제어면이 파드에 주는 2분 예산을 pull 에 쓰면 안 된다), SHA 태그가 불변이라 하네스가 재현 가능하다는 근거 |
| 2026-09-05 | `tests/k8s/kind/oidc-fixture.yaml` | 78 | `0cffc931b2c9` | 제거 1줄 — 「허용목록·상한은 이 파일 때문에 변하지 않는다 (등재 5 · 상한 5 그대로)」. 바로 앞 문단이 `UPS`·`IMG` 어느 카테고리도 성립하지 않는다고 이미 결론냈고(③), 괄호 안 현재값은 `check_mock_policy.py` R6 이 양방향으로 강제하는 수치라 아무도 검증하지 않는 자리에 두면 낡는다. dex 최소 설정의 근거(`server.NewServer` 가 커넥터 0 개를 거부한다), digest 핀, readiness 를 `/healthz` 가 아니라 discovery 로 둔 이유, (d) 변형의 CrashLoop 이 곧 AC2 논증을 정직하게 만든다는 절은 전부 복원 경로가 없어 유지 |
| 2026-09-05 | `tests/k8s/kind/http-trace.yaml` | 43 | `e6747e1eba38` | 제거 1줄 — `TRACE_ROUTES` 값 **바로 위**의 형식 주석(① 아래 줄이 형식을 그대로 보여준다). 같은 형식을 적은 임베드 스크립트 쪽 1줄은 파싱 지점의 앵커라 판단이 갈렸고 「애매하면 남긴다」로 보존했다. 프록시가 왜 있는가(픽스처는 assumed-role 요청과 base 자격증명 요청을 구분하지 못해 결과 기반 단정이 공허해진다), hop-by-hop·`Expect`·중복 `Content-Length`·Host 서명 투명성, AssumeRole 레코드는 축출하지 않는 이유는 전부 유지 |
| 2026-09-05 | `tests/k8s/kind/auth-fixture.yaml` · `tests/k8s/kind/test-deployment.yaml` | 38 | `8ef5b31642d6` | 제거 1줄(3줄 → 2줄) — `MCP_API_KEYS` 위의 「Auth ENABLED: `MCP_AUTH_DISABLED` 는 일부러 미설정」 두 문장이 파일 머리말의 같은 서술을 되풀이해(③) 잘라내고, `_auth_variant.py` 의 `API_KEY` 와 값이 같아야 한다는 교차 제약만 남겼다. 머리말의 「자격증명 시크릿을 여기 붙이지 말 것」은 계약이라 유지. `test-deployment.yaml` 은 제거 0줄 — 영구 crash loop 을 피한 이유(빠른 재시작이 containerd 의 이전 인스턴스 GC 와 겹쳐 E2E 가 플레이키했다)는 관측된 실패 모드다 |
| 2026-09-05 | `tests/k8s/kind/minio.yaml` · `tests/k8s/kind/opensearch.yaml` · `tests/k8s/kind/github-mock.yaml` · `tests/k8s/kind/grafana-mock.yaml` · `tests/k8s/kind/dear-baby-fixture.yaml` · `tests/k8s/kind/kustomization.yaml` | 17 | `4b9ee1aa233b` | 제거 20줄 — ② `docs/e2e-mocking-policy.md` 재진술 16줄 + `minio.yaml` 4줄. `github-mock`·`grafana-mock`·`dear-baby-fixture` 의 머리말은 그 문서의 **대상**·**실환경 불가 사유**·재배선 절을 그대로 옮겨 적은 것이고(dear-baby 의 「busybox 에 성공/미발견 종료코드를 흉내 내는 스텁」까지 문서에 있다), 세 파일 모두 그 문서를 가리키는 `등재:` 줄과 `mock-exception:` 주석을 이미 달고 있어 SSOT 로 가는 경로가 끊기지 않는다. `minio.yaml` 은 seed ConfigMap 머리말 3줄 — 아래 Job 스펙으로 복원되는 데다 **존재하지 않는 파일**(`tests/integration/aws_config.py`)을 가리켜 이미 거짓이었다(실제 단정은 `aws_config_get_ac1.py` 이고, 역방향 링크는 `_aws_config.py` 에 정확히 있다) — 과 Job 위 1줄(바로 아래 명령의 재진술). `opensearch.yaml` 의 heap 512Mi 근거와 GATE 등재 주석, `minio.yaml` 의 「S3 + STS 대역」 서술은 유지 |
| 2026-09-05 | `tests/integration/_helpers.py` · `tests/integration/_session_platform.py` · `tests/integration/_oidc.py` · `tests/integration/_workload.py` · `tests/integration/_auth_variant.py` · `tests/integration/_aws_config.py` · `tests/integration/_opensearch.py` | 130 | `64d8f19c008e` | 제거 0줄 — 전부 「왜 이 형태인가」다. `_helpers.py` 의 도구 표면 집합은 두 번(`session_list` 2026-09-03, `session_read` 2026-09-04) 빠뜨려 검사가 조용히 약해졌다는 관측 이력과 그래서 생긴 규칙을 달고 있고, 포트포워드 헬퍼는 apiserver service proxy 가 대안이 아닌 이유(Authorization 을 덮어쓰고 GET 밖에 못 낸다)를, http-trace 헬퍼는 결과 기반 단정이 공허한 이유를 담는다. `_session_platform.py` 의 `#:` 예산값들은 제어면이 파드 Ready 를 기본 2분 기다린 뒤에야 201 을 낸다는 근거다. `_oidc.py` 의 `#:` 상수 doc 은 구성 (b)·(c)·(d) 라벨을 픽스처와 잇는 자리라 판단이 갈렸다 — 라벨의 의미 자체는 `oidc-fixture.yaml` 코드로 복원되지만 그 매핑을 잃으면 상수 이름만 남으므로 「애매하면 남긴다」로 보존 |
| 2026-09-05 | `tests/integration/run_all.py` · `tests/integration/check_ac_mapping.py` | 11 | `49ac6aeb6984` | 제거 0줄 — `check_ac_mapping.py` 의 `# --- 규칙 N ---` 앵커는 자매 판정(위 #11 `check_comment_policy.py`)이 같은 형태를 「긴 `main()` 의 항해 표지」로 유지한 것과 같은 이유로 유지한다. `run_all.py` 의 `#:` 둘은 규칙 3(비-AC 파일)이 읽는 값의 의미를 적은 것이다 |
| 2026-09-05 | `tests/integration/session_list_ac1.py` · `tests/integration/session_list_ac2.py` · `tests/integration/session_list_ac3.py` · `tests/integration/session_read_ac1.py` · `tests/integration/session_read_ac3.py` · `tests/integration/session_read_ac4.py` · `tests/integration/session_write_ac1.py` · `tests/integration/session_write_ac3.py` · `tests/integration/session_write_ac5.py` | 46 | `dfc76f31359a` | 제거 2줄 — ① 어서션 재진술. `session_write_ac1.py` 의 「세션이 이미 active 였으므로 승격도 복원도 필요 없었다」와 `session_read_ac1.py` 의 「읽기는 수동적이지 않고 결과가 어느 분기가 처리했는지 말한다」는 각각 바로 아래 `path == "active"` 단정이 말하는 것이다. 반대로 마커를 두 조각으로 갈라 넣는 이유(PTY 가 명령을 되울리므로 붙여 쓰면 에코만으로 단정이 성립해 버린다)와 커서가 바이트 오프셋이어야 하는 이유는 이 파일들에만 있는 지식이라 유지. 주석 0줄인 `session_list_ac3.py`·`session_write_ac3.py` 도 같이 등재한다 |
| 2026-09-05 | `tests/integration/opensearch_document_delete_ac1.py` · `tests/integration/opensearch_document_delete_ac2.py` · `tests/integration/opensearch_document_delete_ac3.py` · `tests/integration/opensearch_document_delete_ac4.py` · `tests/integration/opensearch_document_delete_ac5.py` · `tests/integration/opensearch_document_put_ac1.py` · `tests/integration/opensearch_document_put_ac2.py` · `tests/integration/opensearch_document_put_ac3.py` · `tests/integration/opensearch_document_put_ac4.py` · `tests/integration/opensearch_document_put_ac5.py` · `tests/integration/opensearch_search_ac1.py` · `tests/integration/opensearch_search_ac2.py` · `tests/integration/opensearch_search_ac3.py` · `tests/integration/opensearch_search_ac4.py` | 4 | `31d67e48d0e5` | 제거 0줄 — 페이로드 해시까지 서명해야 하는 이유(서명을 깨지 않고 본문을 바꿔치기할 수 있다)와, 비매칭 문서가 색인에 실재하므로 그 부재가 「질의 필터링」이지 「문서 없음」이 아니라는 전제. 나머지 12파일은 주석 0줄이다 |
| 2026-09-05 | `tests/integration/aws_config_get_ac1.py` · `tests/integration/aws_config_get_ac2.py` · `tests/integration/aws_config_get_ac3.py` · `tests/integration/github_app_installation_token_ac1.py` · `tests/integration/github_app_installation_token_ac2.py` · `tests/integration/github_app_installation_token_ac3.py` · `tests/integration/github_app_installation_token_ac4.py` · `tests/integration/grafana_token_ac1.py` · `tests/integration/grafana_token_ac2.py` · `tests/integration/grafana_token_ac3.py` · `tests/integration/grafana_token_ac4.py` · `tests/integration/platform_auth_safety_ac1.py` · `tests/integration/platform_auth_safety_ac2.py` · `tests/integration/platform_auth_safety_ac3.py` · `tests/integration/platform_auth_safety_ac5.py` · `tests/integration/platform_auth_safety_ac6.py` · `tests/integration/platform_auth_safety_ac7.py` · `tests/integration/platform_auth_safety_ac8.py` | 38 | `5d8a74c68236` | 제거 3줄 — ① 어서션 재진술. `github_app_installation_token_ac1.py` 의 「만료 주석」·「범위 주석」 두 줄은 바로 아래 `in env_text` 단정이 리터럴로 말하고, `grafana_token_ac4.py` 의 「payload 전체가 text/plain .env 이고 구조화 내용은 없다」는 `structuredContent is None` 의 재진술이다. 같은 파일의 「발급 토큰은 반환된다 ↔ 발급자 자격증명은 어떤 형태로도 새면 안 된다」 대비쌍은 부정 단정이 공허해지지 않게 하는 장치라 유지하고, `platform_auth_safety_ac3.py` 의 「AC 문언을 옮겨 적은 ClusterRole 전체」는 등가 비교의 피연산자 자체라 재진술이 아니다 |
| 2026-09-05 | `tests/integration/.gitignore` · `tests/integration/dear_baby_reset_user_ac1.py` · `tests/integration/dear_baby_reset_user_ac2.py` · `tests/integration/dear_baby_reset_user_ac3.py` · `tests/integration/namespace_list_ac1.py` · `tests/integration/ping_ac1.py` · `tests/integration/pod_describe_ac1.py` · `tests/integration/pod_describe_ac2.py` · `tests/integration/pod_describe_ac3.py` · `tests/integration/requirements.txt` · `tests/integration/smoke.py` · `tests/integration/workload_list_ac1.py` · `tests/integration/workload_list_ac2.py` · `tests/integration/workload_logs_ac1.py` · `tests/integration/workload_logs_ac2.py` · `tests/integration/workload_logs_ac3.py` · `tests/integration/workload_logs_ac4.py` · `tests/integration/workload_restart_ac1.py` · `tests/integration/workload_restart_ac2.py` · `tests/integration/workload_scale_ac1.py` · `tests/integration/workload_scale_ac2.py` · `tests/integration/workload_scale_ac3.py` | 9 | `bae4b9d18d00` | 제거 0줄 — kubelet 이 `timestamps=true` 에 붙이는 접두 형태, 한 kind 의 요약이 다른 kind 의 모양을 갖지 않는다는 단정의 의도, 도구가 selector/container 미지정 시 적용하는 기본값의 출처. 나머지는 주석 0줄이며, **등재해 두면 이후 유입이 R2 에 걸린다**(위 #4 `fakes_test.go` 와 같은 이유) |
<!-- /판정-원장 -->

판정 완료 합계 **<!-- 판정-합계 -->991<!-- /판정-합계 -->줄**. 전체 대비 비율과 미판정 잔량은
게이트가 출력한다(프로즈에 적으면 낡는다).
