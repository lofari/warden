#!/bin/bash
set -euo pipefail
GO_VERSION="1.23.6"
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xzf -
echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile.d/go.sh
export PATH=$PATH:/usr/local/go/bin
