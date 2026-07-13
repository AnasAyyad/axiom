#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'
export LC_ALL=C

readonly expression='(?:-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----|\bAKIA[0-9A-Z]{16}\b|\bgh[pousr]_[A-Za-z0-9]{30,}\b|\bgithub_pat_[A-Za-z0-9_]{40,}\b|(?i:\b(?:api[_-]?key|api[_-]?secret|access[_-]?token|client[_-]?secret|password)\b[[:space:]]*[:=][[:space:]]*[\x22\x27]?[A-Za-z0-9+/=_-]{24,}))'
matches="$(rg --hidden --no-ignore-vcs --pcre2 --color never --no-heading --with-filename --line-number \
  --glob '!.git/**' --glob '!.secrets/**' --glob '!.local/**' --glob '!node_modules/**' \
  --glob '!web/dist/**' --glob '!internal/api/static/dist/**' \
  --glob '!scripts/check-secret-patterns.sh' --glob '!scripts/test-check-secret-patterns.sh' \
  --regexp "${expression}" . || true)"
if [[ -n "${matches}" ]]; then
  while IFS=: read -r file line _; do
    printf 'ERROR [secret-scan] %s:%s: potential secret material\n' "${file}" "${line}" >&2
  done <<<"${matches}"
  exit 1
fi
printf 'secret-pattern scan passed\n'
