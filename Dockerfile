# syntax=docker/dockerfile:1

# ---- Stage 1: build ---------------------------------------------------
# modernc.org/sqlite is a pure-Go SQLite driver (no cgo), so triCMS
# compiles into a fully static binary -- exactly what a FROM scratch
# runtime stage requires.
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN mkdir -p /out/data && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/tricms ./cmd/tricms

# ---- Stage 2: runtime ---------------------------------------------------
FROM scratch

# Root TLS certificates, needed for outbound HTTPS webhook deliveries
# (pkg/webhooks.Dispatcher) since scratch ships nothing by default.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Pre-owned empty data directory. When Docker creates a fresh (named or
# anonymous) volume at /data, it seeds it from this image layer, so the
# non-root user below can actually write system.db / project data into it.
COPY --from=builder --chown=65532:65532 /out/data /data

COPY --from=builder /out/tricms /tricms

ENV TRICMS_ADDR=:8080 \
    TRICMS_DATA_DIR=/data

EXPOSE 8080
VOLUME ["/data"]

# Distroless-style non-root numeric UID; scratch has no /etc/passwd, but
# Docker doesn't require one to run as a given uid:gid.
USER 65532:65532

ENTRYPOINT ["/tricms"]
