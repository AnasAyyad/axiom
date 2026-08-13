#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

image="${1:-axiom-backup:local}"

docker image inspect "${image}" >/dev/null

test "$(docker image inspect --format '{{.Config.User}}' "${image}")" = "10002:70"
test "$(docker image inspect --format '{{json .Config.Entrypoint}}' "${image}")" = '["/usr/local/bin/storage-backup"]'
test "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "${image}")" = "18.4"

for tool in pg_dump pg_restore psql; do
  version="$(docker run --rm --entrypoint "/usr/local/bin/${tool}" "${image}" --version)"
  case "${version}" in
    *"(PostgreSQL) 18.4") ;;
    *)
      printf 'ERROR [backup-image] unexpected %s version: %s\n' "${tool}" "${version}" >&2
      exit 1
      ;;
  esac
done

container="$(docker create --entrypoint /usr/local/bin/storage-backup "${image}" create)"
cleanup() {
  docker rm --force "${container}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

contents="$(docker export "${container}" | tar -tf -)"
for required in \
  etc/ssl/certs/ca-certificates.crt \
  usr/local/bin/pg_dump \
  usr/local/bin/pg_restore \
  usr/local/bin/psql \
  usr/local/bin/storage-backup \
  usr/share/axiom/postgres-build-components.txt \
  usr/share/axiom/postgresql-source.sha256 \
  usr/share/axiom/runtime-components.json \
  usr/share/axiom/THIRD_PARTY_NOTICES.md \
  usr/share/axiom/licenses/PostgreSQL.txt; do
  if ! grep -qx "${required}" <<<"${contents}"; then
    printf 'ERROR [backup-image] missing required runtime path: %s\n' "${required}" >&2
    exit 1
  fi
done

for forbidden in \
  bin/sh \
  sbin/apk \
  usr/bin/gosu \
  usr/local/bin/postgres \
  usr/local/bin/pg_ctl \
  var/lib/postgresql/data; do
  if grep -Eq "^${forbidden}(/|$)" <<<"${contents}"; then
    printf 'ERROR [backup-image] forbidden runtime path present: %s\n' "${forbidden}" >&2
    exit 1
  fi
done

printf 'backup image is minimal, non-root, source-identified, and PostgreSQL 18.4 compatible: %s\n' "${image}"
