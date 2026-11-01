FROM golang:1.27-alpine AS builder

# make/g++ to build unrar, which Alpine doesn't package. Grabarr itself is
# pure Go (modernc sqlite), so it builds with CGO off.
RUN apk add --no-cache make g++ curl && \
    curl -fL -o /tmp/unrarsrc.tar.gz https://www.rarlab.com/rar/unrarsrc-7.1.3.tar.gz && \
    tar xzf /tmp/unrarsrc.tar.gz -C /tmp && \
    make -C /tmp/unrar -f makefile && \
    install /tmp/unrar/unrar /usr/local/bin/unrar

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" \
        -o /out/grabarr ./cmd/grabarr

# Grabarr shells out to rsync/ssh (copies, scans) and unrar/7z (extraction),
# so the runtime is Alpine with just those tools.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata rsync openssh-client 7zip libstdc++ && \
    echo "unraid:x:99:100:unraid:/home/unraid:/bin/sh" >> /etc/passwd && \
    mkdir -p /data /config /home/unraid && chown 99:100 /home/unraid

COPY --from=builder /usr/local/bin/unrar /usr/local/bin/unrar
WORKDIR /app
COPY --from=builder /out/grabarr ./grabarr
COPY web/static ./web/static

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ["/app/grabarr", "healthcheck"]
CMD ["/app/grabarr"]
