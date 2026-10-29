#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 4 ]]; then
    echo "usage: $0 <platform> <expected-file-architecture> <novas-binary> <mita-binary>" >&2
    exit 2
fi

platform="$1"
expected_machine="$2"
shift 2

for binary in "$@"; do
    [[ -f "$binary" && -x "$binary" ]] || {
        echo "$platform: missing executable: $binary" >&2
        exit 1
    }

    description="$(file -b "$binary")"
    [[ "$description" == *"$expected_machine"* ]] || {
        echo "$platform: $binary is '$description', expected architecture '$expected_machine'" >&2
        exit 1
    }
    if [[ "$description" != *"statically linked"* ]]; then
        echo "$platform: $binary is dynamically linked; release binaries must be static" >&2
        exit 1
    fi
done

printf '%s: both release binaries are executable, static ELF files for %s.\n' "$platform" "$expected_machine"
