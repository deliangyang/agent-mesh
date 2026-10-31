.PHONY: all relay desktop test web

all: relay desktop

web:
	cd internal/relay/web && npm ci && npm run build

relay: web
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/relay ./cmd/relay

desktop:
	mkdir -p bin
	cd desktop && wails build
	cp -f desktop/build/bin/agent-mesh bin/agent-mesh

test:
	go test $$(go list ./... | grep -v /desktop)
