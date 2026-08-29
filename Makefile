GO      ?= go
BIN     := bin

# Release binaries stay static. macOS tests use the system linker because the Go
# 1.22 internal linker can emit test executables without the LC_UUID required by
# current macOS. Other platforms keep the ordinary Go test path.
ifeq ($(shell uname -s),Darwin)
TEST_ENV := CGO_ENABLED=1
TEST_FLAGS := -ldflags=-linkmode=external
endif

.PHONY: all build cli test test-race smoke smoke-sqlite session-smoke harness-demo game-demo worktree-demo hosted-demo pg-smoke check-evals docs fmt vet clean check

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

# The relational backend must answer the same invariants through the same
# shipped binary; its own suite proves that end to end.
smoke-sqlite: cli
	@./scripts/smoke-sqlite.sh

session-smoke: cli
	@./scripts/session-smoke.sh

# The harness workflow user story: planner, generator, and evaluator agents
# run one plan/generate/evaluate loop through the shipped binary.
harness-demo: cli
	@./scripts/harness-demo.sh

# The game protocol user story: one Werewolf round over directed role Events.
game-demo: cli
	@./scripts/game-demo.sh

# The one-Room-across-worktrees user story: two real git worktrees share one
# served Room, cursors per worktree, handoffs across them by watch.
worktree-demo: cli
	@./scripts/worktree-demo.sh

# The hosted stories: a multi-tenant server, admin-issued revocable
# credentials, actor binding, and the 401/403 matrix. Dev mode, SQLite.
hosted-demo: cli
	@./scripts/hosted-demo.sh

# The same invariants over a real PostgreSQL; skips with a notice unless
# TEAROOM_TEST_PG (a postgres:// DSN) is set.
pg-smoke: cli
	@./scripts/pg-smoke.sh

# The evaluation set must stay a bidirectional story<->proof map: a marker
# removed from a script or a row removed from the registry fails the check.
check-evals:
	@./scripts/check-evals.sh

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

# Documentation rot is silent: a moved file leaves a link that still looks fine in
# a diff. Cheap to check, so it runs with everything else.
docs:
	@./scripts/check-docs.sh

check: fmt vet test smoke smoke-sqlite session-smoke harness-demo game-demo worktree-demo hosted-demo pg-smoke check-evals docs

clean:
	rm -rf $(BIN)
