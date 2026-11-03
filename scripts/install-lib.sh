# Shared installer for Agent Mesh relay / node.
# Sourced by install-relay.sh and install-node.sh. Not meant to be curled alone.
# shellcheck shell=sh

DEFAULT_REPO="deliangyang/agent-mesh"

PREFIX="${AGENT_MESH_PREFIX:-}"
BIN_DIR=""
LIB_DIR=""
STATE_DIR="${AGENT_MESH_HOME:-${HOME}/.agent-mesh}"
REPO="${AGENT_MESH_REPO:-}"
VERSION="${AGENT_MESH_VERSION:-latest}"
UNINSTALL=0
PURGE=0
NO_SERVICE=0
NO_START=0
DESKTOP_MODE=auto
WITH_DESKTOP=0
LISTEN=""
RELAY_URL="${MESH_RELAY:-}"
PASSWORD="${MESH_PASSWORD:-}"
ADVERTISE="${MESH_ADVERTISE:-}"
NODE_NAME="${MESH_NAME:-}"
BUNDLE="${AGENT_MESH_BUNDLE:-}"

mesh_usage() {
  role="${AGENT_MESH_ROLE:-node}"
  cat <<EOF
Install Agent Mesh ${role} from a GitHub Release (or a local bundle).

Usage: install-${role}.sh [flags]

  --prefix DIR       install root (root: /usr/local, else ~/.local)
  --repo OWNER/NAME  GitHub repository (default: ${DEFAULT_REPO})
  --version TAG      release tag (default: latest)
  --listen ADDR      bind address
  --password PASS    relay password (MESH_PASSWORD)
  --relay URL        relay URL, node only (MESH_RELAY)
  --advertise URL    public callback URL, node only
  --name NAME        node display name
  --no-service       skip systemd / launchd
  --no-start         write the service but do not start it
  --headless         node only: character console, no desktop launcher
  --desktop          node only: force .desktop / .app even on a TTY
  --no-desktop       same as --headless
  --uninstall        remove binaries and services
  --purge            also delete ${STATE_DIR}

Environment: AGENT_MESH_REPO AGENT_MESH_PREFIX AGENT_MESH_HOME
             AGENT_MESH_VERSION AGENT_MESH_MIRROR AGENT_MESH_BUNDLE
             MESH_PASSWORD MESH_RELAY MESH_ADVERTISE
EOF
}

mesh_parse_args() {
  while [ $# -gt 0 ]; do
    case "$1" in
      --prefix) PREFIX="$2"; shift 2 ;;
      --repo) REPO="$2"; shift 2 ;;
      --version) VERSION="$2"; shift 2 ;;
      --listen) LISTEN="$2"; shift 2 ;;
      --password) PASSWORD="$2"; shift 2 ;;
      --relay) RELAY_URL="$2"; shift 2 ;;
      --advertise) ADVERTISE="$2"; shift 2 ;;
      --name) NODE_NAME="$2"; shift 2 ;;
      --no-service) NO_SERVICE=1; shift ;;
      --no-start) NO_START=1; shift ;;
      --headless|--no-desktop) DESKTOP_MODE=no; shift ;;
      --desktop) DESKTOP_MODE=yes; shift ;;
      --uninstall) UNINSTALL=1; shift ;;
      --purge) UNINSTALL=1; PURGE=1; shift ;;
      -h|--help) mesh_usage; exit 0 ;;
      *) echo "unknown flag: $1" >&2; mesh_usage >&2; exit 2 ;;
    esac
  done
}

mesh_is_root() {
  [ "$(id -u)" -eq 0 ]
}

# 图形会话：DISPLAY / Wayland / 本机 macOS WindowServer。
# SSH 到无显示转发的机器视为字符界面。AGENT_MESH_DISPLAY=gui|headless 可强制。
mesh_has_gui() {
  case "${AGENT_MESH_DISPLAY:-}" in
    gui|yes|1) return 0 ;;
    headless|tty|no|0) return 1 ;;
  esac
  if [ -n "${DISPLAY:-}" ]; then
    return 0
  fi
  if [ -n "${WAYLAND_DISPLAY:-}" ]; then
    return 0
  fi
  case "${XDG_SESSION_TYPE:-}" in
    x11|wayland|mir) return 0 ;;
    tty) return 1 ;;
  esac
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  if [ "$os" = darwin ]; then
    if [ -n "${SSH_CONNECTION:-}${SSH_TTY:-}" ]; then
      return 1
    fi
    pgrep -qx WindowServer 2>/dev/null
    return $?
  fi
  return 1
}

mesh_interactive() {
  if [ -n "${AGENT_MESH_NONINTERACTIVE:-}" ] || [ -n "${CI:-}" ]; then
    return 1
  fi
  [ -r /dev/tty ] && [ -w /dev/tty ]
}

mesh_resolve_desktop() {
  case "$DESKTOP_MODE" in
    yes)
      WITH_DESKTOP=1
      ;;
    no)
      WITH_DESKTOP=0
      ;;
    *)
      if mesh_has_gui; then
        WITH_DESKTOP=1
        DESKTOP_MODE=gui
      else
        WITH_DESKTOP=0
        DESKTOP_MODE=headless
      fi
      ;;
  esac
  if [ "$WITH_DESKTOP" -eq 1 ]; then
    echo "检测到图形界面，将安装桌面启动入口"
  else
    echo "检测到字符界面，按 headless 安装节点服务（不开桌面程序）"
  fi
}

