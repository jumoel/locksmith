#!/bin/bash
# Usage: run-yarn <version> <command...>
# Example: run-yarn 1 install --frozen-lockfile
VERSION=$1
shift
if [ "$VERSION" = "1" ]; then
    /opt/yarn1/bin/yarn "$@"
elif [ "$VERSION" = "3" ] || [ "$VERSION" = "4" ]; then
    corepack yarn "$@"
fi
