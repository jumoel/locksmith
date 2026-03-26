# Test runner for locksmith integration tests.
# Uses multi-stage builds to support old npm versions that need older Node.js.

# Stage 1: Old npm versions that need Node 8
FROM node:8-slim AS old-npm
RUN npm install -g npm@1 --prefix /opt/npm1 2>/dev/null || true
RUN npm install -g npm@2 --prefix /opt/npm2
RUN npm install -g npm@3 --prefix /opt/npm3
RUN npm install -g npm@4 --prefix /opt/npm4
RUN npm install -g npm@5 --prefix /opt/npm5

# Stage 2: Main image with modern Node for npm 6+
FROM node:22-slim AS base

# Copy old npm versions from Stage 1
COPY --from=old-npm /opt/npm1 /opt/npm1
COPY --from=old-npm /opt/npm2 /opt/npm2
COPY --from=old-npm /opt/npm3 /opt/npm3
COPY --from=old-npm /opt/npm4 /opt/npm4
COPY --from=old-npm /opt/npm5 /opt/npm5
# Also copy Node 8 binary for running old npm versions
COPY --from=old-npm /usr/local/bin/node /opt/node8/bin/node
COPY --from=old-npm /usr/local/lib/node_modules /opt/node8/lib/node_modules

# Install npm versions 6-10 (node 22 ships with npm 11).
RUN npm install -g npm@6 --prefix /opt/npm6
RUN npm install -g npm@7 --prefix /opt/npm7
RUN npm install -g npm@8 --prefix /opt/npm8
RUN npm install -g npm@9 --prefix /opt/npm9
RUN npm install -g npm@10 --prefix /opt/npm10

# Install pnpm versions.
RUN npm install -g pnpm@5 --prefix /opt/pnpm5
RUN npm install -g pnpm@7 --prefix /opt/pnpm7
RUN npm install -g pnpm@8 --prefix /opt/pnpm8
RUN npm install -g pnpm@9 --prefix /opt/pnpm9
RUN npm install -g pnpm@10 --prefix /opt/pnpm10

# Install yarn classic (v1).
RUN npm install -g yarn@1 --prefix /opt/yarn1

# Yarn berry: install v2, v3, and v4 using separate corepack homes.
RUN corepack enable
RUN COREPACK_HOME=/opt/corepack-yarn2 corepack prepare yarn@2.4.3 --activate
RUN COREPACK_HOME=/opt/corepack-yarn31 corepack prepare yarn@3.1.0 --activate
RUN COREPACK_HOME=/opt/corepack-yarn3 corepack prepare yarn@3.8.7 --activate
RUN COREPACK_HOME=/opt/corepack-yarn4 corepack prepare yarn@4.6.0 --activate

# Install pnpm v4 for legacy lockfile testing.
RUN npm install -g pnpm@4 --prefix /opt/pnpm4

# pnpm@6 crashes on Node 22 - use @pnpm/exe which bundles its own Node.
RUN npm install -g @pnpm/exe@6 --prefix /opt/pnpm6

# Install bun and git.
RUN apt-get update && apt-get install -y curl unzip git && \
    curl -fsSL https://bun.sh/install | bash && \
    ln -s /root/.bun/bin/bun /usr/local/bin/bun && \
    apt-get clean && rm -rf /var/lib/apt/lists/*


# Helper scripts for version-switching.
COPY run-npm.sh /usr/local/bin/run-npm
COPY run-pnpm.sh /usr/local/bin/run-pnpm
COPY run-yarn.sh /usr/local/bin/run-yarn
RUN chmod +x /usr/local/bin/run-npm /usr/local/bin/run-pnpm /usr/local/bin/run-yarn

WORKDIR /workspace
