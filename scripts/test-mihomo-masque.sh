#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
if [[ -z "${NOVAPANEL_MIHOMO_TEST_BINARY:-}" ]]; then
  case "$(uname -s)/$(uname -m)" in
    Darwin/arm64)
      asset="mihomo-darwin-arm64-v1.19.31.gz"
      digest="d131f44b3deb2a8356f7ac75048ad67a10d53243323951c4f3cda7b672922963"
      ;;
    Linux/x86_64)
      asset="mihomo-linux-amd64-v1.19.31.gz"
      digest="d5e74bbddbdfff49a1aef7775bf5911da59f0d7196ed509a0ac914b3653dd5f1"
      ;;
    *) printf 'Set NOVAPANEL_MIHOMO_TEST_BINARY for this platform.\n' >&2; exit 1 ;;
  esac
  curl --fail --location --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 10 --max-time 120 \
    "https://github.com/MetaCubeX/mihomo/releases/download/v1.19.31/$asset" -o "$temporary/client.gz"
  if command -v sha256sum >/dev/null; then
    printf '%s  %s\n' "$digest" "$temporary/client.gz" | sha256sum -c -
  else
    printf '%s  %s\n' "$digest" "$temporary/client.gz" | shasum -a 256 -c -
  fi
  gzip -dc "$temporary/client.gz" > "$temporary/mihomo"
  chmod 700 "$temporary/mihomo"
  export NOVAPANEL_MIHOMO_TEST_BINARY="$temporary/mihomo"
fi

GOMAXPROCS=2 bash ./scripts/go-test-sing-box.sh -mod=readonly -race -p 2 \
  ./core ./service ./sub \
  -run '^(TestManagedMASQUEMihomo.*|TestMASQUEMihomo.*)$' \
  -count=1 -timeout=120s -v | tee "$temporary/result.log"
for required in \
  TestManagedMASQUEMihomoBinaryTCPAndUDP \
  TestManagedMASQUEMihomoAuthenticationAndRoutes \
  TestMASQUEMihomoCredentialsStableBatchAndRevocation \
  TestMASQUEMihomoTemplateUsesDedicatedECDSAIdentity \
  TestMASQUEMihomoListMetadata \
  TestMASQUEMihomoSubscriptionIsolation \
  TestMASQUEMihomoGeneratedNodeAcceptedByBinary; do
  if ! grep -Fq -- "--- PASS: $required (" "$temporary/result.log"; then
    printf 'Required real Mihomo acceptance did not pass: %s\n' "$required" >&2
    exit 1
  fi
done
