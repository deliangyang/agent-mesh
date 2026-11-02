.PHONY: all binaries web node relay ctl desktop desktop-windows test supervisor setup dist node-windows winres

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOFLAGS ?= -trimpath -ldflags "-s -w"

all: binaries

binaries: node relay ctl

web:
	cd internal/relay/web && npm ci && npm run build
	touch internal/relay/web/dist/.gitkeep

relay: web
	mkdir -p bin
	CGO_ENABLED=0 go build $(GOFLAGS) -o bin/relay ./cmd/relay

node:
	mkdir -p bin
	CGO_ENABLED=0 go build $(GOFLAGS) -o bin/node ./cmd/node

ctl:
	mkdir -p bin
	CGO_ENABLED=0 go build $(GOFLAGS) -o bin/ctl ./cmd/ctl

# Cross-compile node + ctl for Windows (amd64 and arm64).
# Node is a GUI/tray app (-H windowsgui): double-click keeps it running.
WINNODEFLAGS ?= -trimpath -ldflags "-s -w -H windowsgui"
winres:
	cd scripts/winres && go run .
node-windows:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(WINNODEFLAGS) -o bin/mesh-node-windows-amd64.exe ./cmd/node
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -o bin/mesh-ctl-windows-amd64.exe ./cmd/ctl
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build $(WINNODEFLAGS) -o bin/mesh-node-windows-arm64.exe ./cmd/node
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build $(GOFLAGS) -o bin/mesh-ctl-windows-arm64.exe ./cmd/ctl

desktop: web
	mkdir -p bin
	CGO_ENABLED=0 go build $(GOFLAGS) -o bin/agent-mesh ./desktop

# Windows WebView2 shell (Wails). Requires CGO + a C compiler on Windows (e.g. mingw/zig).
# Vue still talks HTTP/WS to the local node; the webview only hosts that origin.
desktop-windows: web
	mkdir -p bin
	CGO_ENABLED=1 go build $(GOFLAGS) -o bin/agent-mesh.exe ./desktop

setup:
	./scripts/setup.sh

dist:
	VERSION=$(VERSION) ./scripts/build-release.sh

supervisor: node
	./scripts/setup.sh --no-install --supervisor

test:
	go test $$(go list ./... | grep -v /desktop)
	cd internal/relay/web && (test -d node_modules/vue || npm ci) && npm test
	./scripts/install_test.sh
