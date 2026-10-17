#!/usr/bin/env bash
set -euo pipefail

# Keep the Sing-Box test build aligned with the feature tags used by build.sh.
exec go test \
  -tags 'with_quic,badlinkname,tfogo_checklinkname0' \
  -ldflags='-checklinkname=0' \
  "$@"
