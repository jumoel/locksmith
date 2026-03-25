#!/bin/bash
# Usage: run-yarn <version> <command...>
# Example: run-yarn 1 install --frozen-lockfile
VERSION=$1
shift
case "$VERSION" in
    1)
        /opt/yarn1/bin/yarn "$@"
        ;;
    2)
        COREPACK_HOME=/opt/corepack-yarn2 corepack yarn "$@"
        ;;
    3)
        COREPACK_HOME=/opt/corepack-yarn3 corepack yarn "$@"
        ;;
    4)
        COREPACK_HOME=/opt/corepack-yarn4 corepack yarn "$@"
        ;;
esac
