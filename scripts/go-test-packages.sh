#!/usr/bin/env bash
set -euo pipefail

packages="$(go list ./... | grep -v '/frontend/node_modules/' || true)"

if [[ -z "${packages}" ]]; then
  echo "no Go packages found"
  exit 1
fi

printf '%s\n' "${packages}" | xargs go test
