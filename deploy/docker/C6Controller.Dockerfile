# syntax=docker/dockerfile:1.12

ARG GO_IMAGE=golang:1.26.5-bookworm

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,id=axiom-c6-go-mod,target=/go/pkg/mod \
    go mod download && \
    mkdir -p /out/gomod && \
    cp -a /go/pkg/mod/. /out/gomod/
COPY Makefile ./
COPY cmd cmd
COPY internal internal
RUN --mount=type=cache,id=axiom-c6-go-mod,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-buildid=" \
    -o /out/c6-chaos ./cmd/c6-chaos

FROM ${GO_IMAGE} AS runtime
ARG COMMIT=unknown
LABEL org.opencontainers.image.title="Axiom C6 qualification controller" \
      org.opencontainers.image.description="Credential-free deterministic C6 gate controller" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.licenses="UNLICENSED"
COPY --from=build /out/gomod /go/pkg/mod
COPY --from=build /out/c6-chaos /app/c6-chaos
WORKDIR /qualification/source
ENV CGO_ENABLED=0 \
    GOCACHE=/tmp/go-build \
    GOFLAGS=-mod=readonly \
    GOMODCACHE=/go/pkg/mod \
    GOTOOLCHAIN=local \
    HOME=/tmp
ENTRYPOINT ["/app/c6-chaos"]
