# PRD: platform — 인증·안전 기반 (공통)

특정 도구에 속하지 않고 **모든 도구에 공통으로 적용**되는 인증·권한 경계·런타임 안전에 대한
횡단 요구사항. (도구별 PRD와 달리, 이 문서는 도구들이 안전하게 동작하기 위한 공통 토대를
정의한다.)

## 달성 가치
- **V3: 안전한 운영(Safe-by-default)** — 무인증 접근·과도한 권한·통합 미설정으로 인한 다운을
  기본적으로 차단하고, 가능한 피해 범위를 구조적으로 제한한다.

## 범위
- **포함**: OAuth Bearer 인증 게이트, 정적 API 키 인증(비대화형 자동화용), GitHub Actions OIDC
  인증과 그 호출자의 도구 범위 제한, 인증 메타데이터 디스커버리, 하드닝된 런타임, 서버 수준
  graceful degradation, 헬스/레디니스.
- **제외**: 사용자·토큰 발급(외부 IdP 책임), 네트워크 인그레스 정책(클러스터 책임),
  도구별 동작(각 도구 PRD 참조), **클러스터 RBAC**.

> **AC3(최소권한 권한 경계)은 결번이다.** `k8s/rbac.yaml`이 `cluster-admin` 바인딩으로
> 바뀌면서 이 PRD가 권한 경계에 대해 주장할 것이 없어졌다. 뒤 번호는 당기지 않는다 —
> AC4~AC8은 같은 번호의 테스트 시나리오(와 그 번호를 딴 e2e 파일명 `platform_auth_safety_sc{5,6,7,8}.py`)와
> `comment-policy/ledger.md`의 과거 판정 행에 그 번호로 박혀 있고, 당기면 그 기록들이 소급해서 거짓이 된다.

## Acceptance Criteria

### AC1: 인증 게이트
- **설명**: `MCP_AUTH_DISABLED`가 설정되지 않은 한, `/mcp`는 유효한 자격증명 없이는 요청을
  거부한다. 유효한 자격증명은 **JWKS로 서명 검증되는 Bearer JWT**(대화형, AC2),
  **설정된 정적 API 키**(비대화형, AC7), **허용된 레포의 GitHub Actions OIDC 토큰**(AC9) 중 하나다.
- **달성 가치**: V3
- **검증 방법**: 자격증명 없음/무효 요청은 401로 거부되고(OAuth 활성 시 `WWW-Authenticate`가
  보호 리소스 메타데이터를 가리킴), 유효한 JWT 또는 유효한 API 키 요청은 정상 처리된다.

### AC2: 인증 디스커버리
- **설명**: `/.well-known/oauth-protected-resource`가 발급자/리소스를 광고하고, OIDC
  discovery(`/.well-known/openid-configuration`)로 JWKS를 동적 로드한다.
- **달성 가치**: V3
- **검증 방법**: 보호 리소스 메타데이터가 발급자/리소스를 반환하고, 표준 MCP 클라이언트가 이를
  통해 인증을 자동 구성할 수 있다.

### AC4: 하드닝된 런타임
- **설명**: 컨테이너는 nonroot 사용자, `readOnlyRootFilesystem`, 모든 capability 드롭,
  `seccompProfile: RuntimeDefault`로 구동된다(distroless nonroot 이미지, 정적 바이너리).
- **달성 가치**: V3
- **검증 방법**: 배포 매니페스트의 securityContext가 위 설정을 포함하고 컨테이너가 비특권으로
  정상 기동한다.

### AC5: 서버 수준 graceful degradation
- **설명**: 통합(GitHub/AWS/Grafana/k8s) 일부가 미설정이어도 서버는 정상 기동하고
  `tools/list`도 정상 응답한다. (도구별 unavailable 동작은 각 도구 PRD에 정의)
- **달성 가치**: V3
- **검증 방법**: 자격증명 env를 비운 채 기동해도 서버가 떠 있고 `tools/list`가 정상 응답한다.

### AC6: 헬스·레디니스
- **설명**: `/healthz`(liveness)·`/readyz`(readiness)와 startup 프로브를 제공해 오케스트레이터가
  서버 상태를 올바르게 판단하게 한다.
- **달성 가치**: V3
- **검증 방법**: 각 프로브 경로가 정상/비정상 상태를 올바르게 반영한다.

