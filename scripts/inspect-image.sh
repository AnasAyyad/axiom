#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

image="${1:-axiom:local}"
user="$(docker image inspect --format '{{.Config.User}}' "${image}")"
if [[ "${user}" != "10001:70" ]]; then
  printf 'ERROR [image] runtime user is %s, want 10001:70\n' "${user}" >&2
  exit 1
fi
entrypoint="$(docker image inspect --format '{{json .Config.Entrypoint}}' "${image}")"
if [[ "${entrypoint}" != '["/app/platform"]' ]]; then
  printf 'ERROR [image] unexpected entrypoint\n' >&2
  exit 1
fi
if docker run --rm --entrypoint /bin/sh "${image}" -c true >/dev/null 2>&1; then
  printf 'ERROR [image] runtime unexpectedly contains a shell\n' >&2
  exit 1
fi
docker run --rm --read-only "${image}" help >/dev/null
if docker image inspect --format '{{json .Config.Env}}' "${image}" | \
  rg --ignore-case --quiet '(api[_-]?key|api[_-]?secret|credential|private[_-]?key)'; then
  printf 'ERROR [image] credential-like runtime environment key found\n' >&2
  exit 1
fi
printf 'minimal non-root read-only image inspection passed\n'
