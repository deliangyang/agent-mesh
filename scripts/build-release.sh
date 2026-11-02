#!/bin/sh
# Cross-compile node, relay, and ctl into dist/ archives.
# Each role gets a versioned archive and a version-less copy so
# GitHub /releases/latest/download/agent-mesh-<role>_<os>_<arch>.tar.gz works
# without calling the GitHub API.
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="${ROOT}/dist"
TARGETS="${TARGETS:-linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64}"

cd "$ROOT"
make web

mkdir -p "$OUT"
ldflags="-s -w -X main.version=${VERSION}"
host_os="$(go env GOOS)"
host_arch="$(go env GOARCH)"

pack_files() {
  archive="$1"
  src="$2"
  shift 2
  (
    cd "$src"
    if [ "${archive%.zip}" != "$archive" ]; then
      if command -v zip >/dev/null 2>&1; then
        zip -qr "$archive" "$@"
      else
        python3 - "$archive" "$@" <<'PY'
import sys, zipfile
archive = sys.argv[1]
with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as z:
    for name in sys.argv[2:]:
        z.write(name)
PY
      fi
    else
      tar -czf "$archive" "$@"
    fi
  )
}

build_one() {
  goos="$1"
  goarch="$2"
  ext=""
  [ "$goos" = windows ] && ext=".exe"
  plat="${goos}_${goarch}"
  work="${OUT}/.build_${plat}"
  rm -rf "$work"
  mkdir -p "$work"
  echo "building ${goos}/${goarch}"
  node_ldflags="$ldflags"
  [ "$goos" = windows ] && node_ldflags="$ldflags -H windowsgui"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$node_ldflags" -o "${work}/mesh-node${ext}" ./cmd/node
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$ldflags" -o "${work}/mesh-relay${ext}" ./cmd/relay
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$ldflags" -o "${work}/mesh-ctl${ext}" ./cmd/ctl
  cp "$ROOT/README.md" "$work/"
  [ -f "$ROOT/LICENSE" ] && cp "$ROOT/LICENSE" "$work/"
  if [ "$goos" = "$host_os" ] && [ "$goarch" = "$host_arch" ] && [ -f "$ROOT/bin/agent-mesh${ext}" ]; then
    cp "$ROOT/bin/agent-mesh${ext}" "${work}/agent-mesh${ext}"
  fi

  relay_files="mesh-relay${ext} README.md"
  node_files="mesh-node${ext} mesh-ctl${ext} README.md"
  all_files="mesh-node${ext} mesh-relay${ext} mesh-ctl${ext} README.md"
  [ -f "$work/LICENSE" ] && relay_files="${relay_files} LICENSE" && node_files="${node_files} LICENSE" && all_files="${all_files} LICENSE"
  [ -f "$work/agent-mesh${ext}" ] && node_files="${node_files} agent-mesh${ext}" && all_files="${all_files} agent-mesh${ext}"

  if [ "$goos" = windows ]; then
    pack_files "${OUT}/agent-mesh-relay_${VERSION}_${plat}.zip" "$work" $relay_files
    pack_files "${OUT}/agent-mesh-node_${VERSION}_${plat}.zip" "$work" $node_files
    pack_files "${OUT}/agent-mesh_${VERSION}_${plat}.zip" "$work" $all_files
    cp "${OUT}/agent-mesh-relay_${VERSION}_${plat}.zip" "${OUT}/agent-mesh-relay_${plat}.zip"
    cp "${OUT}/agent-mesh-node_${VERSION}_${plat}.zip" "${OUT}/agent-mesh-node_${plat}.zip"
    cp "${OUT}/agent-mesh_${VERSION}_${plat}.zip" "${OUT}/agent-mesh_${plat}.zip"
  else
    pack_files "${OUT}/agent-mesh-relay_${VERSION}_${plat}.tar.gz" "$work" $relay_files
    pack_files "${OUT}/agent-mesh-node_${VERSION}_${plat}.tar.gz" "$work" $node_files
    pack_files "${OUT}/agent-mesh_${VERSION}_${plat}.tar.gz" "$work" $all_files
    cp "${OUT}/agent-mesh-relay_${VERSION}_${plat}.tar.gz" "${OUT}/agent-mesh-relay_${plat}.tar.gz"
    cp "${OUT}/agent-mesh-node_${VERSION}_${plat}.tar.gz" "${OUT}/agent-mesh-node_${plat}.tar.gz"
    cp "${OUT}/agent-mesh_${VERSION}_${plat}.tar.gz" "${OUT}/agent-mesh_${plat}.tar.gz"
  fi
  rm -rf "$work"
}

for spec in $TARGETS; do
  build_one "${spec%/*}" "${spec#*/}"
done

echo "archives in ${OUT}"
ls -1 "$OUT"
