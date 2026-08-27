// Command tea-server exposes a workspace over HTTP and serves the console.
//
// The same file store the CLI uses is wrapped here, so a team can point remote
// agents at one machine without changing anything about how they call tea.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/williamfzc/tearoom/internal/store"
	"github.com/williamfzc/tearoom/internal/transport"
)

func main() {
	addr := flag.String("addr", ":8787", "listen address")
	dir := flag.String("dir", ".", "directory containing the .tearoom workspace")
	webDir := flag.String("web", "", "directory of console assets (optional)")
	flag.Parse()

	st, err := store.Open(*dir)
	if err != nil {
		// Refuse to start rather than silently creating a workspace. Auto-creating
		// one on a mistyped path would split agents across two stores, and the
		// symptom would be leases that mysteriously never conflict.
		log.Fatalf("no workspace under %s: run 'tea init' there first", *dir)
	}

	srv := transport.NewServer(st)
	srv.Now = time.Now
	if *webDir != "" {
		abs, err := filepath.Abs(*webDir)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := os.Stat(abs); err != nil {
			log.Fatalf("web assets not found at %s", abs)
		}
		srv.Assets = http.FileServer(http.Dir(abs))
	}

	fmt.Printf("tearoom serving %s on %s\n", st.Root(), *addr)
	if *webDir != "" {
		fmt.Printf("console at http://localhost%s/\n", *addr)
	}

	h := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Fatal(h.ListenAndServe())
}
