# PRD: opensearch_search

OpenSearch REST 호환 백엔드(운영: docstore — `dlddu/docstore`, EFS 파일 기반 · OpenSearch REST 부분집합. OpenSearch 자체로도 교체 가능)에서 문서를 전문(full-text) 검색하는 도구.

## 달성 가치
- **V4: 운영 지식의 축적·검색** — 축적된 운영 문서를 자연어 질의로 찾아 재사용한다.
- **V2: 단명·최소권한 자격증명** — 백엔드가 SigV4를 요구하면(AWS 관리형) 정적 키 없이 AssumeRole
  단명 자격증명으로 서명한다(선택 모드).
- **V3: 안전한 운영(Safe-by-default)** — read-only 도구이며, 결과 상한과 미설정 시 graceful
  거부가 기본값이다.

## 도구 개요
- 입력: `query`(필수, 검색어), `index`(선택, 기본 전체 인덱스), `size`(선택, 기본 10, 최대 50)
- 동작: 백엔드에 `simple_query_string` 검색 요청(OpenSearch `_search` 형식)을
  보낸다. 매칭 문서를 index·id·score·본문(`_source`)과 함께 반환한다.
- 서버 요구 설정: `OPENSEARCH_ENDPOINT` (백엔드 기준 URL. 운영 값은 flux-cd-apps의
  ConfigMap `homelab-k3s-mcp-opensearch`가 준다.) 인증은 설정으로 고른다 — 없음(기본) ·
  basic(`OPENSEARCH_USERNAME`+`OPENSEARCH_PASSWORD`) · SigV4(`OPENSEARCH_ROLE_ARN`,
  `OPENSEARCH_SIGNING_SERVICE`=`es`|`aoss`, 선택 `OPENSEARCH_REGION`). 인증 값은 선택 Secret
  `homelab-k3s-mcp-opensearch-auth`로 넣는다.
- 어노테이션: `readOnlyHint=true`, `idempotentHint=true`, `openWorldHint=true`

## Acceptance Criteria

### AC1: 질의 검색
- **설명**: `query`와 매칭되는 문서를 index·id·score·본문(`_source`)과 함께 반환한다.
  `index` 지정 시 해당 인덱스로 검색 범위를 한정하고, 미지정 시 컬렉션 전체 인덱스를
  대상으로 한다.
- **달성 가치**: V4
- **검증 방법**: 시드된 문서 중 질의어와 매칭되는 문서만 반환되고, 각 결과에 index·id·
  score·본문이 포함된다.

### AC2: 결과 상한
- **설명**: `size` 기본값은 10, 상한은 50이다. 상한을 초과하는 요청은 클램프하지 않고
  도구 에러로 거부한다.
- **달성 가치**: V3
- **검증 방법**: size 미지정 시 최대 10건이 반환되고, size=51 요청은 거부된다.

### AC3: 백엔드 접근(OpenSearch REST 호환 · 인증 모드 설정)
- **설명**: 백엔드는 `OPENSEARCH_ENDPOINT`가 가리키는 OpenSearch REST 호환 서버이며(운영:
  docstore), 도구는 백엔드 종류를 모른다 — `_search`(simple_query_string)·`_doc` 와이어 형식만
  쓴다. 인증은 설정으로 고르며 코드 변경 없이 바꿀 수 있다: 없음(기본 — 클러스터 내부
  docstore 또는 security off OpenSearch) · basic · SigV4(STS AssumeRole 단명 자격증명, service
  `es`|`aoss`, 정적 AWS 키 미사용). 모드가 모호한 설정(basic과 SigV4 동시, basic 한쪽만,
  알 수 없는 서명 서비스)은 시작 시 unavailable로 거부한다. 운영 엔드포인트는 Traefik을 거치는
  이름(`docstore-external.docstore.svc.cluster.local`)이라 scale-to-zero된 docstore를 Sablier가
  깨우며, 대기 응답(307 같은 URL)은 본문째 재시도된다.
- **달성 가치**: V4 (SigV4 모드는 V2)
- **검증 방법**: 기본 모드에서 요청이 백엔드에 `Authorization`·`X-Amz-Security-Token` 없이
  도달한다. basic 모드는 Basic 헤더를, SigV4 모드는 지정 서비스 스코프의 AssumeRole 자격증명
  서명을 싣는다. 모호한 설정은 거부된다.

### AC4: 미설정 시 graceful 거부
- **설명**: 필수 서버 설정이 없으면 unavailable 류 에러를 반환하며, 서버 기동·다른 도구에는
  영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
