# PRD: opensearch_document_put

OpenSearch Serverless 컬렉션(운영: `kubernetes-docs`)의 인덱스에 JSON 문서를
색인(업서트)하는 도구.

## 달성 가치
- **V4: 운영 지식의 축적·검색** — 운영 문서·기록을 축적하는 쓰기 경로.
- **V2: 단명·최소권한 자격증명** — 정적 키 없이 AssumeRole 단명 자격증명으로 SigV4 서명
  접근한다.
- **V3: 안전한 운영(Safe-by-default)** — 기존 문서를 덮어쓸 수 있음을 `destructiveHint`로
  명시하고, 미설정 시 graceful 거부가 기본값이다.

## 도구 개요
- 입력: `index`(필수), `document`(필수, JSON 객체), `id`(선택 — 지정 시 해당 id로 업서트,
  미지정 시 자동 생성)
- 동작: SigV4(service `aoss`) 서명으로 문서를 색인한다. 대상 인덱스가 없으면 색인 시 자동
  생성된다. 결과로 index·id·result(created/updated)를 반환한다. (검색 노출은 refresh 이후)
- 서버 요구 설정: `OPENSEARCH_ENDPOINT`, `OPENSEARCH_ROLE_ARN` (opensearch_search와 공유)
- 어노테이션: `readOnlyHint=false`, `destructiveHint=true`, `idempotentHint=false`,
  `openWorldHint=true`
  (idempotent=false: id 미지정 반복 호출은 문서를 중복 생성한다)

## Acceptance Criteria

### AC1: 문서 색인·업서트
- **설명**: `index`와 `document`로 문서를 색인하고 index·id·result를 반환한다. `id` 지정 시
  같은 id 재호출은 기존 문서를 덮어쓰며(updated), 미지정 시 id가 자동 생성된다(created).
- **달성 가치**: V4
- **검증 방법**: 최초 색인은 created, 같은 id 재색인은 updated가 반환되고 이후 검색에서
  새 본문이 조회된다. id 미지정 호출은 서로 다른 자동 id를 받는다.

### AC2: 인덱스 자동 생성
- **설명**: 존재하지 않는 인덱스에 색인하면 인덱스가 자동 생성되고 문서가 색인된다. 별도의
  인덱스 관리 도구 없이 쓰기 경로만으로 지식 축적을 시작할 수 있다.
- **달성 가치**: V4
- **검증 방법**: 사전에 없던 인덱스명으로 색인 후 해당 인덱스에서 문서가 검색된다.

### AC3: 파괴적 작업 표기
- **설명**: `tools/list`에서 이 도구가 `destructiveHint=true`로 광고된다(id 지정 업서트는
  기존 문서를 덮어쓴다).
- **달성 가치**: V3
- **검증 방법**: `tools/list` 응답의 어노테이션이 destructiveHint=true이다.

### AC4: AssumeRole·SigV4 접근
- **설명**: 베이스 자격증명은 기본 체인에서 오고, 데이터 플레인 요청은 STS AssumeRole로
  얻은 단명 자격증명으로 SigV4 서명하여 수행한다. 정적 AWS 키를 사용하지 않는다.
- **달성 가치**: V2
- **검증 방법**: 접근 경로가 AssumeRole → SigV4 서명 요청이며 정적 키 환경변수에 의존하지
  않는다.

### AC5: 미설정 시 graceful 거부
- **설명**: 필수 서버 설정이 없으면 unavailable 류 에러를 반환하며, 서버 기동·다른 도구에는
  영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
