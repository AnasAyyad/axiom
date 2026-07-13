#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

image="${1:-axiom:local}"
rebuild_image="${2:-axiom:local-rebuild}"
version="${VERSION:-dev}"
commit="${COMMIT:-unknown}"
built_at="${BUILT_AT:-unknown}"
dirty="${DIRTY:-true}"

docker image inspect "${image}" >/dev/null
docker build --file deploy/docker/Dockerfile --tag "${rebuild_image}" \
  --build-arg "VERSION=${version}" \
  --build-arg "COMMIT=${commit}" \
  --build-arg "BUILT_AT=${built_at}" \
  --build-arg "DIRTY=${dirty}" . >/dev/null

runtime_descriptor() {
  docker image inspect --format \
    '{{json .Config}}|{{json .RootFS}}|{{.Size}}|{{.Architecture}}|{{.Os}}' "$1"
}

if ! cmp <(runtime_descriptor "${image}") <(runtime_descriptor "${rebuild_image}"); then
  printf 'ERROR [image] runtime payload or configuration is not reproducible\n' >&2
  exit 1
fi

scripts/inspect-image.sh "${rebuild_image}" >/dev/null
fingerprint="$(runtime_descriptor "${image}" | sha256sum | awk '{print $1}')"
printf 'reproducible runtime image fingerprint: sha256:%s\n' "${fingerprint}"
