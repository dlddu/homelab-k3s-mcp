# homelab-k3s-mcp 제품 가치 문서

이 문서는 homelab-k3s-mcp의 **최상위 판단 기준**이다. 이후 작성되는 모든 문서
(PRD, Acceptance Criteria, 테스트 문서)는 "이 문서가 어떤 가치에 기여하는가?"를
여기에 정의된 가치를 기준으로 판단한다.

## 제품 소유자

- **홈랩 운영자** — 이 제품의 가치를 정의하고 우선순위를 정하는 책임자.
  homelab-k3s-mcp가 운영하는 k3s 클러스터와 연결된 클라우드 리소스의 소유자이며,
  AI 어시스턴트(MCP)를 통해 자신의 홈랩을 운영하는 당사자이기도 하다.

## 제품 가치

### V1: 자연어로 클러스터 운영

- **유형**: 추상적
- **설명**: 운영자가 `kubectl`을 직접 작성하지 않고 AI 어시스턴트(MCP)를 통해 k3s
  클러스터를 운영한다. 네임스페이스·워크로드 조회, 컨테이너 로그 확인, 파드 진단,
  롤링 재시작, 레플리카 스케일 조정을 자연어 의도만으로 수행한다. 이 가치가 지향하는
  방향은 **홈랩 운영의 마찰(명령어 암기·반복 타이핑·컨텍스트 전환)을 줄이는 것**이다.
- **관련 도구**: `namespace_list`, `workload_list`, `workload_logs`, `pod_describe`,
  `workload_restart`, `workload_scale`, `ping`

### V2: 단명·최소권한 자격증명

- **유형**: 구체적
- **설명**: 장수 PAT나 클라우드 키를 복사해 들고 다니지 않고, 필요한 순간에 **짧은
  수명의 스코프된 자격증명**을 발급받는다. 서버의 베이스 자격증명(쿠버네티스
  인스턴스 프로파일, GitHub App 개인키, Grafana 발급 토큰)은 서버에만 머물고
  운영자에게 노출되지 않는다.
- **측정 가능 목표**:
  - GitHub App 설치 토큰 — 수명 약 1시간, repo 및 권한의 부분집합으로 스코프 가능
  - Grafana Cloud 토큰 — 수명 1시간, read-only(메트릭·로그)로 고정
  - AWS config — 정적 키 없이 AssumeRole로 **고정된 단일 S3 객체**만 조회
- **관련 도구**: `github_app_installation_token`, `grafana_token`, `aws_config_get`

### V3: 안전한 운영 (Safe-by-default)

- **유형**: 추상적 (일부 구체적)
- **설명**: 운영자가 도구를 신뢰하고 쓸 수 있도록 **기본값이 안전하게** 설계된다.
  의도하지 않은 파괴적 동작, 무인증 접근, 통합 미설정으로 인한 서버 다운이 기본적으로
  차단된다.
- **구체적 근거**:
  - `/mcp` 엔드포인트는 인증 없이 접근할 수 없다 — 대화형 클라이언트는 OAuth 2.0
    Bearer(RS256 JWT + JWKS 검증)로, 자동화(비대화형) 클라이언트는 정적 API 키로
    인증한다. 두 방식은 병행 가능하며 최소 하나는 활성이어야 한다.
  - 파괴적 도구는 `destructiveHint`로 명시된다 — `workload_restart`,
    `workload_scale`, `dear_baby_reset_user`.
  - 통합(k8s/GitHub/AWS/Grafana)이 미설정이어도 서버는 죽지 않고 해당 도구만 에러를
    반환한다(graceful degradation).
  - 클러스터 RBAC가 최소권한으로 제한된다 — 워크로드에 `get/list/watch/patch`만
    부여되고 `delete`·시크릿 읽기·워크로드 생성 권한이 없어, 가능한 피해 범위가
    구조적으로 제한된다.
- **관련 도구/구성**: 전 도구 공통(인증), `internal/auth`, `k8s/rbac.yaml`,
  도구 어노테이션(`destructiveHint`)

### V4: 운영 지식의 축적·검색

- **유형**: 추상적
- **설명**: 운영자가 홈랩을 운영하며 생기는 문서·기록·지식(트러블슈팅 노트, 구성 결정,
  절차 등)을 AI 어시스턴트를 통해 축적하고, 필요한 순간 자연어로 검색해 재사용한다. 이
  가치가 지향하는 방향은 **흩어진 운영 지식을 검색 가능한 한 곳에 모아, 같은 문제를 두 번
  풀지 않게 하는 것**이다.
- **기반 인프라**: OpenSearch Serverless(NextGen) `kubernetes-docs` 컬렉션 —
  scale-to-zero(min 0 OCU)로 유휴 시 compute 과금이 멈추고, 접근은 IAM(`aoss`)과 데이터
  액세스 정책의 2중 레이어로 통제된다.
- **관련 도구**: `opensearch_search`, `opensearch_document_put`,
  `opensearch_document_delete`

---

## 변경 이력

| 시점 | 변경 내용 |
|------|-----------|
| 2026-06-19 | 가치 문서 최초 생성. 소유자 지정(홈랩 운영자), V1~V3 정의. |
| 2026-07-02 | V4(운영 지식의 축적·검색) 추가 — OpenSearch Serverless `kubernetes-docs` 연동 도구 3종의 근거 가치. |
| 2026-07-04 | V3 인증 서술 확장 — `/mcp`에 정적 API 키 인증(비대화형 자동화용)을 OAuth와 병행 추가(platform PRD AC7·AC8). 새 가치 추가 없음. |
