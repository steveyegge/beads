# Multi-stage build for bd daemon
# Builds a minimal container with just the bd binary

# Build stage
FROM golang:1.23-alpine AS builder

# Install git and build dependencies for CGO (including ICU for go-icu-regex)
RUN apk add --no-cache git gcc g++ musl-dev icu-dev

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Use GOTOOLCHAIN=auto to handle version requirements
ENV GOTOOLCHAIN=auto
RUN go mod download

# Copy source
COPY . .

# Build with CGO enabled for gozstd and go-icu-regex dependencies
ARG VERSION=dev
ARG BUILD_COMMIT=unknown
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.Build=${BUILD_COMMIT}" \
    -o bd ./cmd/bd

# Runtime stage - must match builder alpine version for ICU compatibility
FROM alpine:3.22

# Install ca-certificates for HTTPS, tzdata for timezone, icu-libs for runtime
RUN apk add --no-cache ca-certificates tzdata icu-libs netcat-openbsd

# Create non-root user
RUN addgroup -g 1000 beads && \
    adduser -u 1000 -G beads -s /bin/sh -D beads

# Copy binary from builder
COPY --from=builder /build/bd /usr/local/bin/bd

# Set ownership
RUN chown beads:beads /usr/local/bin/bd

# Switch to non-root user
USER beads

# Default working directory
WORKDIR /home/beads

# Expose TCP daemon port (9876) and HTTP port (9877)
EXPOSE 9876 9877

# Health check via HTTP endpoint
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD nc -z localhost 9877 || exit 1

# Default command - start daemon in foreground with TCP and HTTP listeners
ENTRYPOINT ["bd"]
CMD ["daemon", "start", "--foreground", "--tcp-addr=:9876", "--http-addr=:9877", "--log-json"]
