# syntax=docker/dockerfile:1.7
FROM golang:1.26.5-alpine3.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/storage-backup ./cmd/storage-backup
COPY internal ./internal
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/storage-backup ./cmd/storage-backup

FROM postgres:18.4-alpine
COPY --from=build /out/storage-backup /usr/local/bin/storage-backup
USER 10002:70
ENTRYPOINT ["/usr/local/bin/storage-backup"]
CMD ["create"]
