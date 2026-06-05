# ============================================================
# Dockerfile: NAFAS Bot Go Backend
# Multi-stage build untuk size optimization
# ============================================================

# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files first (layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o nafas-bot ./cmd/api

# ============================================================
# Runtime stage (minimal image)
# ============================================================
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 appgroup && adduser -u 1000 -G appgroup -s /bin/sh -D appuser

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/nafas-bot .
COPY --from=builder /app/.env.example .env

# Set ownership
RUN chown -R appuser:appgroup /app

USER appuser

# Expose port
EXPOSE 8989

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8989/health || exit 1

# Run the application
CMD ["./nafas-bot"]
