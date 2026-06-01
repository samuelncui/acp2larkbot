#!/usr/bin/env bash
set -euo pipefail

# acp2larkbot installer
# Usage: curl -fsSL https://raw.githubusercontent.com/samuelncui/acp2larkbot/main/install.sh | bash
#
# Set BINDIR to override install path (default: /usr/local/bin)
# Set VERSION to pin a specific version (default: latest)

REPO="samuelncui/acp2larkbot"
BINDIR="${BINDIR:-/usr/local/bin}"
VERSION="${VERSION:-latest}"

# --- detect platform ---
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
    linux)   GOOS="Linux" ;;
    darwin)  GOOS="Darwin" ;;
    *)
        echo "Unsupported OS: $OS" >&2
        exit 1
        ;;
esac

case "$ARCH" in
    x86_64|amd64) ARCH_NAME="x86_64" ;;
    aarch64|arm64) ARCH_NAME="arm64" ;;
    *)
        echo "Unsupported arch: $ARCH" >&2
        exit 1
        ;;
esac

ARCHIVE="acp2larkbot_${GOOS}_${ARCH_NAME}.tar.gz"

# --- resolve version ---
if [ "$VERSION" = "latest" ]; then
    if command -v curl >/dev/null 2>&1; then
        VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    elif command -v wget >/dev/null 2>&1; then
        VERSION=$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    else
        echo "Need curl or wget to determine latest version" >&2
        exit 1
    fi
fi

if [ -z "$VERSION" ]; then
    echo "Could not determine version" >&2
    exit 1
fi

echo "→ Installing acp2larkbot ${VERSION} for ${GOOS}/${ARCH_NAME}"

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

cd "$TMPDIR"

# --- download ---
if command -v curl >/dev/null 2>&1; then
    curl -fsSLO "$DOWNLOAD_URL"
elif command -v wget >/dev/null 2>&1; then
    wget -q "$DOWNLOAD_URL"
else
    echo "Need curl or wget to download" >&2
    exit 1
fi

# --- extract ---
tar xzf "$ARCHIVE"

if [ ! -f acp2larkbot ]; then
    echo "Binary not found in archive" >&2
    exit 1
fi

# --- install ---
if [ ! -d "$BINDIR" ]; then
    mkdir -p "$BINDIR"
fi

if [ -w "$BINDIR" ]; then
    mv acp2larkbot "$BINDIR/"
else
    sudo mv acp2larkbot "$BINDIR/"
fi

chmod +x "$BINDIR/acp2larkbot"

echo "✓ acp2larkbot ${VERSION} installed to ${BINDIR}/acp2larkbot"
