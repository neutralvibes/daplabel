#!/bin/sh
set -e

REPO="neutralvibes/daplabel"
BINARY_NAME="daplabel"

BOLD='\033[1m'
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0;0m'

info() { echo "${GREEN}${BOLD}::${NC} $1"; }
error() { echo "${RED}${BOLD}Error:${NC} $1" >&2; exit 1; }

# 1. Detect OS
OS_RAW="$(uname -s)"
case "${OS_RAW}" in
    Linux*)   OS="linux" ;;
    Darwin*)  OS="darwin" ;;
    MSYS*|MINGW*|CYGWIN*) OS="windows" ;;
    *)        error "Unsupported OS: ${OS_RAW}" ;;
esac

# 2. Detect Architecture
ARCH_RAW="$(uname -m)"
case "${ARCH_RAW}" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    armv7l|armv7)  ARCH="armv7" ;;
    *)             error "Unsupported Architecture: ${ARCH_RAW}" ;;
esac

info "Detected platform: ${OS}_${ARCH}"

# 3. Securely Fetch Latest Release Tag (Bypasses API rate limits)
info "Resolving latest release version..."
LATEST_TAG=$(curl -sS -I "https://github.com{REPO}/releases/latest" | grep -i '^location:' | sed -E 's/.*\/tag\/([^[:space:]\r\n]+).*/\1/')

if [ -z "${LATEST_TAG}" ]; then
    error "Failed to resolve the latest version tag from GitHub."
fi

info "Latest release found: ${LATEST_TAG}"

# 4. Resolve exact archive format
case "${OS}" in
    windows) EXT="zip" ;;
    *)       EXT="tar.gz" ;;
esac

TARBALL_NAME="${BINARY_NAME}_${OS}_${ARCH}.${EXT}"
DOWNLOAD_URL="https://github.com{REPO}/releases/download/${LATEST_TAG}/${TARBALL_NAME}"

# 5. Safe Temporary Extraction Environment
TMP_DIR=$(mktemp -d)
# Standard POSIX-compliant trap handling
trap 'rc=$?; rm -rf "${TMP_DIR}"; exit $rc' EXIT
trap 'exit 1' INT TERM

info "Downloading ${TARBALL_NAME}..."
curl -fsSL -o "${TMP_DIR}/${TARBALL_NAME}" "${DOWNLOAD_URL}" || error "Download failed. Asset may not exist for this platform."

info "Extracting archive..."
cd "${TMP_DIR}"
if [ "${EXT}" = "zip" ]; then
    unzip -q "${TARBALL_NAME}"
else
    tar -xzf "${TARBALL_NAME}"
fi

# 6. Smart Fallback Path Selection (No forced sudo crashes)
# Default to /usr/local/bin, fallback safely to $HOME/.local/bin if unprivileged
INSTALL_DIR="/usr/local/bin"
USE_SUDO=false

if [ ! -w "${INSTALL_DIR}" ]; then
    if command -v sudo >/dev/null 2>&1 && [ -t 0 ]; then
        USE_SUDO=true
    else
        INSTALL_DIR="${HOME}/.local/bin"
        info "/usr/local/bin is not writable and sudo is unavailable. Falling back to ${INSTALL_DIR}"
        mkdir -p "${INSTALL_DIR}"
    fi
fi

info "Installing ${BINARY_NAME} to ${INSTALL_DIR}..."
if [ "${USE_SUDO}" = true ]; then
    sudo mv "${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    [ "${OS}" != "windows" ] && sudo chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
else
    mv "${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    [ "${OS}" != "windows" ] && chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
fi

info "${BINARY_NAME} successfully installed!"
"${INSTALL_DIR}/${BINARY_NAME}" --version
