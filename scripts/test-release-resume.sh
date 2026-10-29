#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin"

cat > "$tmp/bin/git" <<'MOCK_GIT'
#!/usr/bin/env bash
printf '%s\n' "${MOCK_GIT_OUTPUT:-}"
exit "${MOCK_GIT_STATUS:-2}"
MOCK_GIT

cat > "$tmp/bin/gh" <<'MOCK_GH'
#!/usr/bin/env bash
if [[ "${MOCK_GH_STATUS:-0}" -ne 0 ]]; then
    echo "mock gh failure" >&2
    exit "$MOCK_GH_STATUS"
fi
printf '%s\n' "${MOCK_RELEASE_JSON:-}"
MOCK_GH

chmod +x "$tmp/bin/git" "$tmp/bin/gh"
export PATH="$tmp/bin:$PATH"
export GITHUB_REPOSITORY=example/NovaPanel
export GITHUB_OUTPUT="$tmp/github-output"
commit=0123456789abcdef0123456789abcdef01234567
tag=v1.2.3

run_prepare() {
    : > "$GITHUB_OUTPUT"
    env "$@" bash "$root/scripts/prepare-release-tag.sh" "$tag" "${tag#v}" refs/heads/main "$commit"
}

run_prepare MOCK_GIT_STATUS=2 MOCK_GIT_OUTPUT= >/dev/null
grep -Fxq "tag=$tag" "$GITHUB_OUTPUT"
grep -Fxq 'resume=false' "$GITHUB_OUTPUT"

tag_refs="$commit refs/tags/$tag"
release='{"tag_name":"v1.2.3","draft":true}'
run_prepare MOCK_GIT_STATUS=0 "MOCK_GIT_OUTPUT=$tag_refs" "MOCK_RELEASE_JSON=$release" >/dev/null
grep -Fxq 'resume=true' "$GITHUB_OUTPUT"

annotated_refs="$(printf '%s\n' "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa refs/tags/$tag" "$commit refs/tags/$tag^{}")"
run_prepare MOCK_GIT_STATUS=0 "MOCK_GIT_OUTPUT=$annotated_refs" "MOCK_RELEASE_JSON=$release" >/dev/null
grep -Fxq 'resume=true' "$GITHUB_OUTPUT"

expect_rejection() {
    if run_prepare "$@" >/dev/null 2>&1; then
        echo "release resume validation accepted unsafe state: $*" >&2
        exit 1
    fi
}

expect_rejection MOCK_GIT_STATUS=0 "MOCK_GIT_OUTPUT=$tag_refs" 'MOCK_RELEASE_JSON={"tag_name":"v1.2.3","draft":false}'
expect_rejection MOCK_GIT_STATUS=0 "MOCK_GIT_OUTPUT=$tag_refs" 'MOCK_RELEASE_JSON={"tag_name":"v9.9.9","draft":true}'
expect_rejection MOCK_GIT_STATUS=0 "MOCK_GIT_OUTPUT=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa refs/tags/$tag" "MOCK_RELEASE_JSON=$release"
expect_rejection MOCK_GIT_STATUS=0 "MOCK_GIT_OUTPUT=$tag_refs" MOCK_GH_STATUS=1
expect_rejection MOCK_GIT_STATUS=128 MOCK_GIT_OUTPUT='network failure'

printf 'Release resume checks passed.\n'
