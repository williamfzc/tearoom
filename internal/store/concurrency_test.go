package store

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

// TestConcurrentClaimSingleWinner is the test the product lives or dies by.
//
// Goroutines in one process would be a weak check, because a plain in-memory mutex
// would satisfy it while the real scenario — separate terminals, separate processes
// — still corrupted state. So this spawns actual processes racing on the same
// workspace, which is what flock exists to handle.
func TestConcurrentClaimSingleWinner(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	bin := buildRacer(t)
	dir := t.TempDir()
	if _, err := Init(dir, "race"); err != nil {
		t.Fatal(err)
	}

	const racers = 12
	root := filepath.Join(dir, Dir)

	// Pre-register agents so nobody is treated as stale and preempted mid-race,
	// which would legitimately allow more than one winner.
	f, err := OpenAt(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < racers; i++ {
		if _, err := f.UpsertAgent(ctx, agentName(i), "racer"); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]string, racers)

	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cmd := exec.Command(bin, root, agentName(idx), "src/**")
			<-start // release everyone at once to maximise overlap
			out, _ := cmd.CombinedOutput()
			results[idx] = strings.TrimSpace(string(out))
		}(i)
	}
	close(start)
	wg.Wait()

	won, lost, other := 0, 0, []string{}
	for _, r := range results {
		switch {
		case strings.HasPrefix(r, "WON"):
			won++
		case strings.HasPrefix(r, "CONFLICT"):
			lost++
		default:
			other = append(other, r)
		}
	}

	if won != 1 {
		t.Fatalf("%d processes acquired the same lease, want exactly 1 (lost=%d, other=%v)", won, lost, other)
	}
	if lost != racers-1 {
		t.Errorf("losers = %d, want %d; unexpected outcomes: %v", lost, racers-1, other)
	}

	// And the store must agree with the processes: exactly one lease on disk.
	leases, err := f.Leases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	live := 0
	for _, l := range leases {
		if !l.Expired(time.Now()) {
			live++
		}
	}
	if live != 1 {
		t.Errorf("store holds %d live leases, want 1", live)
	}
}

// TestConcurrentAppendNoLostEvents checks that racing replies all land. Reads and
// writes are not atomic as a pair unless the lock covers both, so a missing event
// here would mean read-modify-write is losing updates.
func TestConcurrentAppendNoLostEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	bin := buildRacer(t)
	dir := t.TempDir()
	f, err := Init(dir, "race")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	th, err := f.CreateThread(ctx, "seed", "concurrent replies", "")
	if err != nil {
		t.Fatal(err)
	}

	const writers = 10
	root := filepath.Join(dir, Dir)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cmd := exec.Command(bin, root, agentName(idx), "--reply", th.ID)
			<-start
			cmd.CombinedOutput()
		}(i)
	}
	close(start)
	wg.Wait()

	got, err := f.Thread(ctx, th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != writers {
		t.Fatalf("thread has %d events, want %d — writes were lost", len(got.Events), writers)
	}
	// Sequence numbers must be dense and unique, or the log cannot be trusted.
	seen := map[int]bool{}
	for _, e := range got.Events {
		if seen[e.Seq] {
			t.Errorf("duplicate seq %d", e.Seq)
		}
		seen[e.Seq] = true
	}
	for i := 1; i <= writers; i++ {
		if !seen[i] {
			t.Errorf("missing seq %d", i)
		}
	}
}

func TestConflictErrorUnwraps(t *testing.T) {
	err := error(&core.ConflictError{Resource: "src/**", Holder: "a", Expires: time.Now()})
	if !errors.Is(err, core.ErrConflict) {
		t.Error("ConflictError must satisfy errors.Is(err, ErrConflict) so callers can branch")
	}
}

func agentName(i int) string { return "racer" + string(rune('a'+i)) }

// buildRacer compiles a helper that performs one store operation and reports the
// outcome, so the test can run genuinely separate processes.
func buildRacer(t *testing.T) string {
	t.Helper()
	src := `package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
)

func main() {
	root, agent := os.Args[1], os.Args[2]
	f, err := store.OpenAt(root)
	if err != nil {
		fmt.Println("ERR", err)
		os.Exit(1)
	}
	ctx := context.Background()

	if os.Args[3] == "--reply" {
		if _, err := f.AppendEvent(ctx, os.Args[4], agent, "hello from "+agent, "", nil); err != nil {
			fmt.Println("ERR", err)
			os.Exit(1)
		}
		fmt.Println("REPLIED")
		return
	}

	if _, err := f.Claim(ctx, agent, os.Args[3], "", time.Hour); err != nil {
		var ce *core.ConflictError
		if errors.As(err, &ce) {
			fmt.Println("CONFLICT held by", ce.Holder)
			return
		}
		fmt.Println("ERR", err)
		os.Exit(1)
	}
	fmt.Println("WON", agent)
}
`
	// The helper has to live inside the module: Go forbids importing internal/
	// packages from a file outside the tree that owns them, so a temp dir in /tmp
	// will not compile.
	dir, err := os.MkdirTemp(moduleRoot(t), ".racer-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "racer")
	cmd := exec.Command("go", "build", "-o", bin, srcPath)
	cmd.Dir = moduleRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building racer failed: %v\n%s", err, out)
	}
	return bin
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
