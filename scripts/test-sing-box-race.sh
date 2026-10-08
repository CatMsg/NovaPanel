#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

go run -mod=readonly ./scripts/verify-sing-box.go
go test -mod=readonly -race ./scripts/verify-sing-box.go ./scripts/verify-sing-box_test.go \
  -count=1 -timeout=60s
go test -mod=readonly -race ./scripts -count=1 -timeout=60s
go test -mod=readonly -race github.com/sagernet/sing-box/route \
  -run '^TestNetworkManagerStarted' -count=20 -timeout=120s
go test -mod=readonly -race ./core \
  -run '^TestAddInboundNaiveRequiresUsers$' -count=20 -timeout=120s
go test -mod=readonly -race ./core -count=1 -timeout=180s
