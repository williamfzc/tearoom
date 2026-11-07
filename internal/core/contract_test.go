// Contract tests prove the Store contract against the shipped local store;
// the HTTP client runs the same battery from the transport tests.
package core_test

import (
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
)

func TestLocalStoreContract(t *testing.T) {
	core.StoreContract(t, func(t *testing.T) core.Store {
		st, err := store.Init(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return st
	})
}
