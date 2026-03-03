#!/bin/bash
set -euo pipefail
# Install Node.js if not already present
if ! command -v node &>/dev/null; then
  curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
  apt-get install -y nodejs
fi
npm install -g @anthropic-ai/claude-code
