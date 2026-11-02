#!/bin/sh
# Split installer. Prefer the role-specific scripts:
#   curl -fsSL https://raw.githubusercontent.com/deliangyang/agent-mesh/main/scripts/install-relay.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/deliangyang/agent-mesh/main/scripts/install-node.sh | sh
#
# This file stays so the old one-liner fails with a clear message.
# First argument selects the role: relay | node | all
set -eu

RAW_BASE="${AGENT_MESH_RAW:-https://raw.githubusercontent.com/${AGENT_MESH_REPO:-deliangyang/agent-mesh}/main/scripts}"

usage() {
  cat <<EOF
Agent Mesh install is split by role.

  Relay (server, web UI):
    curl -fsSL ${RAW_BASE}/install-relay.sh | sh

  Node (machine with coding agents):
    curl -fsSL ${RAW_BASE}/install-node.sh | sh

  From this script:
    install.sh relay [flags]
    install.sh node [flags]
    install.sh all [flags]
    install.sh --uninstall [--purge]

Linux enables systemd and starts on boot. macOS uses launchd.
The node installer also writes a desktop launcher (Linux .desktop / macOS .app).
EOF
}

run_role() {
  role="$1"
  shift
  self_dir=""
  case "$0" in
    */install.sh|install.sh)
      self_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
      if [ -f "${self_dir}/install-${role}.sh" ]; then
        sh "${self_dir}/install-${role}.sh" "$@"
        return
      fi
      ;;
  esac
  url="${RAW_BASE}/install-${role}.sh"
  if [ -n "${AGENT_MESH_MIRROR:-}" ]; then
    url="${AGENT_MESH_MIRROR%/}/${url}"
  fi
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" | sh -s -- "$@"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- "$url" | sh -s -- "$@"
  else
    echo "need curl or wget" >&2
    exit 1
  fi
}

if [ $# -eq 0 ]; then
  usage >&2
  exit 2
fi

role=""
case "$1" in
  relay|--relay) role=relay; shift ;;
  node|--node) role=node; shift ;;
  all|--all) role=all; shift ;;
  --uninstall|--purge) role=all ;;
  -h|--help) usage; exit 0 ;;
  *)
    echo "unknown command: $1" >&2
    usage >&2
    exit 2
    ;;
esac

if [ "$role" = all ]; then
  run_role relay "$@"
  run_role node "$@"
  exit 0
fi

run_role "$role" "$@"
