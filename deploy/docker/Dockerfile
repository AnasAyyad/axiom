# syntax=docker/dockerfile:1.12

ARG NODE_VERSION=24.18.0
ARG GO_VERSION=1.26.5

FROM node:${NODE_VERSION}-bookworm-slim AS web-build
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

FROM golang:${GO_VERSION}-bookworm AS go-build
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown
ARG DIRTY=true
ENV CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,id=axiom-go-mod,target=/go/pkg/mod \
    go mod download
COPY cmd cmd
COPY internal internal
COPY --from=web-build /src/web/dist /src/internal/api/static/dist
RUN --mount=type=cache,id=axiom-go-build,target=/root/.cache/go-build \
    go build -trimpath \
    -ldflags "-s -w -X axiom/internal/buildinfo.Version=${VERSION} -X axiom/internal/buildinfo.Commit=${COMMIT} -X axiom/internal/buildinfo.BuiltAt=${BUILT_AT} -X axiom/internal/buildinfo.Dirty=${DIRTY}" \
    -o /out/platform ./cmd/platform

FROM scratch AS runtime
LABEL org.opencontainers.image.title="Axiom" \
      org.opencontainers.image.description="V1A public-data research and simulation platform" \
      org.opencontainers.image.licenses="UNLICENSED"
COPY --from=go-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=go-build /usr/share/zoneinfo/UTC /usr/share/zoneinfo/UTC
COPY --from=go-build --chown=10001:70 /out/platform /app/platform
COPY --chown=10001:70 deploy/config/platform-shadow.json /etc/axiom/platform.json
ENV TZ=UTC
USER 10001:70
EXPOSE 8080 9091
ENTRYPOINT ["/app/platform"]
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=8 \
  CMD ["/app/platform", "healthcheck", "--url=http://127.0.0.1:8080/health/ready"]
