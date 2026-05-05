# syntax=docker/dockerfile:1.7

ARG RUST_VERSION=1
ARG DEBIAN_CODENAME=bookworm

FROM rust:${RUST_VERSION}-slim-${DEBIAN_CODENAME} AS builder
WORKDIR /app

RUN apt-get update \
 && apt-get install -y --no-install-recommends pkg-config \
 && rm -rf /var/lib/apt/lists/*

COPY Cargo.toml Cargo.lock ./
COPY src ./src

RUN cargo build --release --locked --bin homelab-k3s-mcp \
 && strip target/release/homelab-k3s-mcp

FROM gcr.io/distroless/cc-debian12:nonroot AS runtime

COPY --from=builder /app/target/release/homelab-k3s-mcp /usr/local/bin/homelab-k3s-mcp

ENV LISTEN_ADDR=0.0.0.0:3000
EXPOSE 3000
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/homelab-k3s-mcp"]
