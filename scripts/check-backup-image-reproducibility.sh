#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

image="${1:-axiom-backup:local}"
rebuild_image="${2:-axiom-backup:local-rebuild}"

docker image inspect "${image}" >/dev/null
docker build --no-cache --file deploy/backup/Dockerfile --tag "${rebuild_image}" . >/dev/null

runtime_descriptor() {
  docker image inspect --format \
    '{{json .Config}}|{{json .RootFS}}|{{.Size}}|{{.Architecture}}|{{.Os}}' "$1"
}

if ! cmp <(runtime_descriptor "${image}") <(runtime_descriptor "${rebuild_image}"); then
  printf 'ERROR [backup-image] runtime payload or configuration is not reproducible\n' >&2
  exit 1
fi

scripts/inspect-backup-image.sh "${rebuild_image}" >/dev/null
fingerprint="$(runtime_descriptor "${image}" | sha256sum | awk '{print $1}')"
printf 'reproducible backup image fingerprint: sha256:%s\n' "${fingerprint}"
