#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

violations=0
while IFS= read -r -d '' file; do
  case "${file}" in
    */generated/* | */dist/* | */node_modules/* | *.gen.go | *.test.* | *_test.go)
      continue
      ;;
  esac
  lines="$(wc -l <"${file}")"
  if ((lines > 400)); then
    printf 'ERROR [file-policy] %s has %d lines; split or record an approved exception\n' "${file}" "${lines}" >&2
    violations=$((violations + 1))
  fi
  if [[ "${file}" == *.tsx ]] && ((lines > 250)); then
    printf 'ERROR [file-policy] React source %s exceeds 250 lines\n' "${file}" >&2
    violations=$((violations + 1))
  fi
done < <(find cmd internal web/src scripts -type f \( -name '*.go' -o -name '*.ts' -o -name '*.tsx' -o -name '*.js' -o -name '*.mjs' -o -name '*.css' \) -print0)

if find internal -type d \( -name utils -o -name helpers -o -name common \) -print -quit | rg --quiet .; then
  printf 'ERROR [file-policy] generic dumping-ground package name found\n' >&2
  violations=$((violations + 1))
fi
if ((violations > 0)); then
  exit 1
fi
printf 'file and package layout policy passed\n'
