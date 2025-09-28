#!/bin/sh

# Start rclone daemon in the background
echo "Starting rclone daemon..."
rclone rcd --rc-addr 0.0.0.0:5572 --rc-no-auth --rc-web-gui --rc-web-gui-update --rc-web-gui-no-open-browser --cache-dir /data/rclone-cache --config /config/rclone.conf &
RCLONE_PID=$!

# Wait a bit for rclone daemon to start
sleep 2

# Check if rclone daemon is running
if ! kill -0 $RCLONE_PID 2>/dev/null; then
    echo "Failed to start rclone daemon"
    exit 1
fi

echo "rclone daemon started with PID $RCLONE_PID"

# Start the main application
echo "Starting grabarr..."
exec ./grabarr