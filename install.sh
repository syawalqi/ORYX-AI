#!/bin/bash
set -e

REPO="syawalqi/flare"
BIN_NAME="flare"
INSTALL_DIR="/usr/local/bin"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
    linux|darwin) ;;
    *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

ASSET="flare-${OS}-${ARCH}"
VERSION="v1.0.0"

echo "📦 Downloading FLARE ${VERSION} for ${OS}/${ARCH}..."

# Download the binary from GitHub releases
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
HTTP_CODE=$(curl -fsSL -o "/tmp/${ASSET}" -w "%{http_code}" "${DOWNLOAD_URL}" 2>/dev/null || echo "000")

if [ "$HTTP_CODE" != "200" ]; then
    echo "❌ Download failed (HTTP ${HTTP_CODE})"
    echo "   URL: ${DOWNLOAD_URL}"
    exit 1
fi

# Make executable and install
chmod +x "/tmp/${ASSET}"
sudo mv "/tmp/${ASSET}" "${INSTALL_DIR}/${BIN_NAME}"

echo "✅ FLARE ${VERSION} installed to ${INSTALL_DIR}/${BIN_NAME}"
echo ""
echo "Run 'flare setup' to configure your API key, then 'flare chat' to start."