### AC7: API 키 인증 (비대화형)
- **설명**: `MCP_API_KEYS`(콤마 구분 목록)가 설정되면, `/mcp`는 목록의 키 중 하나를
  `Authorization: Bearer <key>`로 제시한 요청을 통과시킨다. 키 비교는 **상수시간**
  (`crypto/subtle.ConstantTimeCompare`)으로 수행해 타이밍 공격을 차단한다. 미들웨어는 제시된
  Bearer 값을 **먼저 API 키 목록과 대조**하고, 일치하지 않으면 기존 **JWT 검증으로 폴백**한다
  (AC1). 어느 쪽도 통과하지 못하면 401. 이 방식으로 CI·스크립트 등 자동화 클라이언트가
  대화형 OAuth 흐름 없이 인증할 수 있고, 인증을 완전히 끄는 `MCP_AUTH_DISABLED`에 의존하지
  않아도 된다.
- **달성 가치**: V3
- **검증 방법**: (a) 유효 키 요청 통과, (b) 무효/부재 키 401, (c) JWT와 동시 구성 시 유효 키와
  유효 JWT 요청이 **모두** 통과, (d) 키 값이 로그·에러 응답·`WWW-Authenticate`에 노출되지 않음
  (인증 방식만 로깅).

### AC8: 인증 방식 구성 유연성
- **설명**: 인증 활성(`MCP_AUTH_DISABLED` 미설정) 상태에서 OAuth(`MCP_OAUTH_*`)와 API 키
  (`MCP_API_KEYS`), GitHub Actions OIDC(`MCP_GITHUB_OIDC_*`, AC9)는 **각각 독립적으로**
  활성화되며 **최소 하나는** 설정되어야 한다. API 키만
  설정된 자동화 전용 배포에서는 IdP/OAuth 설정 없이 인증이 활성화된다. OAuth가 미설정이면
  광고할 발급자가 없으므로 `/.well-known/oauth-protected-resource` 디스커버리 엔드포인트는
  제공되지 않는다.
- **달성 가치**: V3
- **검증 방법**: (a) API 키만 설정 → 인증 활성 + 디스커버리 엔드포인트 미제공, (b) OAuth만 설정
  → 기존 동작(디스커버리 제공), (c) 둘 다 설정 → 둘 다 동작, (d) 둘 다 미설정이면서
  `MCP_AUTH_DISABLED`도 미설정 → **기동 실패**(무방비 노출을 안전하게 차단).

### AC9: GitHub Actions OIDC 인증 (워크플로)
- **설명**: `MCP_GITHUB_OIDC_AUDIENCE`와 `MCP_GITHUB_OIDC_REPOSITORIES`가 설정되면, `/mcp`는
  GitHub Actions 워크플로가 발급받은 OIDC 토큰을 `Authorization: Bearer <token>`으로 받는다.
  토큰은 발급자(`MCP_GITHUB_OIDC_ISSUER`, 기본 `https://token.actions.githubusercontent.com`)의
  OIDC discovery로 얻은 JWKS로 **RS256 서명**을 검증하고, `iss`·`aud`(= `MCP_GITHUB_OIDC_AUDIENCE`)·
  `exp`(필수)를 확인한 뒤, `repository` 클레임(`owner/name`, 대소문자 무시)이
  `MCP_GITHUB_OIDC_REPOSITORIES`에 **있을 때만** 통과시킨다. 미들웨어는 API 키 대조(AC7) 다음에,
  토큰의 `iss`가 GitHub 발급자이면 이 검증으로, 아니면 OAuth JWT 검증(AC1)으로 보낸다 — `iss`는
  검증기를 고르는 데만 쓰고 통과 판정은 서명 검증이 한다. 어느 것도 통과하지 못하면 401
  `invalid_token`. 호출 주체는 `github_oidc:<owner/name>`으로 기록된다(prd-event-log AC1).
  OAuth 발급자와 GitHub 발급자가 같으면 토큰을 구분할 수 없으므로 기동을 거부한다. 이 경로는
  디스커버리(AC2)로 광고하지 않는다 — 워크플로는 대상 서버를 미리 알고 토큰을 요청한다.
