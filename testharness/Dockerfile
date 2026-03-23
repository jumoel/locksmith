# Test runner for locksmith integration tests.
# Supports multiple npm versions for compatibility testing.

FROM node:22-slim AS base
RUN corepack enable

# Install multiple npm versions globally so tests can switch between them.
# npm ci was introduced in npm 5.7.0, so we start from npm 6.
RUN npm install -g npm@6 --prefix /opt/npm6 && \
    npm install -g npm@7 --prefix /opt/npm7 && \
    npm install -g npm@8 --prefix /opt/npm8 && \
    npm install -g npm@9 --prefix /opt/npm9 && \
    npm install -g npm@10 --prefix /opt/npm10

# Install pnpm
RUN corepack prepare pnpm@9 --activate

# Create a script that runs a given npm version
COPY run-npm.sh /usr/local/bin/run-npm
RUN chmod +x /usr/local/bin/run-npm

WORKDIR /workspace
