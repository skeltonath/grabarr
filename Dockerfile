# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata gcc musl-dev sqlite-dev

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
    sshpass \
    openssh-client \
    curl \
    && rm -rf /var/cache/apk/*

# Create user for UID 99 (Unraid nobody) and necessary directories
RUN echo "unraid:x:99:100:unraid:/home/unraid:/bin/sh" >> /etc/passwd && \
    mkdir -p /data /config /app /home/unraid && \
    chmod 755 /data /config /app

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/grabarr .

# Copy web UI files
COPY web/ ./web/

# Copy configuration template and setup script
COPY config.example.yaml /config/config.example.yaml
COPY scripts/setup-rclone.sh /setup-rclone.sh

# Set permissions for all files and change ownership to user 99
RUN chmod +x grabarr /setup-rclone.sh && \
    chown -R 99:100 /app

# Note: User will be set to 99:100 by docker-compose override

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/api/v1/health || exit 1

# Run setup script then the application
CMD ["/setup-rclone.sh", "./grabarr"]