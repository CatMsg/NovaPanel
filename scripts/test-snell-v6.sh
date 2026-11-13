#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

if [[ -z "${NOVAPANEL_MIHOMO_TEST_BINARY:-}" ]]; then
  case "$(uname -s)/$(uname -m)" in
    Linux/x86_64)
      asset="mihomo-linux-amd64-v1.19.34.gz"
      digest="223fab877e5795ce780ba39902e5f0b1113c1256ae244a7160f1165e017e9ccc"
      ;;
    Darwin/arm64)
      asset="mihomo-darwin-arm64-v1.19.34.gz"
      digest="1a7a20eca63fbbaec90137717465bfde488d0db2a3a79b8fadebf9b2545b6e4c"
      ;;
    *)
      printf 'Set NOVAPANEL_MIHOMO_TEST_BINARY to a Snell-v6-capable Mihomo binary.\n' >&2
      exit 1
      ;;
  esac

  if command -v gh >/dev/null 2>&1 && { [[ -n "${GH_TOKEN:-}${GITHUB_TOKEN:-}${GH_ENTERPRISE_TOKEN:-}" ]] || gh auth status >/dev/null 2>&1; }; then
    gh api repos/MetaCubeX/mihomo/releases/tags/v1.19.34 > "$temporary/release.json"
    asset_id="$(jq -r --arg wanted "$asset" '[.assets[] | select(.name == $wanted) | .id][0] // empty' "$temporary/release.json")"
    [[ "$asset_id" =~ ^[0-9]+$ ]] || { printf 'Mihomo v1.19.34 asset not found: %s\n' "$asset" >&2; exit 1; }
    gh api -H 'Accept: application/octet-stream' "repos/MetaCubeX/mihomo/releases/assets/$asset_id" > "$temporary/mihomo.gz"
  else
    curl --fail --location --proto '=https' --proto-redir '=https' --tlsv1.2 --retry 3 --connect-timeout 10 --max-time 30 \
      'https://api.github.com/repos/MetaCubeX/mihomo/releases/tags/v1.19.34' -o "$temporary/release.json"
    asset_id="$(jq -r --arg wanted "$asset" '[.assets[] | select(.name == $wanted) | .id][0] // empty' "$temporary/release.json")"
    [[ "$asset_id" =~ ^[0-9]+$ ]] || { printf 'Mihomo v1.19.34 asset not found: %s\n' "$asset" >&2; exit 1; }
    curl --fail --location --proto '=https' --proto-redir '=https' --tlsv1.2 --retry 3 --connect-timeout 10 --max-time 120 \
      -H 'Accept: application/octet-stream' \
      "https://api.github.com/repos/MetaCubeX/mihomo/releases/assets/$asset_id" -o "$temporary/mihomo.gz"
  fi

  if command -v sha256sum >/dev/null; then
    printf '%s  %s\n' "$digest" "$temporary/mihomo.gz" | sha256sum -c -
  else
    printf '%s  %s\n' "$digest" "$temporary/mihomo.gz" | shasum -a 256 -c -
  fi
  gzip -dc "$temporary/mihomo.gz" > "$temporary/mihomo"
  chmod 700 "$temporary/mihomo"
  export NOVAPANEL_MIHOMO_TEST_BINARY="$temporary/mihomo"
fi

mkdir -p "$temporary/home"
export NOVAPANEL_MIHOMO_SNELL_CONFIG="$temporary/generated-snell-subscription.yaml"
GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.11}" bash ./scripts/go-test-sing-box.sh -mod=readonly ./sub \
  -run '^(TestSnellV6SharedPSKSubscriptions|TestSnellV6SubscriptionsPreserveAllModes)$' -count=1
"$NOVAPANEL_MIHOMO_TEST_BINARY" -d "$temporary/home" -t -f "$NOVAPANEL_MIHOMO_SNELL_CONFIG"

GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.11}" \
  bash ./scripts/go-test-sing-box.sh -mod=readonly ./core -run '^(TestSnellV6LoopbackIntegration|TestMihomoSnellV6SharedPSKLoopback)$' \
  -count=1 -timeout=90s -v
