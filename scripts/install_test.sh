#!/bin/sh
# Offline tests for the split installers. No GitHub, no systemd start.
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
# shellcheck disable=SC1091
. "${ROOT}/scripts/install-lib.sh"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_file() {
  [ -f "$1" ] || fail "missing file $1"
}

assert_exec() {
  [ -x "$1" ] || fail "not executable $1"
}

assert_grep() {
  grep -q "$1" "$2" || fail "expected /$1/ in $2"
}

for f in install.sh install-relay.sh install-node.sh install-lib.sh build-release.sh; do
  sh -n "${ROOT}/scripts/${f}" || fail "sh -n scripts/${f}"
done

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
HOME="$TMP/home"
mkdir -p "$HOME"

STAGE="${TMP}/stage"
mkdir -p "$STAGE"
printf '#!/bin/sh\necho relay\n' >"${STAGE}/mesh-relay"
printf '#!/bin/sh\necho node\n' >"${STAGE}/mesh-node"
printf '#!/bin/sh\necho ctl\n' >"${STAGE}/mesh-ctl"
printf '#!/bin/sh\necho gui\n' >"${STAGE}/agent-mesh"
chmod +x "${STAGE}/mesh-relay" "${STAGE}/mesh-node" "${STAGE}/mesh-ctl" "${STAGE}/agent-mesh"
echo 'readme' >"${STAGE}/README.md"

tar -C "$STAGE" -czf "${TMP}/relay.tar.gz" mesh-relay README.md
tar -C "$STAGE" -czf "${TMP}/node.tar.gz" mesh-node mesh-ctl agent-mesh README.md

# nested archive still installs (old layout)
NEST="${TMP}/nested"
mkdir -p "${NEST}/agent-mesh_dev_linux_amd64"
cp "${STAGE}/mesh-relay" "${NEST}/agent-mesh_dev_linux_amd64/relay"
tar -C "$NEST" -czf "${TMP}/relay-nested.tar.gz" agent-mesh_dev_linux_amd64

run_install() {
  role="$1"
  bundle="$2"
  prefix="$3"
  state="$4"
  shift 4
  env \
    HOME="$HOME" \
    CI= \
    AGENT_MESH_NONINTERACTIVE=1 \
    AGENT_MESH_BUNDLE="$bundle" \
    AGENT_MESH_PREFIX="$prefix" \
    AGENT_MESH_HOME="$state" \
    AGENT_MESH_UNIT_DIR="${TMP}/systemd" \
    AGENT_MESH_LAUNCHD_DIR="${TMP}/launchd" \
    AGENT_MESH_DESKTOP_DIR="${TMP}/applications" \
    AGENT_MESH_APP_DIR="${TMP}/Applications" \
    sh "${ROOT}/scripts/install-${role}.sh" --no-start "$@"
}

PREFIX_R="${TMP}/prefix-relay"
STATE_R="${TMP}/state-relay"
run_install relay "${TMP}/relay.tar.gz" "$PREFIX_R" "$STATE_R" --password 'secret-pass'
assert_exec "${PREFIX_R}/bin/mesh-relay"
assert_exec "${PREFIX_R}/lib/agent-mesh/run-relay"
assert_file "${STATE_R}/relay.env"
assert_grep 'MESH_PASSWORD=secret-pass' "${STATE_R}/relay.env"
assert_file "${TMP}/systemd/agent-mesh-relay.service"
assert_grep 'run-relay' "${TMP}/systemd/agent-mesh-relay.service"

PREFIX_N="${TMP}/prefix-nested"
STATE_N="${TMP}/state-nested"
rm -f "${TMP}/systemd/agent-mesh-relay.service"
run_install relay "${TMP}/relay-nested.tar.gz" "$PREFIX_N" "$STATE_N" --password nested
assert_exec "${PREFIX_N}/bin/mesh-relay"

PREFIX_NODE="${TMP}/prefix-node"
STATE_NODE="${TMP}/state-node"
run_install node "${TMP}/node.tar.gz" "$PREFIX_NODE" "$STATE_NODE" \
  --desktop --relay 'https://mesh.example.com' --password 'node-pass' --listen '0.0.0.0:7700'
