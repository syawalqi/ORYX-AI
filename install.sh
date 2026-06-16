#!/bin/bash
set -e

REPO="syawalqi/ORYX-AI"
BIN_NAME="oryx"
INSTALL_DIR="/usr/local/bin"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "❌ Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
    linux|darwin) ;;
    *) echo "❌ Unsupported OS: $OS"; exit 1 ;;
esac

ASSET="oryx-${OS}-${ARCH}"

# Auto-detect latest version from GitHub API
echo "🔍 Checking latest version..."
VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')

if [ -z "$VERSION" ]; then
    echo "⚠️  Could not detect latest version, using v1.2.0"
    VERSION="v1.2.0"
fi

echo "📦 Downloading ORYX ${VERSION} for ${OS}/${ARCH}..."

# Download the binary from GitHub releases
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
HTTP_CODE=$(curl -fsSL -o "/tmp/${ASSET}" -w "%{http_code}" "${DOWNLOAD_URL}" 2>/dev/null || echo "000")

if [ "$HTTP_CODE" != "200" ]; then
    echo "❌ Download failed (HTTP ${HTTP_CODE})"
    echo "   URL: ${DOWNLOAD_URL}"
    echo ""
    echo "   Available releases: https://github.com/${REPO}/releases"
    exit 1
fi

# Make executable and install
chmod +x "/tmp/${ASSET}"
sudo mv "/tmp/${ASSET}" "${INSTALL_DIR}/${BIN_NAME}"

echo "✅ ORYX ${VERSION} installed to ${INSTALL_DIR}/${BIN_NAME}"
echo ""
echo "Run 'oryx setup' to configure your API key, then 'oryx' to start chatting."