- **달성 가치**: V3
- **검증 방법**: (a) 허용 레포의 유효 토큰 통과, (b) 허용 목록 밖 레포·`repository` 클레임 없음·
  다른 audience·만료·`exp` 없음·다른 키 서명은 401 `invalid_token`, (c) OAuth와 함께 구성해도
  OAuth JWT는 OAuth 경로로 검증되고 도구 범위가 걸리지 않음, (d) audience·레포 목록 중 하나라도
  비면 기동 실패, (e) 응답·로그에 토큰 값이 없음.

### AC10: GitHub Actions 호출자의 도구 범위 제한
- **설명**: `MCP_GITHUB_OIDC_REPOSITORIES`는 레포마다 그 레포의 워크플로가 쓸 수 있는 도구 목록을
  준다(JSON 객체 `{"owner/name": ["도구", ...]}`). AC9로 인증된 요청은 그 목록 안에서만
  동작한다 — `tools/list`는 목록의 도구만 **원래 광고 순서대로** 돌려주고, 목록 밖 도구의
  `tools/call`은 존재하지 않는 도구와 **같은** JSON-RPC 오류(`-32602 unknown tool: <name>`)로
  거부되어 범위 밖 도구의 존재가 드러나지 않는다. 그 거부는 `refused` · `invalid_input`으로
  기록된다(prd-event-log AC2). 범위 안 도구는 다른 호출자와 똑같이 동작하며 승인 게이트
  (prd-approval-gate)도 그대로 적용된다. API 키·OAuth 호출자에게는 범위가 걸리지 않는다.
  목록이 이 서버에 없는 도구 이름을 담거나, 도구가 하나도 없는 레포를 담으면 기동을 거부한다
  (오타가 조용히 「아무것도 허용하지 않음」이 되지 않게).
- **달성 가치**: V3
- **검증 방법**: (a) 범위가 `[A, B]`인 레포의 `tools/list`가 정확히 A·B만 광고 순서대로 반환,
  (b) 범위 밖 도구 호출과 존재하지 않는 도구 호출의 오류 코드·메시지 형태가 같음, (c) 범위 안
  도구 호출은 정상 처리, (d) 거부 레코드의 주체가 `github_oidc:<owner/name>`, (e) 미지의 도구
  이름이 든 구성은 기동 실패.

## 비기능 요구사항
- `MCP_AUTH_DISABLED`는 신뢰된 네트워크의 로컬 개발/테스트 용도로만 사용하며 운영 배포에서는
  인증 활성을 기본으로 한다. API 키 도입 이후 자동화 접근은 `MCP_AUTH_DISABLED` 대신 API 키를
  사용하는 것이 원칙이다(인증을 끄지 않고도 비대화형 접근이 가능하므로).
- API 키는 **장수 공유 비밀**이다 — 비대화형 인증을 위한 의도적 트레이드오프이며, V2(서버가
  발급하는 단명 다운스트림 자격증명)와는 성격이 다르다. 따라서 (a) k8s Secret으로만 주입하고,
  (b) TLS 경유로만 전송하며, (c) 주기적으로 로테이션하고, (d) 개별 키는 `MCP_API_KEYS`에서
  제거해 폐기한다. 복수 키 지원으로 클라이언트별 발급·무중단 로테이션이 가능하다.
- 키 값은 로그·에러 응답·디스커버리 메타데이터 어디에도 출력하지 않는다. 접근 로그에는 인증
  방식(`api_key`/`jwt`)만 남겨 가시성과 비밀 보호를 양립시킨다.
- 비프로브 HTTP 요청은 메서드·경로·상태·소요시간이 로깅되어 최소한의 접근 가시성을 남긴다.
- GitHub Actions OIDC 토큰은 **워크플로 실행마다 발급되는 단명 토큰**이라 API 키와 달리 저장·
  로테이션할 비밀이 없다. 대신 신뢰의 단위는 **레포**다 — 그 레포에 워크플로를 푸시할 수 있는
  사람은 — 어느 브랜치에서든 `id-token: write`로 도는 워크플로를 통해 — 그 레포에 허용된 도구를
  쓸 수 있다. 브랜치·
  environment 단위로 좁히는 것은 이 AC의 범위 밖이므로, 레포에 주는 도구는 그 레포의 쓰기
  권한자 전원에게 주는 것으로 보고 최소한으로 고른다.
- 발급자의 discovery·JWKS는 기동 시 한 번 읽고(실패하면 기동 실패 — AC2와 같은 성질), 처음 보는
  `kid`가 오면 다시 읽는다.
