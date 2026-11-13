#!/usr/bin/env bash
set -euo pipefail
export CGO_LDFLAGS_ALLOW="${CGO_LDFLAGS_ALLOW:-^-l:libcronet[.]a$}"

packages="$(go list ./... | grep -v '/frontend/node_modules/' || true)"

if [[ -z "${packages}" ]]; then
  echo "no Go packages found"
  exit 1
fi

core_package="$(go list ./core)"
other_packages="$(printf '%s\n' "${packages}" | grep -Fvx "${core_package}" || true)"

if [[ -n "${other_packages}" ]]; then
  printf '%s\n' "${other_packages}" | xargs go test -mod=readonly
fi

bash ./scripts/go-test-sing-box.sh -mod=readonly "${core_package}"
