#!/usr/bin/env bash
# Build, deploy the relay to MESH_RELAY, restart the relay, restart the local node.
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/../../../.." && pwd)"
cd "$ROOT"

STATE_DIR="${AGENT_MESH_HOME:-$HOME/.agent-mesh}"

die() { echo "deploy: $*" >&2; exit 1; }
note() { echo "==> $*"; }

# MESH_RELAY from the environment or node.env; never print the whole file.
if [ -z "${MESH_RELAY:-}" ] && [ -f "$STATE_DIR/node.env" ]; then
  MESH_RELAY="$(sed -n 's/^[[:space:]]*MESH_RELAY=//p' "$STATE_DIR/node.env" | tail -1 | tr -d "\"'")"
fi
[ -n "${MESH_RELAY:-}" ] || die "MESH_RELAY is not set (env or $STATE_DIR/node.env)"

relay_host="$MESH_RELAY"
relay_host="${relay_host#*://}"
relay_host="${relay_host%%/*}"
relay_host="${relay_host%%:*}"
[ -n "$relay_host" ] || die "cannot parse host from MESH_RELAY=$MESH_RELAY"

RELAY_SSH="${MESH_RELAY_SSH:-root@$relay_host}"
RELAY_DIR="${MESH_RELAY_DIR:-/opt/agent-mesh}"
RELAY_SERVICE="${MESH_RELAY_SERVICE:-agent-mesh-relay}"
NODE_BIN="${MESH_NODE_BIN:-$HOME/.local/bin/mesh-node}"
NODE_HEALTH="${MESH_NODE_HEALTH:-http://127.0.0.1:7700}"
SUPERVISOR_CONF="$STATE_DIR/supervisord.conf"

remote() { ssh -o BatchMode=yes "$RELAY_SSH" "$@"; }

# 1. Build: web assets are embedded in relay and node.
note "build (make binaries) for $relay_host"
make binaries

[ -x bin/relay ] || die "bin/relay missing"
[ -x bin/node ] || die "bin/node missing"

# 2. Deploy relay binary.
note "deploy relay to $RELAY_SSH:$RELAY_DIR/relay"
scp -q bin/relay "$RELAY_SSH:$RELAY_DIR/relay.new"
remote "set -e
  cd '$RELAY_DIR'
  cp -p relay relay.bak-\$(date +%Y%m%d%H%M%S)
  mv relay.new relay
  chmod 755 relay"

# 3. Restart relay and verify.
note "restart $RELAY_SERVICE"
remote "systemctl restart '$RELAY_SERVICE'"
sleep 2
remote "systemctl is-active '$RELAY_SERVICE'" | grep -qx active \
  || die "relay service is not active"
remote "curl -fsS -o /dev/null -m 8 http://127.0.0.1:8443/healthz" \
  || die "relay healthz failed"

local_sum="$(sha256sum bin/relay | awk '{print $1}')"
remote_sum="$(remote "sha256sum '$RELAY_DIR/relay'" | awk '{print $1}')"
[ "$local_sum" = "$remote_sum" ] || die "relay hash mismatch: local $local_sum remote $remote_sum"
note "relay running $local_sum"

# 4. Install the local node binary (same one the supervisor unit runs).
note "install node to $NODE_BIN"
install -D -m 0755 bin/node "$NODE_BIN"

# 5. Restart the local node.
if [ -f "$SUPERVISOR_CONF" ] && command -v supervisorctl >/dev/null 2>&1; then
  note "restart agent-mesh-node (supervisor)"
  supervisorctl -c "$SUPERVISOR_CONF" restart agent-mesh-node >/dev/null
  supervisorctl -c "$SUPERVISOR_CONF" status agent-mesh-node | grep -q RUNNING \
    || die "agent-mesh-node is not RUNNING"
elif command -v systemctl >/dev/null 2>&1 && systemctl --user list-unit-files 2>/dev/null | grep -q '^agent-mesh-node'; then
  note "restart agent-mesh-node (systemd user)"
  systemctl --user restart agent-mesh-node
  systemctl --user is-active agent-mesh-node | grep -qx active \
    || die "agent-mesh-node is not active"
else
  die "no supervisor/systemd unit for agent-mesh-node; start $NODE_BIN manually"
fi

# 6. Verify the local node.
sleep 2
curl -fsS -o /dev/null -m 8 "$NODE_HEALTH/healthz" || die "node healthz failed at $NODE_HEALTH"
note "node healthy at $NODE_HEALTH"

# 7. Warn about a second process registering the same node identity. Both read
# ~/.agent-mesh/node.key, so the relay keeps kicking one tunnel every ~16s and
# the web catalog stream drops with it.
if pgrep -f '/agent-mesh( |$)' >/dev/null 2>&1; then
  echo "warning: the desktop app (agent-mesh) is running; it registers the same node id and will fight this tunnel" >&2
fi
NODE_LOG="$STATE_DIR/supervisor.err.log"
if [ -f "$NODE_LOG" ]; then
  since="$(date -d '-60 seconds' +%Y-%m-%dT%H:%M:%S 2>/dev/null || true)"
  if [ -n "$since" ]; then
    drops="$(awk -v since="time=$since" '$0 ~ /tunnel dropped/ && $1 >= since' "$NODE_LOG" | wc -l)"
    [ "$drops" -gt 2 ] && echo "warning: tunnel dropped $drops times in the last 60s (duplicate node identity?)" >&2
  fi
fi
note "done: relay=$relay_host node=$(hostname)"