mesh_env_get() {
  file="$1"
  key="$2"
  [ -f "$file" ] || return 1
  awk -v k="$key" '
    index($0, k "=") == 1 { print substr($0, length(k) + 2); exit }
  ' "$file"
}

mesh_upsert_kv() {
  file="$1"
  key="$2"
  value="$3"
  tmp="${file}.tmp.$$"
  if [ -f "$file" ] && grep -q "^${key}=" "$file"; then
    awk -v k="$key" -v v="$value" '
      index($0, k "=") == 1 { print k "=" v; next }
      { print }
    ' "$file" >"$tmp"
  elif [ -f "$file" ]; then
    cat "$file" >"$tmp"
    printf '%s=%s\n' "$key" "$value" >>"$tmp"
  else
    printf '%s=%s\n' "$key" "$value" >"$tmp"
  fi
  mv "$tmp" "$file"
  chmod 0600 "$file"
}

# 安装节点时确认中继地址；curl|sh 从 /dev/tty 读入，方便改 MESH_RELAY。
mesh_prompt_relay() {
  current="$RELAY_URL"
  if [ -z "$current" ]; then
    current="$(mesh_env_get "${STATE_DIR}/node.env" MESH_RELAY || true)"
  fi
  if [ -z "$current" ]; then
    current="http://127.0.0.1:8443"
  fi
  echo
  echo "节点需要中继地址 MESH_RELAY，当前: ${current}"
  echo "装完后可编辑 ${STATE_DIR}/node.env 再重启节点服务"
  if mesh_interactive; then
    printf "请输入中继地址（直接回车保留当前值）: " >/dev/tty
    answer=""
    IFS= read -r answer </dev/tty || true
    if [ -n "$answer" ]; then
      RELAY_URL="$answer"
    else
      RELAY_URL="$current"
    fi
  else
    echo "非交互安装，沿用 ${current}。要用别的地址请加 --relay URL 或 MESH_RELAY=..."
    RELAY_URL="$current"
  fi
  case "$RELAY_URL" in
    http://*|https://*) ;;
    *) echo "警告: 中继地址应以 http:// 或 https:// 开头" >&2 ;;
  esac
  echo "使用 MESH_RELAY=${RELAY_URL}"
}

mesh_init_paths() {
  if [ -z "$PREFIX" ]; then
    if mesh_is_root; then
      PREFIX="/usr/local"
    else
      PREFIX="${HOME}/.local"
    fi
  fi
  BIN_DIR="${PREFIX}/bin"
  LIB_DIR="${PREFIX}/lib/agent-mesh"
}

mesh_detect_repo() {
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
  echo "$DEFAULT_REPO"
}

mesh_os_arch() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$os" in
    linux|darwin) ;;
    *) echo "unsupported OS: $os" >&2; exit 1 ;;
  esac
  case "$arch" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) echo "unsupported arch: $arch" >&2; exit 1 ;;
  esac
  echo "${os}_${arch}"
}

mesh_need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "need $1 on PATH" >&2
    exit 1
  }
}

mesh_mirror_url() {
  url="$1"
  if [ -n "${AGENT_MESH_MIRROR:-}" ]; then
    echo "${AGENT_MESH_MIRROR%/}/${url}"
  else
    echo "$url"
  fi
}

mesh_fetch() {
  url="$(mesh_mirror_url "$1")"
  dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
  else
    echo "need curl or wget" >&2
    return 1
  fi
}

mesh_headers() {
  url="$(mesh_mirror_url "$1")"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSI "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -qS --spider "$url" 2>&1
  else
    return 1
  fi
}

mesh_resolve_latest() {
  repo="$1"
  loc=""
  loc="$(mesh_headers "https://github.com/${repo}/releases/latest" 2>/dev/null | tr -d '\r' | awk 'tolower($1)=="location:"{print $2; exit}')" || true
  case "$loc" in
    */tag/*)
      echo "${loc##*/}"
      return 0
      ;;
  esac
  if command -v git >/dev/null 2>&1; then
    tag="$(git ls-remote --tags --refs "https://github.com/${repo}.git" 2>/dev/null | sed 's#.*refs/tags/##' | grep -E '^v?[0-9]' | tail -n 1 || true)"
    if [ -n "$tag" ]; then
      echo "$tag"
      return 0
    fi
  fi
  return 1
}

mesh_find_file() {
  dir="$1"
  shift
  for name in "$@"; do
    found="$(find "$dir" -type f \( -name "$name" -o -name "${name}.exe" \) 2>/dev/null | head -n 1 || true)"
    if [ -n "$found" ]; then
      echo "$found"
      return 0
    fi
  done
  return 1
}

mesh_find_dir() {
  dir="$1"
  shift
  for name in "$@"; do
    found="$(find "$dir" -type d -name "$name" 2>/dev/null | head -n 1 || true)"
    if [ -n "$found" ]; then
      echo "$found"
      return 0
    fi
  done
  return 1
}

mesh_random() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 12
  else
    dd if=/dev/urandom bs=12 count=1 2>/dev/null | od -An -tx1 | tr -d ' \n'
  fi
}

mesh_write_if_missing() {
  path="$1"
  if [ -f "$path" ]; then
    echo "kept ${path}"
    return 1
  fi
  cat >"$path"
  chmod 0600 "$path"
  echo "wrote ${path}"
  return 0
}

