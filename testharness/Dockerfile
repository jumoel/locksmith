# Test runner for locksmith integration tests.
# Supports multiple package manager versions for compatibility testing across
# npm, pnpm, yarn, and bun.

FROM node:22-slim AS base

# Install npm versions (6 through 10; node 22 ships with npm 11).
# npm 6 may fail on newer Node.js - allow failure gracefully.
RUN npm install -g npm@6 --prefix /opt/npm6 2>/dev/null || true
RUN npm install -g npm@7 --prefix /opt/npm7
RUN npm install -g npm@8 --prefix /opt/npm8
RUN npm install -g npm@9 --prefix /opt/npm9
RUN npm install -g npm@10 --prefix /opt/npm10

# Install pnpm versions.
RUN npm install -g pnpm@7 --prefix /opt/pnpm7
RUN npm install -g pnpm@8 --prefix /opt/pnpm8
RUN npm install -g pnpm@9 --prefix /opt/pnpm9
RUN npm install -g pnpm@10 --prefix /opt/pnpm10

# Install yarn classic (v1).
RUN npm install -g yarn@1 --prefix /opt/yarn1

# Yarn berry (3 and 4) use corepack.
RUN corepack enable
RUN corepack prepare yarn@3.8.7 --activate

# Install bun.
RUN apt-get update && apt-get install -y curl unzip && \
    curl -fsSL https://bun.sh/install | bash && \
    ln -s /root/.bun/bin/bun /usr/local/bin/bun && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

# Helper scripts for version-switching.
COPY run-npm.sh /usr/local/bin/run-npm
COPY run-pnpm.sh /usr/local/bin/run-pnpm
COPY run-yarn.sh /usr/local/bin/run-yarn
RUN chmod +x /usr/local/bin/run-npm /usr/local/bin/run-pnpm /usr/local/bin/run-yarn

WORKDIR /workspace
