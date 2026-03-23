#!/bin/bash
# Usage: run-npm <version> <command...>
# Example: run-npm 9 ci
VERSION=$1
shift
/opt/npm${VERSION}/bin/npm "$@"
