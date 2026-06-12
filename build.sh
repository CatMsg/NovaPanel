#!/bin/sh
set -e

retry() {
  max_attempts=3
  attempt=1
  delay=3

  while :; do
    "$@" && return 0

    if [ "$attempt" -ge "$max_attempts" ]; then
      return 1
    fi

    echo "Retry $attempt/$max_attempts failed, retrying in ${delay}s..."
    sleep "$delay"
    attempt=$((attempt + 1))
  done
}

cd frontend
npm i
npm run build

cd ..
echo "Backend"

mkdir -p web/html
rm -fr web/html/*
cp -R frontend/dist/* web/html/

BUILD_TAGS="with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_musl,badlinkname,tfogo_checklinkname0,with_tailscale"
retry go mod download
retry go build -ldflags '-w -s -checklinkname=0 -extldflags "-Wl,-no_warn_duplicate_libraries"' -tags "$BUILD_TAGS" -o sui main.go
