GO      ?= go
BIN     := bin

# Release binaries stay static. macOS tests use the system linker because the Go
# 1.22 internal linker can emit test executables without the LC_UUID required by
# current macOS. Other platforms keep the ordinary Go test path.
ifeq ($(shell uname -s),Darwin)
TEST_ENV := CGO_ENABLED=1
TEST_FLAGS := -ldflags=-linkmode=external
endif

.PHONY: all build cli test test-race smoke session-smoke docs fmt vet clean check

all: build

build: cli

cli:
	CGO_ENABLED=0 $(GO) build -o $(BIN)/tearoom ./cmd/tearoom

test:
	$(TEST_ENV) $(GO) test $(TEST_FLAGS) ./...

# The store tests exercise concurrent writers under the same file lock.
test-race:
	$(TEST_ENV) $(GO) test $(TEST_FLAGS) -race ./...

# End-to-end check of the M0 hypothesis: two agents hand off with no human relay.
smoke: cli
	@./scripts/smoke.sh

session-smoke: cli
	@./scripts/session-smoke.sh

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

# Documentation rot is silent: a moved file leaves a link that still looks fine in
# a diff. Cheap to check, so it runs with everything else.
docs:
	@./scripts/check-docs.sh

check: fmt vet test smoke session-smoke docs

clean:
	rm -rf $(BIN)
