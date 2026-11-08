CARGO   ?= cargo
BIN     := bin
RUST_MANIFEST := rust/Cargo.toml
RELEASE_TAG ?= v0.0.0-dev
RELEASE_DIR ?= dist/releases/$(RELEASE_TAG)
RELEASE_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
RELEASE_SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || date +%s)
.PHONY: all build cli test target-room target-replay check-evals line-budget docs fmt vet clean check release-test package package-check release-target release-check release-upload

all: build

build: cli

cli:
	$(CARGO) build --locked --manifest-path $(RUST_MANIFEST) --target-dir rust/target
	mkdir -p $(BIN)
	cp rust/target/debug/tearoom $(BIN)/tearoom

test:
	$(CARGO) test --locked --manifest-path $(RUST_MANIFEST) --target-dir rust/target

# The evaluation set must stay a bidirectional story<->proof map: a marker
# removed from a script or a row removed from the registry fails the check.
check-evals:
	@./scripts/check-evals.sh

# The line-budget ratchet: production files over 300 lines fail only when
# they are new or grew past their recorded count in scripts/line-baseline.txt.
# The baseline only ever shrinks; deleting a line is how a split pays debt off.
line-budget:
	@./scripts/check-line-budget.sh

# E1: the room-tree contract through the shipped binary — named rooms under
# parents, per-room dense sequences, concurrent append, idempotent retries,
# tails, subtrees, the skeleton, missing signatures, and the loopback HTTP
# leg.
target-room: cli
	@./scripts/target-room.sh

# E2: replay across real processes without server-owned read progress —
# explicit eN boundaries, no session/cursor files, and consistent history
# across restarts.
target-replay: cli
	@./scripts/target-replay.sh

fmt:
	$(CARGO) fmt --manifest-path $(RUST_MANIFEST) -- --check

vet:
	$(CARGO) clippy --locked --manifest-path $(RUST_MANIFEST) --all-targets -- -D warnings

# Documentation rot is silent: a moved file leaves a link that still looks fine in
# a diff. Cheap to check, so it runs with everything else.
docs:
	@./scripts/check-docs.sh

release-test:
	python3 scripts/test-release-package.py

check: fmt vet test target-room target-replay check-evals line-budget docs release-test

# Each target must be built on a matching native runner, then assembled.
release-target:
	./scripts/release-package.sh build-target --tag "$(RELEASE_TAG)" --output "$(RELEASE_DIR)" --target "$(RELEASE_TARGET)" --cargo "$(CARGO)" --commit "$(RELEASE_COMMIT)" --source-date-epoch "$(RELEASE_SOURCE_DATE_EPOCH)"

package:
	./scripts/release-package.sh assemble --tag "$(RELEASE_TAG)" --output "$(RELEASE_DIR)" --commit "$(RELEASE_COMMIT)" --source-date-epoch "$(RELEASE_SOURCE_DATE_EPOCH)"

package-check: release-check
	@test -n "$(RELEASE_COMPARE_DIR)" || { echo "release: set RELEASE_COMPARE_DIR to a second assembled build" >&2; exit 1; }
	./scripts/release-package.sh check --tag "$(RELEASE_TAG)" --output "$(RELEASE_COMPARE_DIR)" --commit "$(RELEASE_COMMIT)"
	cmp "$(RELEASE_DIR)/SHA256SUMS" "$(RELEASE_COMPARE_DIR)/SHA256SUMS"

release-check:
	./scripts/release-package.sh check --tag "$(RELEASE_TAG)" --output "$(RELEASE_DIR)" --commit "$(RELEASE_COMMIT)"

release-upload:
	./scripts/release-package.sh upload --tag "$(RELEASE_TAG)" --output "$(RELEASE_DIR)" --endpoint "$(R2_ENDPOINT)" --bucket "$(R2_BUCKET)" --public-base-url "$(R2_PUBLIC_BASE_URL)" --commit "$(RELEASE_COMMIT)"

clean:
	rm -rf $(BIN)
