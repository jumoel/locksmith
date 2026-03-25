#!/bin/bash
set -euo pipefail

# Usage: ./scripts/release.sh [major|minor|patch]
# Defaults to patch if no argument given.

BUMP="${1:-patch}"

# Get the latest tag, default to v0.0.0 if none exists.
LATEST=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
IFS='.' read -r MAJOR MINOR PATCH <<< "${LATEST#v}"

case "$BUMP" in
    major)
        MAJOR=$((MAJOR + 1))
        MINOR=0
        PATCH=0
        ;;
    minor)
        MINOR=$((MINOR + 1))
        PATCH=0
        ;;
    patch)
        PATCH=$((PATCH + 1))
        ;;
    *)
        echo "Usage: $0 [major|minor|patch]"
        exit 1
        ;;
esac

NEW_VERSION="v${MAJOR}.${MINOR}.${PATCH}"

echo "Current version: ${LATEST}"
echo "New version:     ${NEW_VERSION}"
echo ""
read -p "Create and push tag ${NEW_VERSION}? [y/N] " -n 1 -r
echo ""

if [[ $REPLY =~ ^[Yy]$ ]]; then
    git tag -a "${NEW_VERSION}" -m "Release ${NEW_VERSION}"
    git push origin "${NEW_VERSION}"
    echo ""
    echo "Tag ${NEW_VERSION} pushed. GitHub Actions will build the release."
    echo "Watch progress: https://github.com/jumoel/locksmith/actions"
else
    echo "Aborted."
fi
