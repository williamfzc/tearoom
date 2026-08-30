GO      ?= go
BIN     := bin
RELEASE_TAG ?= v0.0.0-dev
RELEASE_DIR ?= dist/releases/$(RELEASE_TAG)
RELEASE_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
RELEASE_SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || date +%s)
# internal/version is the release version contract. Override this when the
# version package moves; release packaging remains otherwise unchanged.
VERSION_LDFLAGS ?= -s -w -X github.com/williamfzc/tearoom/internal/version.Version=$(RELEASE_TAG) -X github.com/williamfzc/tearoom/internal/version.Commit=$(RELEASE_COMMIT)

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

.PHONY: all build cli test test-race target-room target-replay check-evals docs fmt vet clean check package package-check release-check release-upload

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

# Release packaging is executable locally; the workflow only supplies the tag
# and R2 credentials. package-check builds all four targets and validates the
# archives, checksums, manifest, and SPDX document in a disposable directory.
package:
	./scripts/release-package.sh package --tag "$(RELEASE_TAG)" --output "$(RELEASE_DIR)" --go "$(GO)" --commit "$(RELEASE_COMMIT)" --source-date-epoch "$(RELEASE_SOURCE_DATE_EPOCH)" --version-ldflags "$(VERSION_LDFLAGS)"

package-check:
	@release_tmp=$$(mktemp -d "$${TMPDIR:-/tmp}/tearoom-package-check.XXXXXX"); \
	release_one="$$release_tmp/one"; release_two="$$release_tmp/two"; \
	trap 'rm -rf "$$release_tmp"' EXIT INT TERM; \
	./scripts/release-package.sh package --tag "$(RELEASE_TAG)" --output "$$release_one" --go "$(GO)" --commit "$(RELEASE_COMMIT)" --source-date-epoch "$(RELEASE_SOURCE_DATE_EPOCH)" --version-ldflags "$(VERSION_LDFLAGS)"; \
	./scripts/release-package.sh package --tag "$(RELEASE_TAG)" --output "$$release_two" --go "$(GO)" --commit "$(RELEASE_COMMIT)" --source-date-epoch "$(RELEASE_SOURCE_DATE_EPOCH)" --version-ldflags "$(VERSION_LDFLAGS)"; \
	./scripts/release-package.sh check --tag "$(RELEASE_TAG)" --output "$$release_one" --commit "$(RELEASE_COMMIT)"; \
	./scripts/release-package.sh check --tag "$(RELEASE_TAG)" --output "$$release_two" --commit "$(RELEASE_COMMIT)"; \
	cmp "$$release_one/SHA256SUMS" "$$release_two/SHA256SUMS"

release-check:
	./scripts/release-package.sh check --tag "$(RELEASE_TAG)" --output "$(RELEASE_DIR)" --commit "$(RELEASE_COMMIT)"

release-upload:
	./scripts/release-package.sh upload --tag "$(RELEASE_TAG)" --output "$(RELEASE_DIR)" --endpoint "$(R2_ENDPOINT)" --bucket "$(R2_BUCKET)" --public-base-url "$(R2_PUBLIC_BASE_URL)" --commit "$(RELEASE_COMMIT)"

clean:
	rm -rf $(BIN)