mesh_write_relay_env() {
  mkdir -p "$STATE_DIR"
  chmod 0700 "$STATE_DIR"
  listen="${LISTEN:-0.0.0.0:8443}"
  if [ -f "${STATE_DIR}/relay.env" ]; then
    echo "kept ${STATE_DIR}/relay.env"
    return 0
  fi
  if [ -z "$PASSWORD" ]; then
    PASSWORD="$(mesh_random)"
    GENERATED_PASSWORD=1
  fi
  mesh_write_if_missing "${STATE_DIR}/relay.env" <<EOF
# sourced by the relay service wrapper
MESH_LISTEN=${listen}
MESH_PASSWORD=${PASSWORD}
EOF
}

mesh_write_node_env() {
  mkdir -p "$STATE_DIR"
  chmod 0700 "$STATE_DIR"
  listen="${LISTEN:-0.0.0.0:7700}"
  relay="${RELAY_URL:-http://127.0.0.1:8443}"
  path="${STATE_DIR}/node.env"
  if [ ! -f "$path" ]; then
    cat >"$path" <<EOF
# sourced by the node service wrapper
MESH_LISTEN=${listen}
MESH_RELAY=${relay}
MESH_PASSWORD=${PASSWORD}
MESH_ADVERTISE=${ADVERTISE}
MESH_NAME=${NODE_NAME}
EOF
    chmod 0600 "$path"
    echo "wrote ${path}"
  else
    mesh_upsert_kv "$path" MESH_RELAY "$relay"
    echo "updated ${path} MESH_RELAY=${relay}"
  fi
}

mesh_write_wrappers() {
  mkdir -p "$LIB_DIR"
  role="$1"
  if [ "$role" = relay ]; then
    cat >"${LIB_DIR}/run-relay" <<EOF
#!/bin/sh
set -e
STATE="\${AGENT_MESH_HOME:-\${HOME}/.agent-mesh}"
if [ -f "\${STATE}/relay.env" ]; then
  set -a
  . "\${STATE}/relay.env"
  set +a
fi
exec "${BIN_DIR}/mesh-relay" -listen "\${MESH_LISTEN:-0.0.0.0:8443}" -web
EOF
    chmod 0755 "${LIB_DIR}/run-relay"
  fi
  if [ "$role" = node ]; then
    cat >"${LIB_DIR}/run-node" <<EOF
#!/bin/sh
set -e
STATE="\${AGENT_MESH_HOME:-\${HOME}/.agent-mesh}"
if [ -f "\${STATE}/node.env" ]; then
  set -a
  . "\${STATE}/node.env"
  set +a
fi
set -- "${BIN_DIR}/mesh-node" -listen "\${MESH_LISTEN:-0.0.0.0:7700}"
if [ -n "\${MESH_RELAY:-}" ]; then
  set -- "\$@" -relay "\$MESH_RELAY"
fi
if [ -n "\${MESH_ADVERTISE:-}" ]; then
  set -- "\$@" -advertise "\$MESH_ADVERTISE"
fi
if [ -n "\${MESH_NAME:-}" ]; then
  set -- "\$@" -name "\$MESH_NAME"
fi
exec "\$@"
EOF
    chmod 0755 "${LIB_DIR}/run-node"
    if [ "$WITH_DESKTOP" -eq 1 ]; then
    cat >"${LIB_DIR}/run-desktop" <<EOF
#!/bin/sh
set -e
STATE="\${AGENT_MESH_HOME:-\${HOME}/.agent-mesh}"
if [ -f "\${STATE}/node.env" ]; then
  set -a
  . "\${STATE}/node.env"
  set +a
fi
GUI="${BIN_DIR}/agent-mesh"
if [ -x "\$GUI" ]; then
  exec "\$GUI" "\$@"
fi
os="\$(uname -s | tr '[:upper:]' '[:lower:]')"
if [ "\$os" = darwin ]; then
  launchctl kickstart -k "gui/\$(id -u)/com.agentmesh.node" 2>/dev/null || \
    launchctl kickstart -k system/com.agentmesh.node 2>/dev/null || true
  if command -v osascript >/dev/null 2>&1; then
    osascript -e 'display notification "Node is running as a background service." with title "Agent Mesh"'
  fi
  if [ -n "\${MESH_RELAY:-}" ]; then
    open "\$MESH_RELAY" 2>/dev/null || true
  fi
  exit 0
fi
if command -v systemctl >/dev/null 2>&1; then
  if [ "\$(id -u)" -eq 0 ]; then
    systemctl start agent-mesh-node.service 2>/dev/null || true
  else
    systemctl --user start agent-mesh-node.service 2>/dev/null || true
  fi
fi
if [ -n "\${MESH_RELAY:-}" ] && command -v xdg-open >/dev/null 2>&1; then
  xdg-open "\$MESH_RELAY" >/dev/null 2>&1 || true
fi
EOF
    chmod 0755 "${LIB_DIR}/run-desktop"
    fi
  fi
}

mesh_systemd_unit_dir() {
  if [ -n "${AGENT_MESH_UNIT_DIR:-}" ]; then
    echo "$AGENT_MESH_UNIT_DIR"
    return
  fi
  if mesh_is_root; then
    echo "/etc/systemd/system"
  else
    echo "${HOME}/.config/systemd/user"
  fi
}

