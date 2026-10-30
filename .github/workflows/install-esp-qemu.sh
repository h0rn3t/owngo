#!/bin/bash
set -euo pipefail

# Installation directory (from argument or default)
INSTALL_DIR="${1:-.cache/qemu}"

# Detect platform
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

# Map architecture names
case "$ARCH" in
  x86_64|amd64)
    ARCH="x86_64"
    ;;
  aarch64|arm64)
    ARCH="aarch64"
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

# Map OS names
case "$OS" in
  darwin)
    PLATFORM="${ARCH}-apple-darwin"
    ;;
  linux)
    PLATFORM="${ARCH}-linux-gnu"
    ;;
  *)
    echo "Unsupported OS: $OS"
    exit 1
    ;;
esac

RELEASE_TAG="esp-develop-9.2.2-20250817"
VERSION="esp_develop_9.2.2_20250817"
PACKAGES=(
  "qemu-riscv32-softmmu-${VERSION}-${PLATFORM}.tar.xz"
  "qemu-xtensa-softmmu-${VERSION}-${PLATFORM}.tar.xz"
)

# SHA-256 values published with the pinned Espressif QEMU release. Keep the
# order aligned with PACKAGES (riscv32, then xtensa).
case "$PLATFORM" in
  aarch64-apple-darwin)
    SHA256S=(
      "234690b6fa7c1d5dfe3dbb2bdd0c2810755e7c98999a9f21c389a6046b7eb76d"
      "aa92e337461d482f5d9f31cd8efc0bd67b3de8fcfcfb567289cb43a59c184651"
    )
    ;;
  aarch64-linux-gnu)
    SHA256S=(
      "f907a54313058f8a9681d2f48257d518950ff98bcd5a319194b4bee7c10cf223"
      "317f6e0fd1dba0886d8110709823d909593ef29438822a14f81ebe19d72ce7cd"
    )
    ;;
  x86_64-apple-darwin)
    SHA256S=(
      "820028ee7cd2dd8fe8cd8ca5519ab6e792d15fea9367c4525cf63c0f707c0b1f"
      "00b9dbc2124cf7633cb86f264fbc524226ad4001bce68bbdba43c9bdc4eb026e"
    )
    ;;
  x86_64-linux-gnu)
    SHA256S=(
      "373b37a68bae3ef441ead24a7bfc950fcbfc274cbdd2b628fc6915f179eb1d8e"
      "588bfaccd0f929650655d10a580f020c6ba9c131712d8fa519280081b8d126eb"
    )
    ;;
esac

echo "Detected platform: $PLATFORM"
echo "Installing to: ${INSTALL_DIR}"

# Download and extract
rm -rf "$INSTALL_DIR"
mkdir -p "$INSTALL_DIR"

for index in "${!PACKAGES[@]}"; do
  filename="${PACKAGES[$index]}"
  archive="${INSTALL_DIR}/${filename}"
  url="https://github.com/espressif/qemu/releases/download/${RELEASE_TAG}/${filename}"
  echo "Downloading: $url"
  curl -fsSL -o "$archive" "$url"
  if command -v sha256sum >/dev/null 2>&1; then
    actual_sha256="$(sha256sum "$archive")"
  else
    actual_sha256="$(shasum -a 256 "$archive")"
  fi
  actual_sha256="${actual_sha256%% *}"
  if [[ "$actual_sha256" != "${SHA256S[$index]}" ]]; then
    echo "SHA-256 mismatch for ${filename}" >&2
    echo "expected: ${SHA256S[$index]}" >&2
    echo "actual:   ${actual_sha256}" >&2
    exit 1
  fi
  tar -xJf "$archive" -C "$INSTALL_DIR" --strip-components=1
  rm -f "$archive"
done

# Verify installation
for exe in qemu-system-riscv32 qemu-system-xtensa; do
  if [ ! -x "${INSTALL_DIR}/bin/${exe}" ]; then
    echo "Error: ${exe} not found after extraction"
    exit 1
  fi
done

echo "ESP QEMU (riscv32 + xtensa) installed successfully to: ${INSTALL_DIR}"
