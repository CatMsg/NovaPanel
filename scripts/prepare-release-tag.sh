#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 4 ]]; then
    echo "usage: $0 <tag-input> <version> <git-ref> <expected-commit>" >&2
    exit 2
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tag="$(bash "$root/scripts/validate-release-tag.sh" "$1" "$2" "$3")"
expected_commit="$4"
if [[ ! "$expected_commit" =~ ^([0-9a-f]{40}|[0-9a-f]{64})$ ]]; then
    echo "expected commit must be a full lowercase Git object ID" >&2
    exit 2
fi

lookup_releases() {
    local pages
    pages="$(gh api --paginate "repos/$GITHUB_REPOSITORY/releases?per_page=100")"
    jq -sc --arg tag "$tag" '[.[][] | select(.tag_name == $tag)]' <<<"$pages"
}

if refs="$(git ls-remote --exit-code origin "refs/tags/$tag" "refs/tags/$tag^{}" 2>&1)"; then
    existing_commit="$(awk -v tag="$tag" \
        '$2 == "refs/tags/" tag "^{}" { peeled = $1 }
         $2 == "refs/tags/" tag { direct = $1 }
         END { if (peeled != "") print peeled; else print direct }' <<<"$refs")"
    if [[ -z "$existing_commit" ]]; then
        echo "Could not resolve existing release tag: $tag" >&2
        exit 1
    fi
    if [[ "$existing_commit" != "$expected_commit" ]]; then
        echo "Existing release tag points to a different commit: $tag" >&2
        exit 1
    fi
else
    status=$?
    if [[ "$status" -ne 2 ]]; then
        echo "Unable to verify whether release tag exists: $refs" >&2
        exit "$status"
    fi
fi

# Draft releases for new tags do not create a Git tag ref until they are
# published. List releases instead of GET /releases/tags/{tag}, which returns
# 404 for that valid draft state.
if ! releases="$(lookup_releases 2>&1)"; then
    echo "Unable to inspect GitHub Releases: $releases" >&2
    exit 1
fi
release_count="$(jq 'length' <<<"$releases")"
if [[ "$release_count" -gt 1 ]]; then
    echo "Multiple GitHub Releases use the requested tag: $tag" >&2
    exit 1
fi

if [[ "$release_count" -eq 1 ]]; then
    release_json="$(jq -c '.[0]' <<<"$releases")"
    release_tag="$(jq -r '.tag_name // empty' <<<"$release_json")"
    is_draft="$(jq -r '.draft // false' <<<"$release_json")"
    release_commit="$(jq -r '.target_commitish // empty' <<<"$release_json")"
    if [[ "$release_tag" != "$tag" || "$is_draft" != "true" ]]; then
        echo "Existing GitHub Release is not an unpublished draft: $tag" >&2
        exit 1
    fi
    if [[ "$release_commit" != "$expected_commit" ]]; then
        echo "Existing draft release targets a different commit: $tag" >&2
        exit 1
    fi
    resume=true
elif [[ -n "${existing_commit:-}" ]]; then
    echo "Existing tag has no matching GitHub draft Release: $tag" >&2
    exit 1
else
    resume=false
fi

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    printf 'tag=%s\nresume=%s\n' "$tag" "$resume" >> "$GITHUB_OUTPUT"
else
    printf 'tag=%s\nresume=%s\n' "$tag" "$resume"
fi
