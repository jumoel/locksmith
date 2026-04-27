#!/bin/bash
# Usage: run-bun <version> <command...>
# Example: run-bun 1.2 install --frozen-lockfile
VERSION=$1
shift
case "$VERSION" in
    1.2) /opt/bun12/bin/bun "$@" ;;
    1.3) /opt/bun13/bin/bun "$@" ;;
    *)   echo "Unknown bun version: $VERSION" >&2; exit 1 ;;
esac
