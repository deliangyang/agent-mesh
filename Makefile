.PHONY: all relay desktop test web node supervisor

all: relay desktop

web:
	cd internal/relay/web && npm ci && npm run build
	touch internal/relay/web/dist/.gitkeep

relay: web
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/relay ./cmd/relay

node:
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/node ./cmd/node

desktop:
	mkdir -p bin
	cd desktop && wails build
	cp -f desktop/build/bin/agent-mesh bin/agent-mesh

supervisor: node
	mkdir -p $(HOME)/.agent-mesh
	test -f $(HOME)/.agent-mesh/supervisord.conf || cp deploy/supervisor/supervisord.conf $(HOME)/.agent-mesh/supervisord.conf
	supervisord -c $(HOME)/.agent-mesh/supervisord.conf || true
	supervisorctl -c $(HOME)/.agent-mesh/supervisord.conf reread
	supervisorctl -c $(HOME)/.agent-mesh/supervisord.conf update
	supervisorctl -c $(HOME)/.agent-mesh/supervisord.conf restart agent-mesh-node

test:
	go test $$(go list ./... | grep -v /desktop)
	cd internal/relay/web && node --experimental-strip-types --test src/events.test.ts src/ask.test.ts src/toolSummary.test.ts src/attachUpload.test.ts src/liveFlags.test.ts src/chatModel.test.ts
