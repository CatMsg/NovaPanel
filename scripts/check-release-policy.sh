#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
validator="$root/scripts/validate-release-tag.sh"
prepare="$root/scripts/prepare-release-tag.sh"
workflow="$root/.github/workflows/release.yml"
version="$(tr -d '[:space:]' < "$root/config/version")"

expect_tag() {
    local expected="$1"
    shift
    local actual
    actual="$(bash "$validator" "$@")"
    [[ "$actual" == "$expected" ]] || {
        echo "expected tag $expected, got $actual" >&2
        exit 1
    }
}

expect_rejection() {
    if bash "$validator" "$@" >/dev/null 2>&1; then
        echo "release tag validator accepted invalid input: $*" >&2
        exit 1
    fi
}

expect_tag "v$version" "" "$version" "refs/heads/main"
expect_tag "v$version" "v$version" "$version" "refs/heads/main"
expect_tag "v$version" "${version}" "$version" "refs/heads/main"
expect_rejection "v9.9.9" "$version" "refs/heads/main"
expect_rejection 'v1.2.3;touch /tmp/release-injection' "$version" "refs/heads/main"
expect_rejection "v1.02.3" "$version" "refs/heads/main"
expect_rejection "" "$version" "refs/heads/release-test"
expect_rejection "" "v$version" "refs/heads/main"

bash "$root/scripts/test-release-resume.sh"
bash "$root/scripts/check-linux-architecture-contract.sh"

grep -Fq 'group: novapanel-release' "$workflow"
grep -Fq 'cancel-in-progress: false' "$workflow"
grep -Fq 'bash ./scripts/prepare-release-tag.sh "$TAG_INPUT" "$VERSION" "$GITHUB_REF" "$GITHUB_SHA"' "$workflow"
if ! sed -n '/id: vars/,/run: |/p' "$workflow" | grep -Fq 'GH_TOKEN: ${{ github.token }}'; then
    echo "release tag recovery step must authenticate its GitHub API lookup" >&2
    exit 1
fi
grep -Fq 'gh release create "$TAG"' "$workflow"
grep -Fq -- '--draft' "$workflow"
grep -Fq "if: steps.vars.outputs.resume != 'true'" "$workflow"
grep -Fq 'gh release edit "$TAG" --draft=false' "$workflow"
grep -Fq 'sha256sum "$asset"' "$workflow"
grep -Fq "'.assets[] | select(.name == \$name) | .digest // empty'" "$workflow"
grep -Fq 'actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6 # v4' "$workflow"
grep -Fq '      id-token: write' "$workflow"
grep -Fq '      attestations: write' "$workflow"
grep -Fq '      attestations: read' "$workflow"
grep -Fq 'gh attestation verify "$archive"' "$workflow"
grep -Fq -- '--signer-workflow "$GITHUB_REPOSITORY/.github/workflows/release.yml"' "$workflow"
grep -Fq -- '--source-ref "$GITHUB_REF"' "$workflow"
grep -Fq -- '--source-digest "$GITHUB_SHA"' "$workflow"
grep -Fq -- '--deny-self-hosted-runners' "$workflow"
grep -Fq 'Existing release tag points to a different commit' "$prepare"
if grep -Eq -- '--clobber|gh release edit .*--title' "$workflow"; then
    echo "release workflow must not overwrite existing releases or assets" >&2
    exit 1
fi

printf 'Release policy checks passed for %s.\n' "$version"
