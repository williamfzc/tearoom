// Contract tests prove the Store contract against the file backend, the only
// implementation present in this wave; every later backend runs the same
// battery from its own tests.
package core_test

import (
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
)

func TestFileStoreContract(t *testing.T) {
	core.StoreContract(t, func(t *testing.T) core.Store {
		st, err := store.Init(t.TempDir(), store.BackendFile)
		if err != nil {
			t.Fatal(err)
		}
		return st
	})
}
