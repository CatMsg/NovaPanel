#!/usr/bin/env bash
set -euo pipefail

workflow_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.github/workflows" && pwd)"
checked=0

for workflow in "$workflow_dir"/*.yml "$workflow_dir"/*.yaml; do
  [[ -f "$workflow" ]] || continue
  awk -v file="$workflow" '
    /^[[:space:]]*uses:[[:space:]]*/ {
      ref = $2
      if (ref ~ /^\.\//) next
      count = split(ref, parts, "@")
      sha = parts[count]
      if (count < 2 || length(sha) != 40 || sha ~ /[^0-9a-f]/) {
        printf "%s:%d: action must use a full 40-character commit SHA: %s\n", file, FNR, $0 > "/dev/stderr"
        failed = 1
      }
    }
    END { exit failed }
  ' "$workflow"
  checked=$((checked + 1))
done

if (( checked == 0 )); then
  echo "no GitHub Actions workflow files found" >&2
  exit 1
fi

printf 'Verified immutable Action refs in %d workflow files.\n' "$checked"
