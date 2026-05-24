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

FROM gcr.io/distroless/static-debian12:nonroot AS runtime

COPY --from=builder /out/homelab-k3s-mcp /usr/local/bin/homelab-k3s-mcp

ENV LISTEN_ADDR=0.0.0.0:3000
EXPOSE 3000
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/homelab-k3s-mcp"]
