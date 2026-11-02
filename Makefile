.PHONY: all binaries web node relay ctl desktop test supervisor setup dist

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

desktop:
	mkdir -p bin
	cd desktop && wails build
	cp -f desktop/build/bin/agent-mesh bin/agent-mesh

setup:
	./scripts/setup.sh

dist:
	VERSION=$(VERSION) ./scripts/build-release.sh

supervisor: node
	./scripts/setup.sh --no-install --supervisor

test:
	go test $$(go list ./... | grep -v /desktop)
	cd internal/relay/web && (test -d node_modules/vue || npm ci) && npm test
