# Multi-stage build: Go compile → Alpine runtime with iproute2 + Node.
# Used by the macOS Docker proxy and for manual container deployment.
#
# Node/pnpm are included so macOS developers can run JS/TS apps under
# `ntnbox run` (host Darwin binaries cannot execute inside Linux containers).

FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go build -o /out/ntnbox ./cmd/ntnbox/
RUN CGO_ENABLED=0 go build -o /out/poller ./cmd/poller/

FROM node:22-alpine AS node

# Runtime image.
FROM alpine:3.20

RUN apk add --no-cache iproute2 iptables curl ca-certificates libstdc++

# Linux Node + corepack/pnpm (required for macOS Docker proxy JS workloads).
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -sf /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
	&& ln -sf /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
	&& ln -sf /usr/local/lib/node_modules/corepack/dist/corepack.js /usr/local/bin/corepack \
	&& corepack enable \
	&& corepack prepare pnpm@10.11.0 --activate

COPY --from=builder /out/ntnbox /usr/local/bin/ntnbox
COPY --from=builder /out/poller /usr/local/bin/poller
COPY testdata/profiles/ /profiles/

ENTRYPOINT ["ntnbox"]
