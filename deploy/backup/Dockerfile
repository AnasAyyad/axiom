# syntax=docker/dockerfile:1.12@sha256:93bfd3b68c109427185cd78b4779fc82b484b0b7618e36d0f104d4d801e66d25

ARG SOURCE_DATE_EPOCH=0

# PostgreSQL client tools are built from the checksum-locked upstream source.
# The compiler stage is never copied wholesale into the runtime image.
FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40 AS postgres-build
RUN apk add --no-cache \
      bison \
      build-base \
      ca-certificates \
      curl \
      flex \
      linux-headers \
      lz4-dev \
      openssl-dev \
      perl \
      zlib-dev \
      zstd-dev
WORKDIR /build
RUN set -eu; \
    curl --fail --location --silent --show-error \
      --output postgresql.tar.bz2 \
      https://ftp.postgresql.org/pub/source/v18.4/postgresql-18.4.tar.bz2; \
    echo '81a81ec695fb0c7901407defaa1d2f7973617154cf27ba74e3a7ab8e64436094  postgresql.tar.bz2' | sha256sum -c -; \
    tar -xjf postgresql.tar.bz2; \
    mv postgresql-18.4 postgresql
WORKDIR /build/postgresql
RUN set -eu; \
    ./configure \
      --prefix=/usr/local/pgsql \
      --with-lz4 \
      --with-ssl=openssl \
      --with-zlib \
      --with-zstd \
      --without-gssapi \
      --without-icu \
      --without-ldap \
      --without-pam \
      --without-readline; \
    make -C src/backend generated-headers; \
    make -j2 -C src/bin/pg_dump; \
    make -j2 -C src/bin/psql; \
    make -C src/interfaces/libpq install; \
    make -C src/bin/pg_dump install; \
    make -C src/bin/psql install
RUN set -eu; \
    root=/runtime; \
    mkdir -p \
      "$root/etc/ssl/certs" \
      "$root/usr/local/bin" \
      "$root/usr/share/axiom/licenses"; \
    for tool in pg_dump pg_restore psql; do \
      cp "/usr/local/pgsql/bin/$tool" "$root/usr/local/bin/$tool"; \
    done; \
    for tool in pg_dump pg_restore psql; do \
      LD_LIBRARY_PATH=/usr/local/pgsql/lib ldd "/usr/local/pgsql/bin/$tool"; \
    done \
      | awk '$2 == "=>" { print $3 } $1 ~ /^\// { print $1 }' \
      | sort -u \
      | while IFS= read -r library; do \
          relative=${library#/}; \
          mkdir -p "$root/$(dirname "$relative")"; \
          cp -L "$library" "$root/$relative"; \
        done; \
    cp /etc/ssl/certs/ca-certificates.crt "$root/etc/ssl/certs/ca-certificates.crt"; \
    cp COPYRIGHT "$root/usr/share/axiom/licenses/PostgreSQL.txt"; \
    apk info -v | LC_ALL=C sort > "$root/usr/share/axiom/postgres-build-components.txt"; \
    printf '%s  %s\n' \
      81a81ec695fb0c7901407defaa1d2f7973617154cf27ba74e3a7ab8e64436094 \
      postgresql-18.4.tar.bz2 \
      > "$root/usr/share/axiom/postgresql-source.sha256"; \
    chown -R 0:0 "$root"; \
    chmod -R go-w "$root"; \
    find "$root" -exec touch -d @0 '{}' +

FROM golang:1.26.5-alpine3.23@sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/storage-backup ./cmd/storage-backup
COPY internal ./internal
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/storage-backup ./cmd/storage-backup && \
    touch -d @0 /out/storage-backup

FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40 AS assemble
COPY --from=postgres-build /runtime /rootfs
COPY --from=build /out/storage-backup /rootfs/usr/local/bin/storage-backup
COPY deploy/backup/runtime-components.json /rootfs/usr/share/axiom/runtime-components.json
COPY deploy/backup/THIRD_PARTY_NOTICES.md /rootfs/usr/share/axiom/THIRD_PARTY_NOTICES.md
RUN set -eu; \
    chown -R 0:0 /rootfs; \
    chmod -R go-w /rootfs; \
    find /rootfs -exec touch -d @0 '{}' +

FROM scratch AS runtime
LABEL org.opencontainers.image.title="Axiom PostgreSQL backup and restore" \
      org.opencontainers.image.description="Minimal PostgreSQL 18.4 backup and restore helper" \
      org.opencontainers.image.licenses="UNLICENSED" \
      org.opencontainers.image.version="18.4" \
      org.opencontainers.image.source="https://github.com/AnasAyyad/axiom"
COPY --from=assemble /rootfs /
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
USER 10002:70
ENTRYPOINT ["/usr/local/bin/storage-backup"]
CMD ["create"]
