#!/bin/bash
# Usage: run-pnpm <version> <command...>
# Example: run-pnpm 9 install --frozen-lockfile
VERSION=$1
shift
/opt/pnpm${VERSION}/bin/pnpm "$@"
