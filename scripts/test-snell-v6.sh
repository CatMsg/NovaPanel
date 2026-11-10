#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

if [[ -z "${NOVAPANEL_MIHOMO_TEST_BINARY:-}" ]]; then
  case "$(uname -s)/$(uname -m)" in
    Linux/x86_64)
      asset="mihomo-linux-amd64-alpha-478af2a.gz"
      digest="452daf119644c2f74d5fe7aad183264189ed31cf5ae13ba3579be0abc6fe6810"
      ;;
    Darwin/arm64)
      asset="mihomo-darwin-arm64-alpha-478af2a.gz"
      digest="efc4aa19453da5070960916faf592f19db4a084bc513bf91450d919bfb699635"
      ;;
    *)
      printf 'Set NOVAPANEL_MIHOMO_TEST_BINARY to a Snell-v6-capable Mihomo Alpha binary.\n' >&2
      exit 1
      ;;
  esac

  if command -v gh >/dev/null 2>&1 && { [[ -n "${GH_TOKEN:-}${GITHUB_TOKEN:-}${GH_ENTERPRISE_TOKEN:-}" ]] || gh auth status >/dev/null 2>&1; }; then
    gh api repos/MetaCubeX/mihomo/releases/tags/Prerelease-Alpha > "$temporary/release.json"
    asset_id="$(jq -r --arg wanted "$asset" '[.assets[] | select(.name == $wanted) | .id][0] // empty' "$temporary/release.json")"
    [[ "$asset_id" =~ ^[0-9]+$ ]] || { printf 'Mihomo Alpha asset not found: %s\n' "$asset" >&2; exit 1; }
    gh api -H 'Accept: application/octet-stream' "repos/MetaCubeX/mihomo/releases/assets/$asset_id" > "$temporary/mihomo.gz"
  else
    curl --fail --location --proto '=https' --proto-redir '=https' --tlsv1.2 --retry 3 --connect-timeout 10 --max-time 30 \
      'https://api.github.com/repos/MetaCubeX/mihomo/releases/tags/Prerelease-Alpha' -o "$temporary/release.json"
    asset_id="$(jq -r --arg wanted "$asset" '[.assets[] | select(.name == $wanted) | .id][0] // empty' "$temporary/release.json")"
    [[ "$asset_id" =~ ^[0-9]+$ ]] || { printf 'Mihomo Alpha asset not found: %s\n' "$asset" >&2; exit 1; }
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
GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.10}" bash ./scripts/go-test-sing-box.sh -mod=readonly ./sub \
  -run '^(TestSnellV6SharedPSKSubscriptions|TestSnellV6SubscriptionsPreserveAllModes)$' -count=1
"$NOVAPANEL_MIHOMO_TEST_BINARY" -d "$temporary/home" -t -f "$NOVAPANEL_MIHOMO_SNELL_CONFIG"

GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.10}" \
  bash ./scripts/go-test-sing-box.sh -mod=readonly ./core -run '^(TestSnellV6LoopbackIntegration|TestMihomoSnellV6SharedPSKLoopback)$' \
  -count=1 -timeout=90s -v
