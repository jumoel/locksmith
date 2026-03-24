#!/bin/bash
# Usage: run-npm <version> <command...>
# Example: run-npm 9 ci --ignore-scripts
VERSION=$1
shift

case "$VERSION" in
    11)
        npm "$@"
        ;;
    1|2|3|4|5)
        # Old npm versions need Node 8 to run
        /opt/node8/bin/node /opt/npm${VERSION}/lib/node_modules/npm/bin/npm-cli.js "$@"
        ;;
    *)
        /opt/npm${VERSION}/bin/npm "$@"
        ;;
esac
