#!/bin/bash
# Usage: run-pnpm <version> <command...>
# Example: run-pnpm 9 install --frozen-lockfile
VERSION=$1
shift
case "$VERSION" in
    6)
        # @pnpm/exe@6 installs the pnpm binary with bundled Node
        /opt/pnpm6/bin/pnpm "$@"
        ;;
    *)
        /opt/pnpm${VERSION}/bin/pnpm "$@"
        ;;
esac
