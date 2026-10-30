#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
validator="$root/scripts/validate-release-tag.sh"
prepare="$root/scripts/prepare-release-tag.sh"
workflow="$root/.github/workflows/release.yml"
ci_workflow="$root/.github/workflows/ci.yml"
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

job_needs() {
    local file="$1" job="$2"
    awk -v job="$job" '
        $0 == "  " job ":" { in_job = 1; next }
        in_job && /^  [[:alnum:]_-]+:/ { exit }
        in_job && /^    needs:/ { in_needs = 1; next }
        in_needs && /^    [[:alnum:]_-]+:/ { exit }
        in_needs { print }
    ' "$file"
}

release_needs="$(job_needs "$workflow" publish-release)"
docker_needs="$(job_needs "$ci_workflow" docker)"
if grep -Eiq 'windows' <<<"$release_needs$docker_needs"; then
    echo "Linux Release and Docker CI must not depend on Windows jobs" >&2
    exit 1
fi
for readme in "$root/README.md" "$root/README.en.md" "$root/README.ja.md" "$root/README.ko.md" "$root/README.ru.md" "$root/README.zh-TW.md"; do
    if grep -Fq 'raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh' "$readme"; then
        echo "installer bootstrap must not execute the mutable main branch: $readme" >&2
        exit 1
    fi
    if [[ "$(basename "$readme")" != README.md ]]; then
        grep -Fq '](README.md)' "$readme" || {
            echo "localized README must link to the canonical verified installer: $readme" >&2
            exit 1
        }
        continue
    fi
    install_example="$(awk '
        /^```bash$/ { capture = 1; block = ""; next }
        capture && /^```$/ { if (index(block, "bash \"$_novas_script\"")) { printf "%s", block; found = 1 }; capture = 0; next }
        capture { block = block $0 "\n" }
        END { if (!found) exit 1 }
    ' "$readme")" || {
        echo "canonical installer command is missing: $readme" >&2
        exit 1
    }
    for required in \
        'api.github.com/repos/CatMsg/NovaPanel/releases/latest' \
        'NovaPanel-install.sh.sigstore.json' \
        'if [[ -s "$_novas_bundle" ]]; then' \
        'attestation verify "$_novas_script"' \
        '--bundle "$_novas_bundle"' \
        '--source-ref refs/heads/main' \
        "_novas_tag\" != 'v1.6.149'" \
        '71a91cf3e7a9d18a2f803e272d46266b4100162f9bfcbc69991d626f94534583' \
        'bash -n "$_novas_script"' \
        'bash "$_novas_script" "$_novas_tag"'; do
        grep -Fq -- "$required" <<<"$install_example" || {
            echo "canonical installer bootstrap is missing '$required'" >&2
            exit 1
        }
    done
    verify_line="$(grep -nF 'attestation verify "$_novas_script"' <<<"$install_example" | head -n1 | cut -d: -f1)"
    legacy_digest_line="$(grep -nF 'sha256sum "$_novas_script"' <<<"$install_example" | head -n1 | cut -d: -f1)"
    syntax_line="$(grep -nF 'bash -n "$_novas_script"' <<<"$install_example" | head -n1 | cut -d: -f1)"
    execute_line="$(grep -nF 'bash "$_novas_script" "$_novas_tag"' <<<"$install_example" | head -n1 | cut -d: -f1)"
    if [[ -z "$verify_line" || -z "$legacy_digest_line" || -z "$syntax_line" || -z "$execute_line" ]] || ((verify_line >= syntax_line || legacy_digest_line >= syntax_line || syntax_line >= execute_line)) || ! bash -n <<<"$install_example"; then
        echo "installer must pass provenance and syntax checks before execution: $readme" >&2
        exit 1
    fi
done
if ! grep -Fq 'test-sing-box-race' <<<"$release_needs" || ! grep -Fq 'bash ./scripts/test-sing-box-race.sh' "$workflow"; then
    echo "Release must retain the Sing-Box race gate as a required parallel job" >&2
    exit 1
fi

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
for go_workflow in "$ci_workflow" "$workflow"; do
    setup_go_count="$(grep -Fc 'uses: actions/setup-go@' "$go_workflow")"
    cache_go_count="$(grep -Fc '          cache: true' "$go_workflow")"
    cache_path_count="$(grep -Fc '          cache-dependency-path: go.sum' "$go_workflow")"
    [[ "$setup_go_count" == "$cache_go_count" && "$setup_go_count" == "$cache_path_count" ]] || {
        echo "Every Go workflow job must use the go.sum-keyed setup-go cache: $go_workflow" >&2
        exit 1
    }
done

grep -Fq "git -C ~/cronet-go sparse-checkout set --no-cone '/*' '!/lib'" "$workflow" || {
    echo "Cronet release checkout must exclude unused prebuilt libraries" >&2
    exit 1
}
grep -Fq 'git -C ~/cronet-go fetch --depth=1 --filter=blob:none origin' "$workflow" || {
    echo "Cronet release checkout must use a partial blob fetch" >&2
    exit 1
}
if ! grep -A2 -F '      - name: 构建 cronet 库并设置工具链环境' "$workflow" | grep -Fq 'set -euo pipefail'; then
    echo "Cronet environment export pipeline must propagate upstream command failures" >&2
    exit 1
fi

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
grep -Fq 'gh release view "$TAG" --repo "$GITHUB_REPOSITORY" --json assets' "$workflow"
grep -Fq 'gh release edit "$TAG" --draft=false' "$workflow"
grep -Fq 'sha256sum "$asset"' "$workflow"
grep -Fq "'.assets[] | select(.name == \$name) | .digest // empty'" "$workflow"
grep -Fq 'actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6 # v4' "$workflow"
grep -Fq 'id: attestation' "$workflow"
grep -Fq 'steps.attestation.outputs.bundle-path' "$workflow"
grep -Fq 'NovaPanel-linux-${PLATFORM}.tar.gz.sigstore.json' "$workflow"
grep -Fq 'subject-path: install.sh' "$workflow"
grep -Fq 'subject-path: novas.sh' "$workflow"
grep -Fq 'NovaPanel-install.sh.sigstore.json' "$workflow"
grep -Fq 'NovaPanel-novas.sh.sigstore.json' "$workflow"
grep -Fq 'gh attestation verify "$source"' "$workflow"
grep -Fq 'bundle="${archive}.sigstore.json"' "$workflow"
grep -Fq -- '--bundle "$bundle"' "$workflow"
grep -Fq '      id-token: write' "$workflow"
grep -Fq '      attestations: write' "$workflow"
grep -Fq '      attestations: read' "$workflow"
grep -Fq 'gh attestation verify "$archive"' "$workflow"
grep -Fq -- '--signer-workflow "$GITHUB_REPOSITORY/.github/workflows/release.yml"' "$workflow"
grep -Fq -- '--source-ref "$GITHUB_REF"' "$workflow"
grep -Fq -- '--source-digest "$GITHUB_SHA"' "$workflow"
grep -Fq -- '--deny-self-hosted-runners' "$workflow"
grep -Fq 'Existing release tag points to a different commit' "$prepare"
grep -Fq 'releases?per_page=100' "$prepare"
if grep -Eq 'gh api .*releases/tags/' "$prepare" "$workflow"; then
    echo "release resume and draft asset verification must not use the tag-only release endpoint" >&2
    exit 1
fi
if grep -Eq -- '--clobber|gh release edit .*--title' "$workflow"; then
    echo "release workflow must not overwrite existing releases or assets" >&2
    exit 1
fi

printf 'Release policy checks passed for %s.\n' "$version"