mesh_launchd_dir() {
  if [ -n "${AGENT_MESH_LAUNCHD_DIR:-}" ]; then
    echo "$AGENT_MESH_LAUNCHD_DIR"
    return
  fi
  if mesh_is_root; then
    echo "/Library/LaunchDaemons"
  else
    echo "${HOME}/Library/LaunchAgents"
  fi
}

mesh_desktop_dir() {
  if [ -n "${AGENT_MESH_DESKTOP_DIR:-}" ]; then
    echo "$AGENT_MESH_DESKTOP_DIR"
    return
  fi
  if mesh_is_root; then
    echo "/usr/share/applications"
  else
    echo "${HOME}/.local/share/applications"
  fi
}

mesh_hicolor_dir() {
  if [ -n "${AGENT_MESH_ICONS_DIR:-}" ]; then
    echo "$AGENT_MESH_ICONS_DIR"
    return
  fi
  if mesh_is_root; then
    echo "/usr/share/icons/hicolor"
  else
    echo "${HOME}/.local/share/icons/hicolor"
  fi
}

mesh_app_dir() {
  if [ -n "${AGENT_MESH_APP_DIR:-}" ]; then
    echo "$AGENT_MESH_APP_DIR"
    return
  fi
  if mesh_is_root; then
    echo "/Applications"
  else
    echo "${HOME}/Applications"
  fi
}

# Resolve shipped multi-size icons (release bundle, checkout, or LIB_DIR copy).
mesh_icon_src_dir() {
  if [ -n "${AGENT_MESH_ICON_SRC:-}" ] && [ -d "${AGENT_MESH_ICON_SRC}/16x16/apps" ]; then
    echo "$AGENT_MESH_ICON_SRC"
    return 0
  fi
  if [ -d "${LIB_DIR}/icons/16x16/apps" ]; then
    echo "${LIB_DIR}/icons"
    return 0
  fi
  if [ -n "${AGENT_MESH_SCRIPT_DIR:-}" ]; then
    cand="${AGENT_MESH_SCRIPT_DIR}/../deploy/desktop/icons"
    if [ -d "${cand}/16x16/apps" ]; then
      CDPATH= cd -- "$cand" && pwd
      return 0
    fi
  fi
  return 1
}

mesh_write_systemd_unit() {
  role="$1"
  unit_dir="$(mesh_systemd_unit_dir)"
  mkdir -p "$unit_dir"
  if mesh_is_root; then
    wanted="multi-user.target"
  else
    wanted="default.target"
  fi
  wrapper="${LIB_DIR}/run-${role}"
  cat >"${unit_dir}/agent-mesh-${role}.service" <<EOF
[Unit]
Description=Agent Mesh ${role}
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${wrapper}
Restart=on-failure
RestartSec=5
KillMode=process
Environment=HOME=${HOME}
WorkingDirectory=${HOME}

[Install]
WantedBy=${wanted}
EOF
  echo "wrote ${unit_dir}/agent-mesh-${role}.service"
}

mesh_write_launchd_plist() {
  role="$1"
  label="com.agentmesh.${role}"
  dir="$(mesh_launchd_dir)"
  mkdir -p "$dir"
  wrapper="${LIB_DIR}/run-${role}"
  log="${STATE_DIR}/${role}.log"
  if mesh_is_root; then
    domain="system"
  else
    domain="gui/$(id -u)"
  fi
  cat >"${dir}/${label}.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>${label}</string>
  <key>ProgramArguments</key>
  <array>
    <string>${wrapper}</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>${HOME}</string>
    <key>PATH</key>
    <string>${BIN_DIR}:/usr/local/bin:/usr/bin:/bin</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>${log}</string>
  <key>StandardErrorPath</key>
  <string>${log}</string>
  <key>WorkingDirectory</key>
  <string>${HOME}</string>
</dict>
</plist>
EOF
  echo "wrote ${dir}/${label}.plist"
  echo "$domain" >"${STATE_DIR}/.${role}.launchd-domain" 2>/dev/null || true
}

mesh_write_svg_icon() {
  dest="$1"
  mkdir -p "$(dirname "$dest")"
  cat >"$dest" <<'EOF'
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32" width="512" height="512">
  <rect width="32" height="32" rx="7" fill="#1E2B27"/>
  <path d="M16 9 L9.2 21.2 H22.8 Z" fill="none" stroke="#7FBEAD" stroke-width="1.6" stroke-linejoin="round"/>
  <circle cx="16" cy="9" r="2.4" fill="#E3EBE6"/>
  <circle cx="9.2" cy="21.2" r="2.4" fill="#7FBEAD"/>
  <circle cx="22.8" cy="21.2" r="2.4" fill="#7FBEAD"/>
</svg>
EOF
}

