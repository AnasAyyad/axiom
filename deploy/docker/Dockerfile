# syntax=docker/dockerfile:1.12@sha256:93bfd3b68c109427185cd78b4779fc82b484b0b7618e36d0f104d4d801e66d25

ARG NODE_VERSION=24.18.0
ARG GO_VERSION=1.26.5

FROM node:${NODE_VERSION}-bookworm-slim@sha256:6f7b03f7c2c8e2e784dcf9295400527b9b1270fd37b7e9a7285cf83b6951452d AS web-build
ARG PNPM_VERSION=11.12.0
ENV COREPACK_HOME=/corepack \
    PNPM_HOME=/pnpm \
    PATH=/pnpm:${PATH}
WORKDIR /src
RUN corepack enable && corepack install --global pnpm@${PNPM_VERSION}
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc ./
COPY web/package.json web/package.json
RUN --mount=type=cache,id=axiom-pnpm,target=/pnpm/store \
    pnpm fetch --frozen-lockfile --store-dir=/pnpm/store
COPY web web
RUN --mount=type=cache,id=axiom-pnpm,target=/pnpm/store \
    pnpm install --offline --frozen-lockfile --store-dir=/pnpm/store && \
    pnpm --filter @axiom/web build

FROM golang:${GO_VERSION}-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651 AS go-build
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown
ARG DIRTY=true
ENV CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
COPY pnpm-lock.yaml ./
RUN --mount=type=cache,id=axiom-go-mod,target=/go/pkg/mod \
    go mod download
COPY cmd cmd
COPY internal internal
COPY scripts/generate_sandbox_runtime_canary_config.go scripts/generate_sandbox_runtime_canary_config.go
COPY --from=web-build /src/web/dist /src/internal/api/static/dist
RUN --mount=type=cache,id=axiom-go-build,target=/root/.cache/go-build \
    GO_SUM_HASH="$(sha256sum go.sum | cut -d' ' -f1)" && \
    PNPM_LOCK_HASH="$(sha256sum pnpm-lock.yaml | cut -d' ' -f1)" && \
    go build -trimpath \
    -ldflags "-s -w -X axiom/internal/buildinfo.Version=${VERSION} -X axiom/internal/buildinfo.Commit=${COMMIT} -X axiom/internal/buildinfo.BuiltAt=${BUILT_AT} -X axiom/internal/buildinfo.Dirty=${DIRTY} -X axiom/internal/buildinfo.GoSumHash=${GO_SUM_HASH} -X axiom/internal/buildinfo.PNPMLockHash=${PNPM_LOCK_HASH}" \
    -o /out/platform ./cmd/platform
RUN go run ./scripts/generate_sandbox_runtime_canary_config.go \
      --exchange binance \
      --output /out/platform-binance-testnet.json && \
    go run ./scripts/generate_sandbox_runtime_canary_config.go \
      --exchange bybit \
      --output /out/platform-bybit-demo.json

FROM scratch AS runtime
LABEL org.opencontainers.image.title="Axiom" \
      org.opencontainers.image.description="Axiom public-data research and simulation platform" \
      org.opencontainers.image.licenses="UNLICENSED"
COPY --from=go-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=go-build /usr/share/zoneinfo/UTC /usr/share/zoneinfo/UTC
COPY --from=go-build --chown=10001:70 /out/platform /app/platform
COPY --chown=10001:70 deploy/config/platform-shadow.json /etc/axiom/platform.json
COPY --chown=10001:70 deploy/config/platform-research.json /etc/axiom/platform-research.json
COPY --from=go-build --chown=10001:70 /out/platform-binance-testnet.json /etc/axiom/platform-binance-testnet.json
COPY --from=go-build --chown=10001:70 /out/platform-bybit-demo.json /etc/axiom/platform-bybit-demo.json
ENV TZ=UTC
USER 10001:70
EXPOSE 8080 9091
ENTRYPOINT ["/app/platform"]
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=8 \
  CMD ["/app/platform", "healthcheck", "--url=http://127.0.0.1:8080/health/ready"]
