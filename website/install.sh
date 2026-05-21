#!/usr/bin/env bash
# ⚡ Alpha Network CLI — One-Line Installer
# curl -sSL https://alphanetx.xyz/install.sh | bash
set -e

BASE_URL="https://alphanetx.xyz"
BIN_NAME="alpha"
INSTALL_DIR="/usr/local/bin"

# Colour
CYN='\033[0;36m'
NC='\033[0m'

echo ""
echo -e "${CYN}⚡ Alpha Network CLI Installer${NC}"
echo ""

# ── Detect OS/Arch ────────────────────────────────────────────────────────────
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l)        ARCH="arm" ;;
  *)
    echo "❌ Unsupported architecture: $ARCH"
    echo "   Build from source: git clone https://github.com/galaxiaalphanet/Alpha-Network.git"
    exit 1
    ;;
esac

case "$OS" in
  linux)   PLATFORM="linux" ;;
  darwin)  PLATFORM="darwin" ;;
  mingw*|msys*|cygwin*|windows*)
    PLATFORM="windows"
    BIN_NAME="alpha.exe"
    ;;
  *)
    echo "❌ Unsupported OS: $OS"
    echo "   Build from source: git clone https://github.com/galaxiaalphanet/Alpha-Network.git"
    exit 1
    ;;
esac

BIN_URL="${BASE_URL}/${BIN_NAME}-${PLATFORM}-${ARCH}"

echo "   Platform: ${PLATFORM}/${ARCH}"
echo "   Download: ${BIN_URL}"
echo ""

# ── Download ──────────────────────────────────────────────────────────────────
TMP_DIR=$(mktemp -d)
TMP_BIN="${TMP_DIR}/${BIN_NAME}"

if command -v curl &>/dev/null; then
  curl -sSL -o "$TMP_BIN" "$BIN_URL"
elif command -v wget &>/dev/null; then
  wget -q -O "$TMP_BIN" "$BIN_URL"
else
  echo "❌ curl or wget required. Install one and retry."
  exit 1
fi

chmod +x "$TMP_BIN"

# ── Install ───────────────────────────────────────────────────────────────────
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP_BIN" "${INSTALL_DIR}/${BIN_NAME}"
else
  sudo mv "$TMP_BIN" "${INSTALL_DIR}/${BIN_NAME}"
fi

rm -rf "$TMP_DIR"

echo -e "${CYN}✅ alpha installed to ${INSTALL_DIR}/${BIN_NAME}${NC}"
echo ""

# ── Verify ────────────────────────────────────────────────────────────────────
if command -v alpha &>/dev/null; then
  echo "   Try: alpha connect"
  alpha connect 2>/dev/null || echo "   (node unreachable — check your network)"
else
  echo "   ⚠️  ${INSTALL_DIR} may not be in PATH"
  echo "   Add to PATH or run: ${INSTALL_DIR}/${BIN_NAME} connect"
fi

echo ""
echo "   Docs:  https://alphanetx.xyz"
echo "   Repo:  https://github.com/galaxiaalphanet/Alpha-Network"
echo ""
