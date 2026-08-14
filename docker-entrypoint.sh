#!/bin/sh
set -e

# Keep yt-dlp current: YouTube changes its extractors frequently and stale
# versions are the leading cause of 403/bot-check failures. Update on every
# container start so fixes apply without a rebuild.
pip install --break-system-packages --no-cache-dir -U yt-dlp

exec /usr/local/bin/chatroom
