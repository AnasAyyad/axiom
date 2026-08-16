# syntax=docker/dockerfile:1.12

ARG GO_IMAGE=golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,id=axiom-sandbox-qualification-go-mod,target=/go/pkg/mod \
    go mod download && \
    mkdir -p /out/gomod && \
    cp -a /go/pkg/mod/. /out/gomod/
COPY Makefile ./
COPY cmd cmd
COPY internal internal
RUN --mount=type=cache,id=axiom-sandbox-qualification-go-mod,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-buildid=" \
    -o /out/sandbox-qualification-chaos ./cmd/sandbox-qualification-chaos

FROM ${GO_IMAGE} AS runtime
ARG COMMIT=unknown
LABEL org.opencontainers.image.title="Axiom sandbox qualification controller" \
      org.opencontainers.image.description="Credential-free deterministic sandbox qualification gate controller" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.licenses="UNLICENSED"
COPY --from=build /out/gomod /go/pkg/mod
COPY --from=build /out/sandbox-qualification-chaos /app/sandbox-qualification-chaos
WORKDIR /qualification/source
ENV CGO_ENABLED=0 \
    GOCACHE=/tmp/go-build \
    GOFLAGS=-mod=readonly \
    GOMODCACHE=/go/pkg/mod \
    GOTOOLCHAIN=local \
    HOME=/tmp
ENTRYPOINT ["/app/sandbox-qualification-chaos"]
