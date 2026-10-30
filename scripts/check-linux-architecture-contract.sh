#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workflow="$root/.github/workflows/release.yml"
ci_workflow="$root/.github/workflows/ci.yml"
installer="$root/install.sh"
docker_test="$root/docker-build-test.sh"
dockerfile="$root/Dockerfile"

release_arches="$(grep -E '^[[:space:]]*- platform:' "$workflow" | awk '{print $3}' | sort -u)"
platform_count="$(grep -Ec '^[[:space:]]*- platform:' "$workflow")"
docker_platform="$(sed -n 's/^PLATFORM="\([^"]*\)"/\1/p' "$docker_test")"
docker_arch_gate_count="$(grep -Fc 'RUN test "$TARGETARCH" = "amd64"' "$dockerfile")"
docker_front_arch_gate_line="$(grep -nF 'RUN test "$TARGETARCH" = "amd64"' "$dockerfile" | head -n1 | cut -d: -f1)"
docker_backend_arch_gate_line="$(grep -nF 'RUN test "$TARGETARCH" = "amd64"' "$dockerfile" | tail -n1 | cut -d: -f1)"
docker_frontend_install_line="$(grep -nF 'RUN npm ci && npm run build' "$dockerfile" | cut -d: -f1)"
docker_package_install_line="$(grep -nF 'RUN apk update && apk add --no-cache' "$dockerfile" | cut -d: -f1)"

[[ "$release_arches" == "amd64" ]] || {
    echo "Linux release matrix must build amd64 only; found: $release_arches" >&2
    exit 1
}
[[ "$platform_count" == "1" ]] || {
    echo "Linux release matrix must contain exactly one platform; found: $platform_count" >&2
    exit 1
}
grep -Fq "x86_64 | x64 | amd64) printf 'amd64\\n' ;;" "$installer"
grep -Fq '当前仅支持 Linux amd64' "$installer"
grep -Fq 'return 1' <(sed -n '/^arch() {/,/^}/p' "$installer")
if grep -Fq 'rm -f install.sh' "$installer"; then
    echo "Unsupported architectures must not delete the installer script" >&2
    exit 1
fi
matrix="$(sed -n '/^[[:space:]]*include:/,/^[[:space:]]*runs-on:/p' "$workflow")"
grep -Fq 'arch: amd64' <<<"$matrix"
grep -Fq 'file_arch: x86-64' <<<"$matrix"
grep -Fq 'with_naive_outbound,with_musl' "$workflow"
if grep -Eq 'goarm|GOARM|TARGETVARIANT|matrix\.naive' "$workflow" "$dockerfile"; then
    echo "Linux amd64 build must not retain unused ARM or conditional-target settings" >&2
    exit 1
fi
grep -Fq 'NovaPanel-linux-${{ matrix.platform }}.tar.gz' "$workflow"
grep -Fq 'NovaPanel-linux-${platform_arch}.tar.gz' "$installer"
grep -Fq 'set -euo pipefail' "$docker_test"
[[ "$docker_platform" == "linux/amd64" ]] || {
    echo "Docker build check must target linux/amd64 only; found: $docker_platform" >&2
    exit 1
}
grep -Fq 'COPY third_party/sing-box/.github/CRONET_GO_VERSION /tmp/CRONET_GO_VERSION' "$dockerfile"
grep -Fq 'git -C /tmp/cronet-go-lib fetch --depth=1 --filter=blob:none origin "$CRONET_GO_VERSION"' "$dockerfile"
grep -Fq -- '-o novas .' "$dockerfile"
grep -Fq 'docker build --progress=plain --platform linux/arm64 --target front-builder -f Dockerfile .' "$ci_workflow"
grep -Fq 'ARM64 Docker build failed before reaching the architecture guard' "$ci_workflow"
grep -Fq "RUN npm ci && npm run build' \"\$output\"" "$ci_workflow"
[[ "$docker_arch_gate_count" == "2" && -n "$docker_front_arch_gate_line" && -n "$docker_backend_arch_gate_line" && -n "$docker_frontend_install_line" && -n "$docker_package_install_line" && "$docker_front_arch_gate_line" -lt "$docker_frontend_install_line" && "$docker_backend_arch_gate_line" -lt "$docker_package_install_line" ]] || {
    echo "Dockerfile must reject unsupported targets before frontend and backend dependency installation" >&2
    exit 1
}
if grep -Fq 'releases/latest/download/libcronet' "$dockerfile"; then
    echo "Docker build must fetch Cronet from the pinned source commit" >&2
    exit 1
fi

printf 'Linux installer, release, and Docker test are amd64-only; Docker Cronet is pinned to source.\n'
