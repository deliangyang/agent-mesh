#!/bin/sh
# Install Agent Mesh node. Character console → headless service; GUI → desktop launcher.
#   curl -fsSL https://raw.githubusercontent.com/deliangyang/agent-mesh/main/scripts/install-node.sh | sh
#   curl -fsSL …/install-node.sh | sh -s -- --relay https://mesh.example.com
#   curl -fsSL …/install-node.sh | sh -s -- --headless
#   curl -fsSL …/install-node.sh | sh -s -- --uninstall
set -eu
AGENT_MESH_ROLE=node

load_install_lib() {
  case "$0" in
    */install-relay.sh|*/install-node.sh|*/install.sh|install-relay.sh|install-node.sh|install.sh)
      _dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
      if [ -f "${_dir}/install-lib.sh" ]; then
        # shellcheck disable=SC1091
        . "${_dir}/install-lib.sh"
        AGENT_MESH_SCRIPT_DIR="$_dir"
        return
      fi
      ;;
  esac
  _raw="${AGENT_MESH_RAW:-https://raw.githubusercontent.com/${AGENT_MESH_REPO:-deliangyang/agent-mesh}/main/scripts/install-lib.sh}"
  if [ -n "${AGENT_MESH_MIRROR:-}" ]; then
    _raw="${AGENT_MESH_MIRROR%/}/${_raw}"
  fi
  _lib="$(mktemp)"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$_raw" -o "$_lib"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$_lib" "$_raw"
  else
    echo "need curl or wget to fetch install-lib.sh" >&2
    exit 1
  fi
  # shellcheck disable=SC1090
  . "$_lib"
  rm -f "$_lib"
}

load_install_lib
agent_mesh_install_main "$@"
