#!/bin/bash
set -e

REPO="syawalqi/ORYX-AI"
BIN_NAME="oryx"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="$HOME/.config/oryx"

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

# Parse flags
TRACK="stable"
VERSION=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --dev) TRACK="dev"; shift ;;
        --tag) VERSION="$2"; TRACK="pinned"; shift 2 ;;
        --tag=*) VERSION="${1#--tag=}"; TRACK="pinned"; shift ;;
        *) shift ;;
    esac
done

# Determine download URL based on track
if [ "$TRACK" = "pinned" ] && [ -n "$VERSION" ]; then
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
elif [ "$TRACK" = "dev" ]; then
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/latest/${ASSET}"
    VERSION="latest (dev)"
else
    # Stable: use releases/latest (GitHub returns newest non-prerelease)
    DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"
    VERSION="stable"
fi

echo "📦 Downloading ORYX ${VERSION} for ${OS}/${ARCH}..."

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

# Save install track for future updates
mkdir -p "${CONFIG_DIR}"
echo "${TRACK}" > "${CONFIG_DIR}/update-track"

echo "✅ ORYX installed to ${INSTALL_DIR}/${BIN_NAME}"
echo "   Track: ${TRACK}"
echo ""
echo "Run 'oryx setup' to configure your API key, then 'oryx' to start chatting."
echo "To self-update later: oryx --update"
