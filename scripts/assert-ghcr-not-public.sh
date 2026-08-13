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

# GHCR can deny an anonymous pull at either the token endpoint or the registry
# endpoint. Accept only explicit authorization-denial codes. Unknown token
# responses, transport errors, and rate limits remain fail-closed errors.
token_response="$(mktemp)"
trap 'rm -f -- "${token_response}"' EXIT
token_status="$(
  curl --silent --show-error --get \
    --output "${token_response}" \
    --write-out '%{http_code}' \
    --data-urlencode "scope=repository:${repository}:pull" \
    https://ghcr.io/token
)"

explicit_denial=false
if jq --exit-status \
  'any(.errors[]?; .code == "DENIED" or .code == "UNAUTHORIZED")' \
  "${token_response}" >/dev/null; then
  explicit_denial=true
fi

case "${token_status}" in
  200)
    if [[ "${explicit_denial}" == true ]]; then
      status=403
    else
      # The anonymous token is passed to curl over stdin. It is never printed or
      # placed in a command argument, and the mode-600 response is removed on exit.
      status="$(
        jq --exit-status --raw-output \
          'if .token then
             "header = \"Authorization: Bearer \(.token)\""
           else
             error("unexpected GHCR token response")
           end' "${token_response}" |
        curl --silent --show-error --config - \
          --output /dev/null \
          --write-out '%{http_code}' \
          --header 'Accept: application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json' \
          "https://ghcr.io/v2/${repository}/${endpoint}"
      )"
    fi
    ;;
  401 | 403)
    if [[ "${explicit_denial}" != true ]]; then
      echo "unexpected GHCR token-service response for ${subject}: HTTP ${token_status}" >&2
      exit 1
    fi
    status="${token_status}"
    ;;
  *)
    echo "unexpected GHCR token-service response for ${subject}: HTTP ${token_status}" >&2
    exit 1
    ;;
esac

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