# Write Freedesktop hicolor PNG sizes when no prebuilt icons are available.
mesh_generate_png_icons() {
  dest_root="$1"
  command -v python3 >/dev/null 2>&1 || return 1
  python3 - "$dest_root" <<'PY'
import os, struct, sys, zlib

root = sys.argv[1]
sizes = (16, 24, 32, 48, 64, 128, 256, 512)
ink = (0x1E, 0x2B, 0x27, 0xFF)
signal = (0x7F, 0xBE, 0xAD, 0xFF)
paper = (0xE3, 0xEB, 0xE6, 0xFF)


def in_round_rect(x, y, w, h, r):
    if r < 1:
        return True
    if r <= x < w - r:
        return 0 <= y < h
    if r <= y < h - r:
        return 0 <= x < w
    cx = cy = r
    if x >= w - r and y < r:
        cx, cy = w - r, r
    elif x < r and y >= h - r:
        cx, cy = r, h - r
    elif x >= w - r and y >= h - r:
        cx, cy = w - r, h - r
    return (x - cx) ** 2 + (y - cy) ** 2 <= r * r


def draw(size):
    scale = 4 if size < 128 else 1
    big = size * scale
    px = bytearray(big * big * 4)
    rr = big * 7 / 32

    def setp(x, y, c):
        if 0 <= x < big and 0 <= y < big:
            i = (y * big + x) * 4
            px[i:i + 4] = bytes(c)

    for y in range(big):
        for x in range(big):
            if in_round_rect(x + 0.5, y + 0.5, big, big, rr):
                setp(x, y, ink)
    s = float(big)
    pts = ((s * 16 / 32, s * 9 / 32), (s * 9.2 / 32, s * 21.2 / 32), (s * 22.8 / 32, s * 21.2 / 32))
    stroke = max(1.0, s * 1.6 / 32)
    cr = s * 2.4 / 32

    def stroke_line(a, b):
        x0, y0 = a
        x1, y1 = b
        n = int(abs(x1 - x0) + abs(y1 - y0)) * 2 + 1
        hw = stroke / 2
        for i in range(n + 1):
            t = i / n
            cx = x0 + (x1 - x0) * t
            cy = y0 + (y1 - y0) * t
            for y in range(int(cy - hw - 1), int(cy + hw + 2)):
                for x in range(int(cx - hw - 1), int(cx + hw + 2)):
                    if (x + 0.5 - cx) ** 2 + (y + 0.5 - cy) ** 2 <= hw * hw:
                        setp(x, y, signal)

    stroke_line(pts[0], pts[1])
    stroke_line(pts[1], pts[2])
    stroke_line(pts[2], pts[0])
    for i, (cx, cy) in enumerate(pts):
        col = paper if i == 0 else signal
        for y in range(int(cy - cr - 1), int(cy + cr + 2)):
            for x in range(int(cx - cr - 1), int(cx + cr + 2)):
                if (x + 0.5 - cx) ** 2 + (y + 0.5 - cy) ** 2 <= cr * cr:
                    setp(x, y, col)
    if scale == 1:
        return bytes(px)
    out = bytearray(size * size * 4)
    area = scale * scale
    for y in range(size):
        for x in range(size):
            r = g = b = a = 0
            for dy in range(scale):
                for dx in range(scale):
                    i = ((y * scale + dy) * big + (x * scale + dx)) * 4
                    r += px[i]
                    g += px[i + 1]
                    b += px[i + 2]
                    a += px[i + 3]
            j = (y * size + x) * 4
            out[j:j + 4] = bytes((r // area, g // area, b // area, a // area))
    return bytes(out)


def write_png(path, size, raw):
    def chunk(tag, data):
        return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)

    rows = b"".join(b"\x00" + raw[i * size * 4:(i + 1) * size * 4] for i in range(size))
    ihdr = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    data = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IDAT", zlib.compress(rows, 9)) + chunk(b"IEND", b"")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "wb") as f:
        f.write(data)


for size in sizes:
    write_png(os.path.join(root, f"{size}x{size}", "apps", "agent-mesh.png"), size, draw(size))
print(root)
PY
}

mesh_install_desktop_icons() {
  mkdir -p "$LIB_DIR"
  hicolor="$(mesh_hicolor_dir)"
  mkdir -p "$hicolor"
  src=""
  if src="$(mesh_icon_src_dir)"; then
    :
  else
    src="${LIB_DIR}/icons"
    mkdir -p "$src"
    mesh_generate_png_icons "$src" || true
  fi
  mesh_write_svg_icon "${LIB_DIR}/agent-mesh.svg"
  if [ -f "${src}/scalable/apps/agent-mesh.svg" ]; then
    cp "${src}/scalable/apps/agent-mesh.svg" "${LIB_DIR}/agent-mesh.svg"
  fi
  for size in 16 24 32 48 64 128 256 512; do
    png="${src}/${size}x${size}/apps/agent-mesh.png"
    if [ -f "$png" ]; then
      mkdir -p "${LIB_DIR}/icons/${size}x${size}/apps"
      cp "$png" "${LIB_DIR}/icons/${size}x${size}/apps/agent-mesh.png"
      # Some machines have root-owned size dirs under ~/.local/share/icons; skip those.
      if mkdir -p "${hicolor}/${size}x${size}/apps" 2>/dev/null; then
        cp "$png" "${hicolor}/${size}x${size}/apps/agent-mesh.png" 2>/dev/null || true
      fi
    fi
  done
  mkdir -p "${LIB_DIR}/icons/scalable/apps"
  cp "${LIB_DIR}/agent-mesh.svg" "${LIB_DIR}/icons/scalable/apps/agent-mesh.svg"
  if mkdir -p "${hicolor}/scalable/apps" 2>/dev/null; then
    cp "${LIB_DIR}/agent-mesh.svg" "${hicolor}/scalable/apps/agent-mesh.svg" 2>/dev/null || true
  fi
  # Prefer a concrete high-res PNG so launchers that ignore icon themes still look sharp.
  if [ -f "${LIB_DIR}/icons/256x256/apps/agent-mesh.png" ]; then
    cp "${LIB_DIR}/icons/256x256/apps/agent-mesh.png" "${LIB_DIR}/agent-mesh.png"
  elif [ -f "${LIB_DIR}/icons/512x512/apps/agent-mesh.png" ]; then
    cp "${LIB_DIR}/icons/512x512/apps/agent-mesh.png" "${LIB_DIR}/agent-mesh.png"
  fi
  if [ -z "${AGENT_MESH_ICONS_DIR:-}" ] && command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f -t "$hicolor" 2>/dev/null || true
  fi
}

mesh_write_linux_desktop() {
  dir="$(mesh_desktop_dir)"
  mkdir -p "$dir" "$LIB_DIR"
  mesh_install_desktop_icons
  icon="${LIB_DIR}/agent-mesh.png"
  if [ ! -f "$icon" ]; then
    icon="${LIB_DIR}/agent-mesh.svg"
  fi
  cat >"${dir}/agent-mesh.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Agent Mesh
Comment=Local coding-agent node
Exec=${LIB_DIR}/run-desktop
Icon=${icon}
Terminal=false
Categories=Development;Utility;
StartupNotify=false
EOF
  echo "wrote ${dir}/agent-mesh.desktop"
  if [ -z "${AGENT_MESH_DESKTOP_DIR:-}" ] && command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "$dir" 2>/dev/null || true
  fi
}

mesh_write_macos_app() {
  app="$(mesh_app_dir)/Agent Mesh.app"
  macos="${app}/Contents/MacOS"
  mkdir -p "$macos" "${app}/Contents/Resources"
  cat >"${app}/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>
  <string>Agent Mesh</string>
  <key>CFBundleDisplayName</key>
  <string>Agent Mesh</string>
  <key>CFBundleIdentifier</key>
  <string>com.agentmesh.node</string>
  <key>CFBundleVersion</key>
  <string>1</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleExecutable</key>
  <string>Agent Mesh</string>
  <key>NSHighResolutionCapable</key>
  <true/>
  <key>LSMinimumSystemVersion</key>
  <string>12.0</string>
</dict>
</plist>
EOF
  if [ -x "${BIN_DIR}/agent-mesh" ]; then
    cp "${BIN_DIR}/agent-mesh" "${macos}/Agent Mesh"
  else
    cp "${LIB_DIR}/run-desktop" "${macos}/Agent Mesh"
  fi
  chmod 0755 "${macos}/Agent Mesh"
  echo "wrote ${app}"
}

mesh_enable_linux_service() {
  role="$1"
  if [ -n "${AGENT_MESH_UNIT_DIR:-}" ]; then
    return 0
  fi
  if ! command -v systemctl >/dev/null 2>&1; then
    echo "systemctl not found; unit written, enable it by hand" >&2
    return 0
  fi
  unit="agent-mesh-${role}.service"
  if mesh_is_root; then
    systemctl daemon-reload || true
    if [ "$NO_START" -eq 1 ]; then
      systemctl enable "$unit" || return 1
    else
      systemctl enable --now "$unit" || return 1
    fi
  else
    systemctl --user daemon-reload || true
    if command -v loginctl >/dev/null 2>&1; then
      loginctl enable-linger "$(id -un)" 2>/dev/null || true
    fi
    if [ "$NO_START" -eq 1 ]; then
      systemctl --user enable "$unit" || return 1
    else
      systemctl --user enable --now "$unit" || return 1
    fi
  fi
}

mesh_enable_macos_service() {
  role="$1"
  label="com.agentmesh.${role}"
  plist="$(mesh_launchd_dir)/${label}.plist"
  if [ -n "${AGENT_MESH_LAUNCHD_DIR:-}" ]; then
    return 0
  fi
  if mesh_is_root; then
    domain="system"
  else
    domain="gui/$(id -u)"
  fi
  launchctl bootout "${domain}/${label}" 2>/dev/null || \
    launchctl unload "$plist" 2>/dev/null || true
  if launchctl bootstrap "$domain" "$plist" 2>/dev/null; then
    launchctl enable "${domain}/${label}" 2>/dev/null || true
    if [ "$NO_START" -eq 0 ]; then
      launchctl kickstart -k "${domain}/${label}" 2>/dev/null || true
    fi
  else
    if [ "$NO_START" -eq 1 ]; then
      launchctl load -w "$plist"
    else
      launchctl load -w "$plist"
    fi
  fi
}

mesh_install_service() {
  role="$1"
  if [ "$NO_SERVICE" -eq 1 ]; then
    return 0
  fi
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux)
      if ! command -v systemctl >/dev/null 2>&1 && [ -z "${AGENT_MESH_UNIT_DIR:-}" ]; then
        echo "systemd not found; skip service (binaries are installed)" >&2
        return 0
      fi
      mesh_write_systemd_unit "$role"
      if mesh_enable_linux_service "$role"; then
        echo "enabled agent-mesh-${role} systemd unit"
      else
        echo "unit written but not started; enable it with systemctl" >&2
      fi
      ;;
    darwin)
      mesh_write_launchd_plist "$role"
      mesh_enable_macos_service "$role"
      echo "enabled ${role} launchd service (RunAtLoad)"
      ;;
    *)
      echo "no boot service on $os" >&2
      ;;
  esac
}

