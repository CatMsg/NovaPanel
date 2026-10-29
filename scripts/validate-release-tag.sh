#!/usr/bin/env bash
set -euo pipefail

validate_release_tag() {
    if [[ $# -ne 3 ]]; then
        echo "usage: validate-release-tag.sh <requested-tag> <config-version> <git-ref>" >&2
        return 2
    fi

    local requested="$1"
    local version="$2"
    local git_ref="$3"
    local tag

    [[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
        echo "config/version must contain a stable semantic version" >&2
        return 1
    }
    [[ "$git_ref" == "refs/heads/main" ]] || {
        echo "releases may only be created from refs/heads/main" >&2
        return 1
    }

    if [[ -z "$requested" ]]; then
        tag="v$version"
    else
        [[ "$requested" =~ ^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
            echo "release tag must be a stable semantic version" >&2
            return 1
        }
        tag="v${requested#v}"
    fi

    [[ "$tag" == "v$version" ]] || {
        echo "release tag $tag must match config/version v$version" >&2
        return 1
    }

    printf '%s\n' "$tag"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    validate_release_tag "$@"
fi
