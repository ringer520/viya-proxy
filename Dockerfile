# Viya multi-stage build — produces a minimal Alpine image.
#
# Multi-arch (CI / buildx):
#   docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 \
#     --build-arg VERSION=v1.0.0 -t ringer520/viya-proxy:latest --push .
#
# Single-arch (local):
#   docker build --build-arg VERSION=dev -t viya-proxy .

# ---- Stage 1: frontend (Vue 3 + Vite) — always built natively ----
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
RUN corepack enable && corepack prepare pnpm@9 --activate
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# ---- Stage 2: Caddy (separate Go module, cross-compiled) ----
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS caddy-builder
ARG TARGETARCH
ARG TARGETVARIANT
WORKDIR /src/build/caddy
COPY build/caddy/go.mod build/caddy/go.sum ./
RUN go mod download
COPY build/caddy/ ./
RUN set -e; \
    ARMV=""; \
    if [ "$TARGETARCH" = "arm" ]; then ARMV="${TARGETVARIANT#v}"; fi; \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH GOARM=$ARMV \
      go build -trimpath -buildvcs=false -ldflags "-s -w" -o /out/caddy .

# ---- Stage 3: Viya (control plane + embedded frontend, cross-compiled) ----
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS viya-builder
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
ARG BUILD_COMMIT=unknown
ARG BUILD_TIME=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=frontend /src/internal/web/dist internal/web/dist
RUN set -e; \
    ARMV=""; \
    if [ "$TARGETARCH" = "arm" ]; then ARMV="${TARGETVARIANT#v}"; fi; \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH GOARM=$ARMV \
      go build -trimpath -buildvcs=false \
        -ldflags "-s -w -X main.version=${VERSION} -X main.buildCommit=${BUILD_COMMIT} -X main.buildTime=${BUILD_TIME} -X main.buildSource=${BUILD_COMMIT}" \
        -o /out/viya ./cmd/viya

# ---- Stage 4: runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata

COPY --from=viya-builder /out/viya /opt/viya/bin/viya
COPY --from=caddy-builder /out/caddy /opt/viya/bin/caddy
RUN chmod +x /opt/viya/bin/viya /opt/viya/bin/caddy

ENV VIYA_LISTEN=0.0.0.0:8181 \
    VIYA_DATA_DIR=/var/lib/viya \
    VIYA_MASTER_KEY_FILE=/var/lib/viya/master.key \
    VIYA_CADDY_BIN=/opt/viya/bin/caddy \
    VIYA_CADDY_ADMIN=http://127.0.0.1:2019 \
    HOME=/var/lib/viya

WORKDIR /var/lib/viya
VOLUME ["/var/lib/viya"]
EXPOSE 8181 80 443 443/udp

ENTRYPOINT ["/opt/viya/bin/viya"]
