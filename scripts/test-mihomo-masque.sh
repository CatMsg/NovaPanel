#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
release_asset_id() {
  local metadata="$1" asset_name="$2"
  if command -v jq >/dev/null 2>&1; then
    jq -r --arg wanted "$asset_name" '[.assets[] | select(.name == $wanted) | .id][0] // empty' "$metadata"
    return
  fi
  awk -v wanted="$asset_name" '
    /^    [{]$/ { in_asset=1; id="" }
    in_asset && /^      "id": [0-9]+,/ {
      id=$0
      sub(/^      "id": /, "", id)
      sub(/,.*/, "", id)
    }
    in_asset && /^      "name": "/ {
      name=$0
      sub(/^      "name": "/, "", name)
      sub(/".*/, "", name)
      if (name == wanted && id ~ /^[0-9]+$/) {
        print id
        exit
      }
    }
    in_asset && /^    [}]$/ { in_asset=0 }
  ' "$metadata"
}

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
  use_gh_api=0
  if command -v gh >/dev/null 2>&1; then
    if [[ -n "${GH_TOKEN:-}${GITHUB_TOKEN:-}${GH_ENTERPRISE_TOKEN:-}" ]] || gh auth status >/dev/null 2>&1; then
      use_gh_api=1
    fi
  fi
  if [[ "$use_gh_api" == 1 ]]; then
    gh api repos/MetaCubeX/mihomo/releases/tags/v1.19.31 > "$temporary/release.json"
  else
    curl --fail --location --proto '=https' --proto-redir '=https' --tlsv1.2 --retry 3 --connect-timeout 10 --max-time 30 \
      "https://api.github.com/repos/MetaCubeX/mihomo/releases/tags/v1.19.31" -o "$temporary/release.json"
  fi
  asset_id="$(release_asset_id "$temporary/release.json" "$asset")"
  if [[ ! "$asset_id" =~ ^[0-9]+$ ]]; then
    printf 'Mihomo Release API did not return asset %s.\n' "$asset" >&2
    exit 1
  fi
  if [[ "$use_gh_api" == 1 ]]; then
    gh api -H 'Accept: application/octet-stream' "repos/MetaCubeX/mihomo/releases/assets/$asset_id" > "$temporary/client.gz"
  else
    curl --fail --location --proto '=https' --proto-redir '=https' --tlsv1.2 --retry 3 --connect-timeout 10 --max-time 120 \
      -H 'Accept: application/octet-stream' \
      "https://api.github.com/repos/MetaCubeX/mihomo/releases/assets/$asset_id" -o "$temporary/client.gz"
  fi
  if command -v sha256sum >/dev/null; then
    printf '%s  %s\n' "$digest" "$temporary/client.gz" | sha256sum -c -
  else
    printf '%s  %s\n' "$digest" "$temporary/client.gz" | shasum -a 256 -c -
  fi
  gzip -dc "$temporary/client.gz" > "$temporary/mihomo"
  chmod 700 "$temporary/mihomo"
  export NOVAPANEL_MIHOMO_TEST_BINARY="$temporary/mihomo"
fi

export NOVAPANEL_MIHOMO_XHTTP_CONFIG="$temporary/aggregate-xhttp.yaml"
GOMAXPROCS=2 bash ./scripts/go-test-sing-box.sh -mod=readonly -p 2 ./sub \
  -run '^TestConvertRawClashProxiesPreservesXHTTPOptions$' -count=1
mkdir -p "$temporary/home"
HOME="$temporary/home" "$NOVAPANEL_MIHOMO_TEST_BINARY" -t -f "$NOVAPANEL_MIHOMO_XHTTP_CONFIG"

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
