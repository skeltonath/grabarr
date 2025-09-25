#!/bin/sh

# Generate rclone config from environment variables to a writable location
mkdir -p /tmp/rclone-config

cat > /tmp/rclone-config/rclone.conf << EOF
[seedbox]
type = ftp
host = ${RCLONE_SEEDBOX_HOST}
user = ${RCLONE_SEEDBOX_USER}
pass = ${RCLONE_SEEDBOX_PASS}
explicit_tls = true
no_check_certificate = true
concurrency = 4
EOF

# Update the config to use the generated rclone config
export RCLONE_CONFIG=/tmp/rclone-config/rclone.conf

# Start the main application
exec "$@"