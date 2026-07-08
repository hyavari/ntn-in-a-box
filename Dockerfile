# Multi-stage build: Go compile → Alpine runtime with iproute2.
# Used by the macOS Docker proxy and for manual container deployment.

FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go build -o /out/ntnbox ./cmd/ntnbox/
RUN CGO_ENABLED=0 go build -o /out/poller ./cmd/poller/

# Runtime image.
FROM alpine:3.20

RUN apk add --no-cache iproute2 iptables curl

COPY --from=builder /out/ntnbox /usr/local/bin/ntnbox
COPY --from=builder /out/poller /usr/local/bin/poller
COPY testdata/profiles/ /profiles/

ENTRYPOINT ["ntnbox"]
