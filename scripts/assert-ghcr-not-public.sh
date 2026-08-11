#!/usr/bin/env bash
set -euo pipefail

repository="${1:-}"
manifest="${2:-}"

if [[ ! "${repository}" =~ ^[a-z0-9][a-z0-9._/-]*$ ]]; then
  echo "usage: $0 owner/package [sha256:digest]" >&2
  exit 2
fi

endpoint="tags/list"
subject="ghcr.io/${repository}"
if [[ -n "${manifest}" ]]; then
  if [[ ! "${manifest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo "manifest must be an immutable sha256 digest" >&2
    exit 2
  fi
  endpoint="manifests/${manifest}"
  subject="${subject}@${manifest}"
fi

# The token is anonymous and is passed between processes over stdin. It is never
# printed, stored, or placed in a command argument.
status="$(
  curl --silent --show-error --get \
    --data-urlencode "scope=repository:${repository}:pull" \
    https://ghcr.io/token |
    jq --exit-status --raw-output \
      'if .token then
         "header = \"Authorization: Bearer \(.token)\""
       elif any(.errors[]?; .code == "DENIED") then
         "header = \"Authorization: Bearer denied\""
       else
         error("unexpected GHCR token response")
       end' |
    curl --silent --show-error --config - \
      --output /dev/null \
      --write-out '%{http_code}' \
      --header 'Accept: application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json' \
      "https://ghcr.io/v2/${repository}/${endpoint}"
)"

case "${status}" in
  200)
    echo "${subject} is anonymously pullable; refusing public GHCR publication" >&2
    exit 1
    ;;
  401 | 403 | 404)
    echo "${subject} is not anonymously pullable (HTTP ${status})"
    ;;
  *)
    echo "unexpected GHCR privacy-probe response for ${subject}: HTTP ${status}" >&2
    exit 1
    ;;
esac
