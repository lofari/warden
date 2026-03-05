#!/bin/bash
set -euo pipefail
apt-get update && apt-get install -y python3 python3-pip python3-venv python3-dev
ln -sf /usr/bin/python3 /usr/bin/python
curl -LsSf https://astral.sh/uv/install.sh | sh
echo 'export PATH="/root/.local/bin:$PATH"' >> /etc/profile.d/uv.sh
