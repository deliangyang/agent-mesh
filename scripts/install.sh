#!/bin/sh
# One-line install for Agent Mesh release binaries.
#   curl -fsSL https://raw.githubusercontent.com/OWNER/REPO/main/scripts/install.sh | sh
#   curl -fsSL …/install.sh | sh -s -- --uninstall
#   curl -fsSL …/install.sh | sh -s -- --purge
set -eu

PREFIX="${AGENT_MESH_PREFIX:-${HOME}/.local}"
BIN_DIR="${PREFIX}/bin"
STATE_DIR="${AGENT_MESH_HOME:-${HOME}/.agent-mesh}"
REPO="${AGENT_MESH_REPO:-}"
VERSION="${AGENT_MESH_VERSION:-latest}"
UNINSTALL=0
PURGE=0

usage() {
  cat <<EOF
Install Agent Mesh node, relay, and ctl binaries.

Usage: install.sh [--prefix DIR] [--repo owner/name] [--version TAG] [--uninstall] [--purge]

  --prefix DIR     install root (default: ~/.local)
  --repo OWNER/NAME  GitHub repository for releases
  --version TAG    release tag (default: latest)
  --uninstall      remove binaries
  --purge          also delete ~/.agent-mesh

Environment: AGENT_MESH_REPO AGENT_MESH_PREFIX AGENT_MESH_HOME AGENT_MESH_VERSION
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --prefix) PREFIX="$2"; BIN_DIR="${PREFIX}/bin"; shift 2 ;;
    --repo) REPO="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --uninstall) UNINSTALL=1; shift ;;
    --purge) UNINSTALL=1; PURGE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown flag: $1" >&2; usage >&2; exit 2 ;;
  esac
done

detect_repo() {
  if [ -n "$REPO" ]; then
    echo "$REPO"
    return
  fi
  if command -v git >/dev/null 2>&1; then
    url="$(git remote get-url origin 2>/dev/null || true)"
    if [ -n "$url" ]; then
      echo "$url" | sed -e 's#^git@github.com:##' -e 's#^https://github.com/##' -e 's#\.git$##'
      return
    fi
  fi
  echo ""
}

os_arch() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$os" in
    linux|darwin) ;;
    mingw*|msys*|cygwin*) os=windows ;;
    *) echo "unsupported OS: $os" >&2; exit 1 ;;
  esac
  case "$arch" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) echo "unsupported arch: $arch" >&2; exit 1 ;;
  esac
  echo "${os}_${arch}"
}

uninstall() {
  rm -f "${BIN_DIR}/mesh-node" "${BIN_DIR}/mesh-relay" "${BIN_DIR}/mesh-ctl" \
    "${BIN_DIR}/agent-mesh-node" "${BIN_DIR}/agent-mesh-relay" "${BIN_DIR}/agent-mesh-ctl"
  echo "removed Agent Mesh binaries from ${BIN_DIR}"
  if [ "$PURGE" -eq 1 ]; then
    rm -rf "$STATE_DIR"
    echo "removed ${STATE_DIR}"
  else
    echo "kept ${STATE_DIR} (pass --purge to delete keys and config)"
  fi
}

if [ "$UNINSTALL" -eq 1 ]; then
  uninstall
  exit 0
fi

REPO="$(detect_repo)"
if [ -z "$REPO" ]; then
  echo "set AGENT_MESH_REPO=owner/name (or --repo) so install.sh can fetch a GitHub Release." >&2
  echo "from a clone, run ./scripts/setup.sh instead." >&2
  exit 1
fi

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "need $1 on PATH" >&2
    exit 1
  }
}

need tar
need mktemp
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
else
  echo "need curl or wget" >&2
  exit 1
fi

plat="$(os_arch)"
if [ "$VERSION" = latest ]; then
  api="https://api.github.com/repos/${REPO}/releases/latest"
  tag="$(curl -fsSL "$api" 2>/dev/null | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1 || true)"
  if [ -z "$tag" ]; then
    echo "could not resolve latest release for ${REPO}" >&2
    exit 1
  fi
  VERSION="$tag"
fi

asset="agent-mesh_${VERSION}_${plat}.tar.gz"
url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
echo "downloading ${url}"
fetch "$url" "${tmp}/${asset}"
tar -C "$tmp" -xzf "${tmp}/${asset}"

mkdir -p "$BIN_DIR" "$STATE_DIR"
install -m 0755 "${tmp}/node" "${BIN_DIR}/mesh-node"
install -m 0755 "${tmp}/relay" "${BIN_DIR}/mesh-relay"
install -m 0755 "${tmp}/ctl" "${BIN_DIR}/mesh-ctl"
chmod 0700 "$STATE_DIR"

echo "installed:"
echo "  ${BIN_DIR}/mesh-node"
echo "  ${BIN_DIR}/mesh-relay"
echo "  ${BIN_DIR}/mesh-ctl"
echo "state: ${STATE_DIR}"
echo
echo "add ${BIN_DIR} to PATH if it is missing, then:"
echo "  MESH_PASSWORD=change-me mesh-relay -listen 0.0.0.0:8443 -web"
echo "  mesh-node -listen 127.0.0.1:7700 -relay http://127.0.0.1:8443"
echo "  mesh-ctl keygen"
