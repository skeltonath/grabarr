FROM golang:1.23-alpine

# Install dependencies
RUN apk add --no-cache git ca-certificates tzdata gcc musl-dev sqlite-dev rclone rsync sshpass openssh-client curl 7zip make g++ && \
    curl -L -o /tmp/unrarsrc.tar.gz https://www.rarlab.com/rar/unrarsrc-7.1.3.tar.gz && \
    tar xzf /tmp/unrarsrc.tar.gz -C /tmp && \
    cd /tmp/unrar && make -f makefile && install unrar /usr/local/bin/unrar && \
    rm -rf /tmp/unrar /tmp/unrarsrc.tar.gz

# Create user for UID 99 (Unraid nobody)
RUN echo "unraid:x:99:100:unraid:/home/unraid:/bin/sh" >> /etc/passwd && \
    mkdir -p /data /config /app /home/unraid

# Set working directory
WORKDIR /app

# Copy everything and build
COPY . .
RUN go mod download && \
    CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o grabarr ./cmd/grabarr

# Set permissions
RUN chmod +x grabarr docker-entrypoint.sh && \
    chown -R 99:100 /app /config

# Expose ports
EXPOSE 8080 5572

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/api/v1/health || exit 1

# Run the startup script
CMD ["./docker-entrypoint.sh"]