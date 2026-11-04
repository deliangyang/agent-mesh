---
name: mesh-deploy
description: Build Agent Mesh, deploy the relay binary to the host behind MESH_RELAY, and restart the relay and the local node. Use when asked to deploy, ship, release, restart the relay, restart the node, or to get local changes running on the remote relay.
---

# Agent Mesh deploy

Build the local tree, replace the relay binary on the MESH_RELAY host, restart the
relay, then install and restart the local node. Run everything from the repo root.

## One command

```bash
.claude/skills/mesh-deploy/scripts/deploy.sh
```

Steps when run by hand:

```bash
make binaries                                                   # 1. build (web + node + relay + ctl)
scp bin/relay root@$RELAY_HOST:/opt/agent-mesh/relay.new        # 2. stage
ssh root@$RELAY_HOST 'cd /opt/agent-mesh && cp -p relay relay.bak-$(date +%Y%m%d%H%M%S) \
  && mv relay.new relay && chmod 755 relay \
  && systemctl restart agent-mesh-relay'                        # 3. swap + restart relay
install -m 0755 bin/node ~/.local/bin/mesh-node                 # 4. install local node
supervisorctl -c ~/.agent-mesh/supervisord.conf restart agent-mesh-node   # 5. restart node
```

## Inputs

| Variable | Default | Meaning |
|----------|---------|---------|
| `MESH_RELAY` | `~/.agent-mesh/node.env`, then the relay host default | Relay base URL, e.g. `https://mesh.example.net` |
| `MESH_RELAY_SSH` | `root@<host from MESH_RELAY>` | SSH target that can write `/opt/agent-mesh` |
| `MESH_RELAY_DIR` | `/opt/agent-mesh` | Relay install dir on that host |
| `MESH_RELAY_SERVICE` | `agent-mesh-relay` | systemd unit name on that host |
| `MESH_NODE_BIN` | `~/.local/bin/mesh-node` | Installed node binary (matches the supervisor unit) |
| `MESH_NODE_HEALTH` | `http://127.0.0.1:7700` | Local node base URL for the health check |

The relay host must accept SSH as root (or as a user that can `sudo`-free write the
install dir). The relay unit is `ExecStart=/opt/agent-mesh/relay -listen 127.0.0.1:8443 -web`,
so the web UI is embedded in that binary and is part of this deploy.

## Verify

The script fails on any step and prints:

- local `bin/relay` and remote `relay` SHA-256 must match;
- remote `systemctl is-active agent-mesh-relay` = `active`;
- remote `http://127.0.0.1:8443/healthz` = 200;
- local node `healthz` = 200 after the restart;
- `supervisorctl status` shows `agent-mesh-node RUNNING`.

Never paste `relay.env` or `node.env` contents into logs: they hold `MESH_PASSWORD`.

## Notes

- The web UI is embedded in both binaries, so a web-only change still needs this
  deploy (relay for browser clients, node for the local UI).
- Old relay binaries are kept as `/opt/agent-mesh/relay.bak-<timestamp>`.
- If the relay host also runs an `agent-mesh-node` unit, restart it separately only
  when the change affects that node; this skill restarts the local node.
- Rollback: `ssh root@$RELAY_HOST 'cp -p /opt/agent-mesh/relay.bak-<ts> /opt/agent-mesh/relay && systemctl restart agent-mesh-relay'`, then reinstall the previous `bin/node`.
