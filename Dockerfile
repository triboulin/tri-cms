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

COPY --from=builder /out/data /data

COPY --from=builder /out/tricms /tricms

ENV TRICMS_ADDR=:8080 \
    TRICMS_DATA_DIR=/data

EXPOSE 8080
VOLUME ["/data"]

# Runs as root (scratch has no /etc/passwd): Kubernetes CSI drivers
# (Longhorn, etc.) mount PVCs root-owned, and a non-root uid can't write
# to /data without cluster-side fsGroup config -- root avoids that
# dependency entirely.

ENTRYPOINT ["/tricms"]
