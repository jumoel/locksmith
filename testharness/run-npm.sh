#!/bin/bash
# Usage: run-npm <version> <command...>
# Example: run-npm 9 ci --ignore-scripts
VERSION=$1
shift
if [ "$VERSION" = "11" ]; then
    npm "$@"
else
    /opt/npm${VERSION}/bin/npm "$@"
fi
