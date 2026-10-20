# ════════════════════════════════════════════════════════════
#  Arch ERP — Multi-stage Containerfile
#  Stage 1: Build the Go binary
#  Stage 2: Minimal Alpine runtime image
# ════════════════════════════════════════════════════════════

FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Cache dependency downloads separately from source
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o arch_erp ./main.go

# ────────────────────────────────────────────────────────────
FROM alpine:3.20 AS runtime

WORKDIR /app

# Runtime certificates and timezone data
RUN apk add --no-cache ca-certificates tzdata

# Copy binary and assets from builder
COPY --from=builder /app/arch_erp .
COPY --from=builder /app/static            ./static
COPY --from=builder /app/internal/templates ./internal/templates

# Non-root user for security
RUN addgroup -S arch && adduser -S arch -G arch
USER arch

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/login || exit 1

CMD ["./arch_erp"]
