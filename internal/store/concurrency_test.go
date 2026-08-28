// Cross-process tests prove flock serializes the complete append decision.
package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
)

// TestConcurrentProcessesAssignOneSequence proves flock protects the complete
// cursor-check-and-append operation across independent OS processes.
func TestConcurrentProcessesAssignOneSequence(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	file := newTestStore(t)
	const writers = 10
	ids := make([]string, writers)
	for i := range ids {
		ids[i] = fmt.Sprintf("actor-%d", i)
	}
	join(t, file, ids...)

	start := make(chan struct{})
	results := make([]string, writers)
	var group sync.WaitGroup
	for i, actor := range ids {
		group.Add(1)
		go func(i int, actor string) {
			defer group.Done()
			<-start
			command := exec.Command(os.Args[0], "-test.run=TestAppendProcessHelper", "--", file.Root(), actor)
			command.Env = append(os.Environ(), "TEAROOM_APPEND_HELPER=1")
			output, _ := command.CombinedOutput()
			results[i] = strings.TrimSpace(string(output))
		}(i, actor)
	}
	close(start)
	group.Wait()

	wins, stale := 0, 0
	for _, result := range results {
		switch result {
		case "WON":
			wins++
		case "STALE":
			stale++
		default:
			t.Fatalf("unexpected helper result %q", result)
		}
	}
	if wins != 1 || stale != writers-1 {
		t.Fatalf("wins=%d stale=%d results=%v", wins, stale, results)
	}

	feed, err := file.Read(context.Background(), ids[0], 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if feed.Cursor != 1 || len(feed.Events) != 1 {
		t.Fatalf("feed after race = %+v", feed)
	}
}

func TestAppendProcessHelper(t *testing.T) {
	if os.Getenv("TEAROOM_APPEND_HELPER") != "1" {
		return
	}
	separator := 0
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator == 0 || len(os.Args) != separator+3 {
		fmt.Print("BAD_ARGS")
		os.Exit(2)
	}
	root, actor := os.Args[separator+1], os.Args[separator+2]
	file, err := OpenAt(filepath.Clean(root))
	if err != nil {
		fmt.Print("OPEN_ERROR")
		os.Exit(2)
	}
	_, err = file.AppendEvent(context.Background(), core.AppendInput{
		Actor: actor, Expected: 0, Type: "race", Body: actor, Key: actor,
	})
	switch {
	case err == nil:
		fmt.Print("WON")
	case errors.Is(err, core.ErrStaleWrite):
		fmt.Print("STALE")
	default:
		fmt.Print("ERROR")
	}
	os.Exit(0)
}
