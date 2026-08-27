# CGO is off deliberately: it yields a static single binary, avoids linker
# variation across machines, and is the right default for a tool people install
# by dropping it on PATH.
export CGO_ENABLED=0

GO      ?= go
BIN     := bin
WEB     := web
WASMJS  := $(shell $(GO) env GOROOT)/misc/wasm/wasm_exec.js

.PHONY: all build cli server wasm test test-wasm test-race smoke docs fmt vet clean check

all: build

build: cli server wasm

cli:
	$(GO) build -o $(BIN)/tea ./cmd/tea

server:
	$(GO) build -o $(BIN)/tea-server ./cmd/tea-server

# The console is Go compiled to WebAssembly, so the view logic in internal/view is
# shared with the CLI rather than reimplemented in JavaScript.
wasm:
	GOOS=js GOARCH=wasm $(GO) build -o $(WEB)/tea.wasm ./cmd/tea-web
	@cp "$(WASMJS)" $(WEB)/wasm_exec.js 2>/dev/null || \
	 cp "$(shell $(GO) env GOROOT)/lib/wasm/wasm_exec.js" $(WEB)/wasm_exec.js

# Frontend tests run in node under GOOS=js. env -i is required: the real
# environment overflows the wasm command-line limit.
NODEDIR := $(shell dirname "$(shell command -v node)")
test-wasm:
	@if [ -z "$(NODEDIR)" ] || [ ! -x "$(NODEDIR)/node" ]; then \
		echo "skipping wasm tests: node not found"; \
	else \
		env -i PATH="/usr/bin:/bin:$(NODEDIR):$(dir $(shell command -v go))" HOME="$(HOME)" \
			GOOS=js GOARCH=wasm GOCACHE="$(shell go env GOCACHE)" GOMODCACHE="$(shell go env GOMODCACHE)" \
			go test -exec="$(shell go env GOROOT)/misc/wasm/go_js_wasm_exec" ./cmd/tea-web/; \
	fi

test:
	$(GO) test ./...

# The concurrency tests spawn real processes, so -race is worth the time here.
test-race:
	$(GO) test -race ./...

# End-to-end check of the M0 hypothesis: two agents hand off with no human relay.
smoke: cli
	@./scripts/smoke.sh

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

# Documentation rot is silent: a moved file leaves a link that still looks fine in
# a diff. Cheap to check, so it runs with everything else.
docs:
	@./scripts/check-docs.sh

check: fmt vet test test-wasm smoke docs

clean:
	rm -rf $(BIN) $(WEB)/tea.wasm $(WEB)/wasm_exec.js
