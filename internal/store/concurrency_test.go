// Cross-process tests prove flock serializes the complete append decision:
// concurrent appends from independent OS processes all succeed and receive a
// dense, ordered sequence.
package store

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
)

func TestConcurrentProcessesDenseSequence(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	file := newTestStore(t)
	const writers = 10
	ids := make([]string, writers)
	for i := range ids {
		ids[i] = fmt.Sprintf("actor-%d", i)
	}
	admit(t, file, ids...)

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

	for i, result := range results {
		if !strings.HasPrefix(result, "OK:") {
			t.Fatalf("helper %d result %q", i, result)
		}
	}
	events, err := file.Read(context.Background(), ids[0], 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != writers {
		t.Fatalf("events = %d, want %d", len(events), writers)
	}
	for i, event := range events {
		if event.Seq != uint64(i+1) {
			t.Fatalf("sequence not dense across processes: event %d has seq %d", i, event.Seq)
		}
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
	st, err := Open(filepath.Clean(root))
	if err != nil {
		fmt.Print("OPEN_ERROR")
		os.Exit(2)
	}
	event, err := st.Append(context.Background(), actor, core.AppendRequest{
		Content: []byte(`"race"`),
	})
	if err != nil {
		fmt.Print("ERROR")
		os.Exit(2)
	}
	fmt.Printf("OK:%d", event.Seq)
	os.Exit(0)
}
