# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.24
ARG DEBIAN_CODENAME=bookworm

FROM golang:${GO_VERSION}-${DEBIAN_CODENAME} AS builder
WORKDIR /app

ENV CGO_ENABLED=0 GOTOOLCHAIN=local

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -trimpath -ldflags="-s -w" -o /out/homelab-k3s-mcp .

# 런타임 설정은 이 한 곳에만 둔다.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime-base

ENV LISTEN_ADDR=0.0.0.0:3000
EXPOSE 3000
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/homelab-k3s-mcp"]

# builder 스테이지는 캐시 마운트가 GHA 러너 사이에 이어지지 않아 소스가 바뀔 때마다
# 의존성까지 통째로 다시 컴파일한다.
FROM runtime-base AS runtime-prebuilt
COPY homelab-k3s-mcp /usr/local/bin/homelab-k3s-mcp

# 기본 target 이므로 마지막 스테이지로 둔다.
FROM runtime-base AS runtime
COPY --from=builder /out/homelab-k3s-mcp /usr/local/bin/homelab-k3s-mcp
