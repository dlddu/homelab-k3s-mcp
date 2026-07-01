# PRD: opensearch_search

OpenSearch Serverless 컬렉션(운영: `kubernetes-docs`)에서 문서를 전문(full-text) 검색하는 도구.

## 달성 가치
- **V4: 운영 지식의 축적·검색** — 축적된 운영 문서를 자연어 질의로 찾아 재사용한다.
- **V2: 단명·최소권한 자격증명** — 정적 키 없이 AssumeRole로 얻은 단명 자격증명으로 SigV4
  서명 접근한다.
- **V3: 안전한 운영(Safe-by-default)** — read-only 도구이며, 결과 상한과 미설정 시 graceful
  거부가 기본값이다.

## 도구 개요
- 입력: `query`(필수, 검색어), `index`(선택, 기본 전체 인덱스), `size`(선택, 기본 10, 최대 50)
- 동작: 기본 AWS 자격증명 체인(운영 환경의 인스턴스 프로파일)으로 `OPENSEARCH_ROLE_ARN`을
  STS AssumeRole 후, 단명 자격증명으로 SigV4(service `aoss`) 서명하여 컬렉션 엔드포인트에
  검색 요청을 보낸다. 매칭 문서를 index·id·score·본문(`_source`)과 함께 반환한다.
- 서버 요구 설정: `OPENSEARCH_ENDPOINT`, `OPENSEARCH_ROLE_ARN`
  (리전은 표준 AWS 설정 체인을 따른다. 운영 환경에서 role은 인프라가 데이터 액세스 정책을
  부여한 `kubernetes-homelab-k3s-mcp`를 가리킨다.)
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

### AC3: AssumeRole·SigV4 접근
- **설명**: 베이스 자격증명은 기본 체인(인스턴스 프로파일)에서 오고, 데이터 플레인 요청은
  STS AssumeRole로 얻은 단명 자격증명으로 SigV4 서명하여 수행한다. 정적 AWS 키를 사용하지
  않는다.
- **달성 가치**: V2
- **검증 방법**: 접근 경로가 AssumeRole → SigV4 서명 요청이며 정적 키 환경변수에 의존하지
  않는다.

### AC4: 미설정 시 graceful 거부
- **설명**: 필수 서버 설정이 없으면 unavailable 류 에러를 반환하며, 서버 기동·다른 도구에는
  영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
