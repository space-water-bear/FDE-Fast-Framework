FROM node:22.22.3-bookworm-slim AS node
FROM golang:1.26.0-bookworm
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && npm install --global pnpm@11.23.0 \
 && go version && node --version && pnpm --version
ENV CI=1 HOME=/tmp GOTOOLCHAIN=local GOENV=off
