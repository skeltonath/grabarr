# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o grabarr ./cmd/grabarr

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add \
    ca-certificates \
    tzdata \
    rclone \
    curl \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN addgroup -g 1000 grabarr && \
    adduser -D -s /bin/sh -u 1000 -G grabarr grabarr

# Create necessary directories
RUN mkdir -p /data /config /app && \
    chown -R grabarr:grabarr /data /config /app

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/grabarr .

# Copy configuration template
COPY config.example.yaml /config/config.example.yaml

# Set permissions
RUN chmod +x grabarr

# Switch to non-root user
USER grabarr

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/api/v1/health || exit 1

# Run the application
CMD ["./grabarr"]