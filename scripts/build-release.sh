#!/bin/sh
# Cross-compile node, relay, and ctl into dist/ archives.
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="${ROOT}/dist"
TARGETS="${TARGETS:-linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64}"

cd "$ROOT"
make web

mkdir -p "$OUT"
ldflags="-s -w -X main.version=${VERSION}"

build_one() {
  goos="$1"
  goarch="$2"
  ext=""
  [ "$goos" = windows ] && ext=".exe"
  dir="${OUT}/agent-mesh_${VERSION}_${goos}_${goarch}"
  rm -rf "$dir"
  mkdir -p "$dir"
  echo "building ${goos}/${goarch}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$ldflags" -o "${dir}/node${ext}" ./cmd/node
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$ldflags" -o "${dir}/relay${ext}" ./cmd/relay
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$ldflags" -o "${dir}/ctl${ext}" ./cmd/ctl
  cp "$ROOT/README.md" "$ROOT/LICENSE" "$dir/" 2>/dev/null || cp "$ROOT/README.md" "$dir/"
  if [ "$goos" = windows ]; then
    (cd "$OUT" && zip -qr "agent-mesh_${VERSION}_${goos}_${goarch}.zip" "$(basename "$dir")")
  else
    tar -C "$OUT" -czf "${dir}.tar.gz" "$(basename "$dir")"
  fi
  rm -rf "$dir"
}

for spec in $TARGETS; do
  build_one "${spec%/*}" "${spec#*/}"
done

echo "archives in ${OUT}"
ls -1 "$OUT"
