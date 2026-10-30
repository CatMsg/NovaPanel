#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workflow="$root/.github/workflows/release.yml"
installer="$root/install.sh"
docker_test="$root/docker-build-test.sh"

installer_arches="$(sed -n '/^arch() {/,/^}/p' "$installer" | grep -oE "echo '[a-z0-9]+'" | sed -E "s/echo '([^']+)'/\1/" | sort -u)"
release_arches="$(grep -E '^[[:space:]]*- platform:' "$workflow" | awk '{print $3}' | sort -u)"
platform_count="$(grep -Ec '^[[:space:]]*- platform:' "$workflow")"
docker_platform="$(sed -n 's/^PLATFORM="\([^"]*\)"/\1/p' "$docker_test")"

[[ "$installer_arches" == "amd64" ]] || {
    echo "Linux installer must support amd64 only; found: $installer_arches" >&2
    exit 1
}
[[ "$release_arches" == "amd64" ]] || {
    echo "Linux release matrix must build amd64 only; found: $release_arches" >&2
    exit 1
}
[[ "$platform_count" == "1" ]] || {
    echo "Linux release matrix must contain exactly one platform; found: $platform_count" >&2
    exit 1
}
grep -Fq "x86_64 | x64 | amd64) echo 'amd64' ;;" "$installer"
matrix="$(sed -n '/^[[:space:]]*include:/,/^[[:space:]]*runs-on:/p' "$workflow")"
grep -Fq 'arch: amd64' <<<"$matrix"
grep -Fq 'goarm: ""' <<<"$matrix"
grep -Fq 'naive: true' <<<"$matrix"
grep -Fq 'file_arch: x86-64' <<<"$matrix"
grep -Fq 'NovaPanel-linux-${{ matrix.platform }}.tar.gz' "$workflow"
grep -Fq 'NovaPanel-linux-$(arch).tar.gz' "$installer"
grep -Fq 'set -euo pipefail' "$docker_test"
[[ "$docker_platform" == "linux/amd64" ]] || {
    echo "Docker build check must target linux/amd64 only; found: $docker_platform" >&2
    exit 1
}

printf 'Linux installer, release, and Docker test are consistently amd64-only.\n'
