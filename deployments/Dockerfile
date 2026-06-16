# syntax=docker/dockerfile:1.7

# Builder stage
FROM golang:1.26 AS builder

WORKDIR /app
ENV CGO_ENABLED=0

ARG VERSION=dev

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build the v2 binary from cmd/releasegen.
COPY . .
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/release-gen ./cmd/releasegen

# Final stage
FROM alpine:latest
WORKDIR /app

# go-git is pure Go and does not shell out, so we do not need the git CLI at
# runtime. ca-certificates is required for the GitHub REST API calls.
RUN apk add --no-cache ca-certificates

COPY --from=builder /out/release-gen /usr/local/bin/release-gen

ENTRYPOINT ["/usr/local/bin/release-gen"]
