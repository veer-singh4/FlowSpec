#!/bin/sh
# UniFlow CLI Installation Script (Linux/macOS)
# Usage: curl -sSL https://raw.githubusercontent.com/veer-singh4/FlowSpec/main/install.sh | sh

set -e

OWNER="veer-singh4"
REPO="FlowSpec"
BINARY_NAME="flow"

# Detect OS and Architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case $ARCH in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "Detected OS: $OS, Arch: $ARCH"

# Get latest release tag from GitHub
TAG=$(curl -s "https://api.github.com/repos/$OWNER/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$TAG" ]; then
    echo "Error: Could not find latest release tag. Make sure you have created a release on GitHub."
    exit 1
fi

echo "Installing $REPO $TAG..."

# Construct download URL
# Example: FlowSpec_1.0.0_linux_amd64.tar.gz
URL="https://github.com/$OWNER/$REPO/releases/download/$TAG/${REPO}_${TAG#v}_${OS}_${ARCH}.tar.gz"

echo "Downloading $URL..."
TMP_DIR=$(mktemp -d)
curl -sSL "$URL" -o "$TMP_DIR/flow.tar.gz"

# Extract and install
tar -xzf "$TMP_DIR/flow.tar.gz" -C "$TMP_DIR"
chmod +x "$TMP_DIR/$BINARY_NAME"

INSTALL_DIR="/usr/local/bin"
echo "Moving binary to $INSTALL_DIR (may require sudo)..."
if [ -w "$INSTALL_DIR" ]; then
    mv "$TMP_DIR/$BINARY_NAME" "$INSTALL_DIR/"
else
    sudo mv "$TMP_DIR/$BINARY_NAME" "$INSTALL_DIR/"
fi

# Cleanup
rm -rf "$TMP_DIR"

echo "✓ Successfully installed UniFlow CLI ($BINARY_NAME) to $INSTALL_DIR"
echo "Run 'flow version' to verify."