mesh_install_desktop() {
  if [ "$WITH_DESKTOP" -eq 0 ]; then
    return 0
  fi
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux) mesh_write_linux_desktop ;;
    darwin) mesh_write_macos_app ;;
  esac
}

mesh_stop_service() {
  role="$1"
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux)
      if command -v systemctl >/dev/null 2>&1; then
        if mesh_is_root; then
          systemctl disable --now "agent-mesh-${role}.service" 2>/dev/null || true
        else
          systemctl --user disable --now "agent-mesh-${role}.service" 2>/dev/null || true
        fi
      fi
      rm -f "$(mesh_systemd_unit_dir)/agent-mesh-${role}.service"
      ;;
    darwin)
      label="com.agentmesh.${role}"
      plist="$(mesh_launchd_dir)/${label}.plist"
      if mesh_is_root; then
        domain="system"
      else
        domain="gui/$(id -u)"
      fi
      launchctl bootout "${domain}/${label}" 2>/dev/null || \
        launchctl unload "$plist" 2>/dev/null || true
      rm -f "$plist"
      ;;
  esac
}

mesh_uninstall() {
  role="$1"
  mesh_stop_service "$role"
  if [ "$role" = relay ]; then
    rm -f "${BIN_DIR}/mesh-relay" "${LIB_DIR}/run-relay"
  fi
  if [ "$role" = node ]; then
    rm -f "${BIN_DIR}/mesh-node" "${BIN_DIR}/agent-mesh" \
      "${LIB_DIR}/run-node" "${LIB_DIR}/run-desktop" \
      "${LIB_DIR}/agent-mesh.svg" "${LIB_DIR}/agent-mesh.png"
    rm -rf "${LIB_DIR}/icons"
    rm -f "$(mesh_desktop_dir)/agent-mesh.desktop"
    hicolor="$(mesh_hicolor_dir)"
    for size in 16 24 32 48 64 128 256 512; do
      rm -f "${hicolor}/${size}x${size}/apps/agent-mesh.png"
    done
    rm -f "${hicolor}/scalable/apps/agent-mesh.svg"
    rm -rf "$(mesh_app_dir)/Agent Mesh.app"
  fi
  echo "removed ${role} binaries and services"
  if [ "$PURGE" -eq 1 ]; then
    rm -rf "$STATE_DIR"
    echo "removed ${STATE_DIR}"
  else
    echo "kept ${STATE_DIR} (pass --purge to delete keys and config)"
  fi
}

