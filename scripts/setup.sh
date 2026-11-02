#!/bin/sh
# One-shot: check toolchain, build binaries, write ~/.agent-mesh, optional supervisor.
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
STATE_DIR="${AGENT_MESH_HOME:-${HOME}/.agent-mesh}"
PREFIX="${AGENT_MESH_PREFIX:-${HOME}/.local}"
BIN_DIR="${PREFIX}/bin"
INSTALL_BIN=1
DO_SUPERVISOR=0
DO_TEST=0

usage() {
  cat <<EOF
Configure a development tree and local Agent Mesh environment.

Usage: setup.sh [--no-install] [--supervisor] [--test]

  --no-install   build into ./bin only; do not copy to ~/.local/bin
  --supervisor   write ~/.agent-mesh supervisord unit and reload it
  --test         run make test after the build

Requires Go 1.25+ and Node 20+.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --no-install) INSTALL_BIN=0; shift ;;
    --supervisor) DO_SUPERVISOR=1; shift ;;
    --test) DO_TEST=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown flag: $1" >&2; usage >&2; exit 2 ;;
  esac
done

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing $1" >&2
    echo "install Go 1.25+ from https://go.dev/dl/ and Node 20+ from https://nodejs.org/" >&2
    exit 1
  }
}

semver_ok() {
  # $1=have $2=want  (major.minor)
  have_maj="${1%%.*}"
  have_min="${1#*.}"
  have_min="${have_min%%.*}"
  want_maj="${2%%.*}"
  want_min="${2#*.}"
  want_min="${want_min%%.*}"
  [ "$have_maj" -gt "$want_maj" ] && return 0
  [ "$have_maj" -eq "$want_maj" ] && [ "$have_min" -ge "$want_min" ]
}

need go
need node
need npm

go_ver="$(go env GOVERSION 2>/dev/null | sed 's/^go//' | sed 's/[^0-9.].*//')"
node_ver="$(node -v | sed 's/^v//' | sed 's/[^0-9.].*//')"
if ! semver_ok "$go_ver" "1.25"; then
  echo "Go 1.25+ required (have ${go_ver:-unknown})" >&2
  exit 1
fi
if ! semver_ok "$node_ver" "20"; then
  echo "Node 20+ required (have ${node_ver:-unknown})" >&2
  exit 1
fi

echo "Go ${go_ver}  Node ${node_ver}"
cd "$ROOT"
make node relay ctl
if [ "$DO_TEST" -eq 1 ]; then
  make test
fi

mkdir -p "$STATE_DIR"
chmod 0700 "$STATE_DIR"
if [ ! -f "${STATE_DIR}/node.env" ]; then
  cat >"${STATE_DIR}/node.env" <<EOF
# sourced by supervisor and by you
MESH_RELAY=${MESH_RELAY:-http://127.0.0.1:8443}
MESH_PASSWORD=${MESH_PASSWORD:-}
MESH_ADVERTISE=${MESH_ADVERTISE:-}
EOF
  chmod 0600 "${STATE_DIR}/node.env"
  echo "wrote ${STATE_DIR}/node.env"
fi

if [ "$INSTALL_BIN" -eq 1 ]; then
  mkdir -p "$BIN_DIR"
  install -m 0755 "${ROOT}/bin/node" "${BIN_DIR}/mesh-node"
  install -m 0755 "${ROOT}/bin/relay" "${BIN_DIR}/mesh-relay"
  install -m 0755 "${ROOT}/bin/ctl" "${BIN_DIR}/mesh-ctl"
  if [ -x "${ROOT}/bin/agent-mesh" ]; then
    install -m 0755 "${ROOT}/bin/agent-mesh" "${BIN_DIR}/agent-mesh"
  fi
  echo "installed binaries to ${BIN_DIR}"
fi

if [ -x "${ROOT}/bin/ctl" ]; then
  "${ROOT}/bin/ctl" keygen >/dev/null
fi

if [ "$DO_SUPERVISOR" -eq 1 ]; then
  if ! command -v supervisord >/dev/null 2>&1; then
    echo "supervisord not on PATH; skip --supervisor" >&2
  else
    NODE_BIN="${ROOT}/bin/node"
    [ "$INSTALL_BIN" -eq 1 ] && NODE_BIN="${BIN_DIR}/mesh-node"
    sed \
      -e "s|__NODE_BIN__|${NODE_BIN}|g" \
      -e "s|__MESH_ROOT__|${ROOT}|g" \
      -e "s|__HOME__|${HOME}|g" \
      -e "s|__USER__|$(id -un)|g" \
      "${ROOT}/deploy/supervisor/agent-mesh-node.conf.in" >"${STATE_DIR}/agent-mesh-node.conf"
    sed \
      -e "s|__HOME__|${HOME}|g" \
      -e "s|__STATE_DIR__|${STATE_DIR}|g" \
      "${ROOT}/deploy/supervisor/supervisord.conf.in" >"${STATE_DIR}/supervisord.conf"
    supervisord -c "${STATE_DIR}/supervisord.conf" 2>/dev/null || true
    supervisorctl -c "${STATE_DIR}/supervisord.conf" reread
    supervisorctl -c "${STATE_DIR}/supervisord.conf" update
    supervisorctl -c "${STATE_DIR}/supervisord.conf" restart agent-mesh-node
    echo "supervisor unit written to ${STATE_DIR}"
  fi
fi

echo
echo "next:"
echo "  MESH_PASSWORD=change-me ${ROOT}/bin/relay -listen 0.0.0.0:8443 -web"
echo "  ${ROOT}/bin/node -listen 127.0.0.1:7700 -relay http://127.0.0.1:8443"
echo "  ${ROOT}/bin/ctl nodes --relay http://127.0.0.1:8443"
