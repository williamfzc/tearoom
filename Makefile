GO      ?= go
BIN     := bin

# This machine cannot reach proxy.golang.org; default to the goproxy.cn mirror
# for every go invocation, overridable with `make GOPROXY=https://proxy.golang.org`.
export GOPROXY ?= https://goproxy.cn,direct

# Release binaries stay static. macOS tests use the system linker because the Go
# 1.22 internal linker can emit test executables without the LC_UUID required by
# current macOS. Other platforms keep the ordinary Go test path.
ifeq ($(shell uname -s),Darwin)
TEST_ENV := CGO_ENABLED=1
TEST_FLAGS := -ldflags=-linkmode=external
endif

.PHONY: all build cli test test-race target-room target-replay check-evals docs fmt vet clean check

all: build

build: cli

cli:
	CGO_ENABLED=0 $(GO) build -o $(BIN)/tearoom ./cmd/tearoom

test:
	$(TEST_ENV) $(GO) test $(TEST_FLAGS) ./...

# The store tests exercise concurrent writers under the same file lock.
test-race:
	$(TEST_ENV) $(GO) test $(TEST_FLAGS) -race ./...

# The evaluation set must stay a bidirectional story<->proof map: a marker
# removed from a script or a row removed from the registry fails the check.
check-evals:
	@./scripts/check-evals.sh

# E1: the P0 Room contract through the shipped binary — explicit join over the
# file, SQLite, and HTTP backends, dense concurrent append, complete member
# history, pure reads, and idempotency.
target-room: cli
	@./scripts/target-room.sh

# E2: P0 replay across real processes without server-owned read progress —
# explicit eN boundaries, no session/cursor files, invocation-scoped wait, and
# consistent history across restarts.
target-replay: cli
	@./scripts/target-replay.sh

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

# Documentation rot is silent: a moved file leaves a link that still looks fine in
# a diff. Cheap to check, so it runs with everything else.
docs:
	@./scripts/check-docs.sh

check: fmt vet test test-race target-room target-replay check-evals docs

clean:
	rm -rf $(BIN)