mesh_install_bins_from() {
  src="$1"
  role="$2"
  mkdir -p "$BIN_DIR"
  if [ "$role" = relay ]; then
    bin="$(mesh_find_file "$src" mesh-relay relay)" || {
      echo "archive has no relay binary" >&2
      exit 1
    }
    install -m 0755 "$bin" "${BIN_DIR}/mesh-relay"
    echo "installed ${BIN_DIR}/mesh-relay"
  fi
  if [ "$role" = node ]; then
    bin="$(mesh_find_file "$src" mesh-node node)" || {
      echo "archive has no node binary" >&2
      exit 1
    }
    install -m 0755 "$bin" "${BIN_DIR}/mesh-node"
    echo "installed ${BIN_DIR}/mesh-node"
    ctl="$(mesh_find_file "$src" mesh-ctl ctl || true)"
    if [ -n "$ctl" ]; then
      install -m 0755 "$ctl" "${BIN_DIR}/mesh-ctl"
      echo "installed ${BIN_DIR}/mesh-ctl"
    fi
    gui="$(mesh_find_file "$src" agent-mesh || true)"
    if [ "$WITH_DESKTOP" -eq 1 ] && [ -n "$gui" ]; then
      install -m 0755 "$gui" "${BIN_DIR}/agent-mesh"
      echo "installed ${BIN_DIR}/agent-mesh"
    fi
    # Multi-size launcher icons shipped next to binaries in the release archive.
    icons="$(mesh_find_dir "$src" icons || true)"
    if [ -n "$icons" ] && [ -d "${icons}/16x16/apps" ]; then
      mkdir -p "$LIB_DIR" "${LIB_DIR}/icons"
      cp -a "$icons"/. "${LIB_DIR}/icons/"
      echo "installed ${LIB_DIR}/icons"
    fi
  fi
}

mesh_try_urls() {
  dest="$1"
  shift
  for url in "$@"; do
    [ -n "$url" ] || continue
    echo "downloading ${url}"
    if mesh_fetch "$url" "$dest"; then
      return 0
    fi
    echo "not found: ${url}" >&2
  done
  return 1
}

mesh_download_role() {
  role="$1"
  plat="$2"
  dest="$3"
  repo="$4"
  ver="$5"
  base="https://github.com/${repo}/releases"
  unversioned="${base}/latest/download/agent-mesh-${role}_${plat}.tar.gz"
  versioned=""
  combined=""
  combined_latest="${base}/latest/download/agent-mesh_${plat}.tar.gz"
  if [ "$ver" != latest ] && [ -n "$ver" ]; then
    versioned="${base}/download/${ver}/agent-mesh-${role}_${ver}_${plat}.tar.gz"
    combined="${base}/download/${ver}/agent-mesh_${ver}_${plat}.tar.gz"
  elif tag="$(mesh_resolve_latest "$repo")"; then
    VERSION="$tag"
    versioned="${base}/download/${tag}/agent-mesh-${role}_${tag}_${plat}.tar.gz"
    combined="${base}/download/${tag}/agent-mesh_${tag}_${plat}.tar.gz"
  fi
  mesh_try_urls "$dest" "$unversioned" "$versioned" "$combined_latest" "$combined"
}

