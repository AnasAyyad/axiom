#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

usage() {
  printf 'Usage: %s REGION [DURATION] [OUTPUT]\n' "$(basename -- "$0")"
  printf 'Example: %s aws-ap-northeast-1 12m /tmp/axiom-cross-tokyo\n' "$(basename -- "$0")"
}

[[ $# -ge 1 && $# -le 3 ]] || { usage >&2; exit 2; }
region="$1"
duration="${2:-12m}"
output="${3:-/tmp/axiom-cross-exchange-${region}-$(date -u +%Y%m%dT%H%M%SZ)}"

case "$region" in
  aws-ap-southeast-1|aws-ap-northeast-1|aws-ap-northeast-3) ;;
  *) printf 'Unsupported region: %s\n' "$region" >&2; exit 2 ;;
esac

for command in chronyc git go jq tee; do
  command -v "$command" >/dev/null 2>&1 || { printf 'Missing command: %s\n' "$command" >&2; exit 1; }
done

tracking="$(chronyc tracking)"
sources="$(chronyc sources -n)"
grep -Eq '^Leap status[[:space:]]*:[[:space:]]*Normal$' <<<"$tracking" || {
  printf 'Chrony is not synchronized\n' >&2
  exit 1
}
awk '$1 ~ /^\^\*/ { found=1 } END { exit !found }' <<<"$sources" || {
  printf 'Chrony has no selected source\n' >&2
  exit 1
}

commit="$(git rev-parse HEAD)"
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || { printf 'Exact commit unavailable\n' >&2; exit 1; }
[[ -z "$(git status --porcelain)" ]] || { printf 'Repository must be clean\n' >&2; exit 1; }
[[ ! -e "$output" ]] || { printf 'Output already exists: %s\n' "$output" >&2; exit 1; }

started="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
printf '[%s] Starting public-only cross-exchange coherence probe\n' "$started"
printf 'Region: %s\nDuration: %s\nCommit: %s\nOutput: %s\n' "$region" "$duration" "$commit" "$output"
printf 'Safety: no credentials, no orders, no formal qualification clock\n'

AXIOM_CROSS_EXCHANGE_PROBE_PUBLIC=1 \
AXIOM_CROSS_EXCHANGE_PROBE_REGION="$region" \
AXIOM_CROSS_EXCHANGE_PROBE_COMMIT="$commit" \
AXIOM_CROSS_EXCHANGE_PROBE_DURATION="$duration" \
AXIOM_CROSS_EXCHANGE_PROBE_OUTPUT="$output" \
go test ./internal/bootstrap -run '^TestCrossExchangeActionablePublicProbe$' -count=1 -timeout=18m -v \
  2>&1 | tee "${output}.log"

jq '{region,source_commit,samples,duplicate_triggers,strict_passes,actionable_passes,
  strict_rejections,actionable_rejections,receive_skew_p50_nanos,receive_skew_p95_nanos,
  receive_skew_p99_nanos,corrected_overlap_p50_nanos,corrected_overlap_p95_nanos,
  book_age_p95_nanos,book_age_p99_nanos,source_delay_p95_nanos,source_delay_p99_nanos,
  final_health}' "$output/summary.json"
printf '[%s] Probe complete: %s\n' "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" "$output"
