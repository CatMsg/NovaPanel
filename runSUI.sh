#!/bin/sh
set -e

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
cd "$SCRIPT_DIR"

sh "$SCRIPT_DIR/build.sh"
SUI_DB_FOLDER="db" SUI_DEBUG=true ./sui