mesh_build_from_source() {
  role="$1"
  dest_dir="$2"
  repo="$3"
  echo "no GitHub Release asset; building ${role} from source" >&2
  mesh_need git
  mesh_need go
  if [ "$role" = relay ]; then
    mesh_need npm
  fi
  src="${TMPDIR:-/tmp}/agent-mesh-src-$$"
  rm -rf "$src"
  git clone --depth 1 "https://github.com/${repo}.git" "$src"
  if [ "$role" = relay ]; then
    make -C "$src" relay
    install -m 0755 "${src}/bin/relay" "${dest_dir}/mesh-relay"
  else
    make -C "$src" node ctl
    install -m 0755 "${src}/bin/node" "${dest_dir}/mesh-node"
    install -m 0755 "${src}/bin/ctl" "${dest_dir}/mesh-ctl"
    if [ "$WITH_DESKTOP" -eq 1 ]; then
      if command -v npm >/dev/null 2>&1 && make -C "$src" desktop; then
        install -m 0755 "${src}/bin/agent-mesh" "${dest_dir}/agent-mesh"
      else
        echo "skip desktop GUI (need npm to embed the web UI); launcher will open MESH_RELAY in a browser" >&2
      fi
    fi
  fi
  rm -rf "$src"
}

mesh_obtain_bins() {
  role="$1"
  tmp="$2"
  repo="$3"
  plat="$(mesh_os_arch)"
  stage="${tmp}/extract"
  mkdir -p "$stage"
  if [ -n "$BUNDLE" ]; then
    echo "using local bundle ${BUNDLE}"
    tar -C "$stage" -xzf "$BUNDLE"
    mesh_install_bins_from "$stage" "$role"
    return 0
  fi
  asset="${tmp}/asset.tar.gz"
  if mesh_download_role "$role" "$plat" "$asset" "$repo" "$VERSION"; then
    tar -C "$stage" -xzf "$asset"
    mesh_install_bins_from "$stage" "$role"
    return 0
  fi
  echo "could not download a ${role} release for ${repo} (${plat})." >&2
  echo "publish a v* GitHub Release, or install Go 1.25+ (and Node 20+ for relay) to build from source." >&2
  echo "GitHub API is not required; assets are fetched from /releases/latest/download/." >&2
  echo "mirror: AGENT_MESH_MIRROR=https://ghfast.top" >&2
  mesh_build_from_source "$role" "$stage" "$repo"
  mesh_install_bins_from "$stage" "$role"
}

mesh_next_steps() {
  role="$1"
  echo
  echo "state: ${STATE_DIR}"
  if [ "$role" = relay ]; then
    echo "edit ${STATE_DIR}/relay.env then restart the service."
    if [ "${GENERATED_PASSWORD:-0}" -eq 1 ]; then
      echo "generated MESH_PASSWORD=${PASSWORD}"
    fi
    echo "web UI listens on ${LISTEN:-0.0.0.0:8443} (see MESH_LISTEN)."
  else
    echo "MESH_RELAY=${RELAY_URL}"
    echo "修改中继: 编辑 ${STATE_DIR}/node.env 后重启节点服务"
    if [ "$WITH_DESKTOP" -eq 0 ]; then
      echo "headless 节点，无桌面入口"
    fi
  fi
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  if [ "$os" = linux ]; then
    if mesh_is_root; then
      echo "service: systemctl status agent-mesh-${role}"
    else
      echo "service: systemctl --user status agent-mesh-${role}"
    fi
  elif [ "$os" = darwin ]; then
    echo "service: launchctl print gui/$(id -u)/com.agentmesh.${role} 2>/dev/null || launchctl print system/com.agentmesh.${role}"
  fi
  case ":${PATH}:" in
    *":${BIN_DIR}:"*) ;;
    *) echo "add ${BIN_DIR} to PATH if the binaries are not found" ;;
  esac
}

agent_mesh_install_main() {
  role="${AGENT_MESH_ROLE:-}"
  if [ "$role" != relay ] && [ "$role" != node ]; then
    echo "AGENT_MESH_ROLE must be relay or node" >&2
    exit 2
  fi
  mesh_parse_args "$@"
  mesh_init_paths
  mkdir -p "$STATE_DIR"
  chmod 0700 "$STATE_DIR" 2>/dev/null || true
  if [ "$UNINSTALL" -eq 1 ]; then
    mesh_uninstall "$role"
    exit 0
  fi
  if [ "$role" = node ]; then
    mesh_resolve_desktop
    mesh_prompt_relay
  fi
  REPO="$(mesh_detect_repo)"
  if [ -z "$REPO" ]; then
    echo "set AGENT_MESH_REPO=owner/name (or --repo)" >&2
    exit 1
  fi
  mesh_need tar
  mesh_need mktemp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  mesh_obtain_bins "$role" "$tmp" "$REPO"
  if [ "$role" = relay ]; then
    mesh_write_relay_env
  else
    mesh_write_node_env
  fi
  mesh_write_wrappers "$role"
  mesh_install_service "$role"
  if [ "$role" = node ]; then
    mesh_install_desktop
  fi
  mesh_next_steps "$role"
}