assert_exec "${PREFIX_NODE}/bin/mesh-node"
assert_exec "${PREFIX_NODE}/bin/mesh-ctl"
assert_exec "${PREFIX_NODE}/bin/agent-mesh"
assert_exec "${PREFIX_NODE}/lib/agent-mesh/run-node"
assert_exec "${PREFIX_NODE}/lib/agent-mesh/run-desktop"
assert_file "${STATE_NODE}/node.env"
assert_grep 'MESH_RELAY=https://mesh.example.com' "${STATE_NODE}/node.env"
assert_file "${TMP}/systemd/agent-mesh-node.service"
assert_file "${TMP}/applications/agent-mesh.desktop"
assert_grep 'run-desktop' "${TMP}/applications/agent-mesh.desktop"
gui_out="$("${PREFIX_NODE}/lib/agent-mesh/run-desktop")"
[ "$gui_out" = "gui" ] || fail "run-desktop should exec the window binary, got: ${gui_out}"

# 字符界面：headless，不装桌面程序；重装时更新 MESH_RELAY
PREFIX_H="${TMP}/prefix-headless"
STATE_H="${TMP}/state-headless"
APPS_H="${TMP}/applications-headless"
mkdir -p "$APPS_H"
env \
  HOME="$HOME" \
  CI= \
  AGENT_MESH_NONINTERACTIVE=1 \
  AGENT_MESH_DISPLAY=headless \
  AGENT_MESH_BUNDLE="${TMP}/node.tar.gz" \
  AGENT_MESH_PREFIX="$PREFIX_H" \
  AGENT_MESH_HOME="$STATE_H" \
  AGENT_MESH_UNIT_DIR="${TMP}/systemd" \
  AGENT_MESH_DESKTOP_DIR="$APPS_H" \
  AGENT_MESH_APP_DIR="${TMP}/Applications-headless" \
  sh "${ROOT}/scripts/install-node.sh" --no-start --relay 'http://127.0.0.1:8443'
assert_exec "${PREFIX_H}/bin/mesh-node"
[ ! -f "${PREFIX_H}/bin/agent-mesh" ] || fail "headless install copied GUI binary"
[ ! -f "${PREFIX_H}/lib/agent-mesh/run-desktop" ] || fail "headless install wrote run-desktop"
[ ! -f "${APPS_H}/agent-mesh.desktop" ] || fail "headless install wrote .desktop"
assert_grep 'MESH_RELAY=http://127.0.0.1:8443' "${STATE_H}/node.env"
env \
  HOME="$HOME" \
  CI= \
  AGENT_MESH_NONINTERACTIVE=1 \
  AGENT_MESH_DISPLAY=headless \
  AGENT_MESH_BUNDLE="${TMP}/node.tar.gz" \
  AGENT_MESH_PREFIX="$PREFIX_H" \
  AGENT_MESH_HOME="$STATE_H" \
  AGENT_MESH_UNIT_DIR="${TMP}/systemd" \
  AGENT_MESH_DESKTOP_DIR="$APPS_H" \
  sh "${ROOT}/scripts/install-node.sh" --no-start --headless --relay 'https://mesh.example.net'
assert_grep 'MESH_RELAY=https://mesh.example.net' "${STATE_H}/node.env"

AGENT_MESH_DISPLAY=headless
mesh_has_gui && fail "mesh_has_gui should be false for headless override"
AGENT_MESH_DISPLAY=gui
mesh_has_gui || fail "mesh_has_gui should be true for gui override"

# macOS .app writer (functions, OS-independent)
PREFIX="$PREFIX_NODE"
BIN_DIR="${PREFIX_NODE}/bin"
LIB_DIR="${PREFIX_NODE}/lib/agent-mesh"
STATE_DIR="$STATE_NODE"
AGENT_MESH_APP_DIR="${TMP}/Applications"
mesh_write_macos_app
assert_exec "${TMP}/Applications/Agent Mesh.app/Contents/MacOS/Agent Mesh"
assert_file "${TMP}/Applications/Agent Mesh.app/Contents/Info.plist"

# uninstall keeps state unless --purge
env \
  HOME="$HOME" \
  AGENT_MESH_PREFIX="$PREFIX_NODE" \
  AGENT_MESH_HOME="$STATE_NODE" \
  AGENT_MESH_UNIT_DIR="${TMP}/systemd" \
  AGENT_MESH_DESKTOP_DIR="${TMP}/applications" \
  AGENT_MESH_APP_DIR="${TMP}/Applications" \
  sh "${ROOT}/scripts/install-node.sh" --uninstall
[ ! -f "${PREFIX_NODE}/bin/mesh-node" ] || fail "node binary still present"
assert_file "${STATE_NODE}/node.env"

# install.sh with no args must print the split commands
if sh "${ROOT}/scripts/install.sh" >/dev/null 2>"${TMP}/usage.err"; then
  fail "install.sh with no args should exit non-zero"
fi
assert_grep 'install-relay.sh' "${TMP}/usage.err"
assert_grep 'install-node.sh' "${TMP}/usage.err"

echo "ok"
