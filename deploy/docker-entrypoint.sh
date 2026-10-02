#!/bin/sh
# Rewrites the API base URL baked into index.html at container start, so
# the same built image can point at whatever backend Service name the
# chart gives it, set via the API_BASE environment variable.
set -eu
if [ -n "${API_BASE:-}" ]; then
  sed -i "s#window.__API_BASE__ = window.__API_BASE__ || \"[^\"]*\"#window.__API_BASE__ = window.__API_BASE__ || \"${API_BASE}\"#" /usr/share/nginx/html/index.html
fi
