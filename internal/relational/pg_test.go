// The same contract battery as the SQLite suite, against a real PostgreSQL
// when the operator provides one. Gated by TEAROOM_TEST_PG so `make check`
// runs anywhere and the PostgreSQL suite runs wherever a database exists.
// Every subtest works inside a name-prefix isolation wrapper so parallel
// batteries share one database without colliding on room names.
package relational_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/relational"
)

func pgDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEAROOM_TEST_PG")
	if dsn == "" {
		t.Skip("TEAROOM_TEST_PG not set; skipping the PostgreSQL suite")
	}
	return dsn
}

// prefixStore scopes one shared workspace database to a random name prefix:
// every room this test creates is prefixed on the way in and un-prefixed on
// the way out, so batteries on one database never collide.
type prefixStore struct {
	inner  core.Store
	prefix string
}

func (p *prefixStore) CreateRoom(ctx context.Context, name, parent string) (*core.Room, error) {
	// A blank name must reach the store unmodified: the prefix exists to
	// isolate names, not to make an invalid one valid.
	innerName := p.prefix + name
	if strings.TrimSpace(name) == "" {
		innerName = name
	}
	innerParent := ""
	if parent != "" {
		innerParent = p.prefix + parent
	}
	room, err := p.inner.CreateRoom(ctx, innerName, innerParent)
	if err != nil {
		return nil, err
	}
	room.Name, room.Parent = strings.TrimPrefix(room.Name, p.prefix), strings.TrimPrefix(room.Parent, p.prefix)
	return room, nil
}

func (p *prefixStore) Rooms(ctx context.Context) ([]core.Room, error) {
	rooms, err := p.inner.Rooms(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]core.Room, 0, len(rooms))
	for _, room := range rooms {
		name := strings.TrimPrefix(room.Name, p.prefix)
		if name == room.Name {
			continue
		}
		room.Name = name
		room.Parent = strings.TrimPrefix(room.Parent, p.prefix)
		out = append(out, room)
	}
	return out, nil
}

func (p *prefixStore) Read(ctx context.Context, room string, after uint64) ([]core.Record, error) {
	records, err := p.inner.Read(ctx, p.prefix+room, after)
	if err != nil {
		return nil, err
	}
	for i := range records {
		records[i].Room = strings.TrimPrefix(records[i].Room, p.prefix)
	}
	return records, nil
}

func (p *prefixStore) Append(ctx context.Context, room, author string, request core.AppendRequest) (*core.Record, error) {
	record, err := p.inner.Append(ctx, p.prefix+room, author, request)
	if err != nil {
		return nil, err
	}
	record.Room = strings.TrimPrefix(record.Room, p.prefix)
	return record, nil
}

func newPGStore(t *testing.T) core.Store {
	t.Helper()
	db, err := relational.OpenPostgres(pgDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := relational.Migrate(db); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	prefix := "pg" + hex.EncodeToString(raw) + "-"
	inner, err := relational.New(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rooms, _ := inner.Rooms(context.Background())
		for _, room := range rooms {
			if !strings.HasPrefix(room.Name, prefix) {
				continue
			}
			_ = db.Exec("DELETE FROM append_idempotency WHERE room_id = ?", room.Name).Error
			_ = db.Exec("DELETE FROM events WHERE room_id = ?", room.Name).Error
			_ = db.Exec("DELETE FROM rooms WHERE name = ?", room.Name).Error
		}
	})
	return &prefixStore{inner: inner, prefix: prefix}
}

func TestPostgreSQLStoreContract(t *testing.T) {
	ctx := context.Background()
	// A reachability check up front gives a clear failure when the DSN is
	// wrong instead of a confusing per-subtest failure.
	db, err := relational.OpenPostgres(pgDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		t.Fatalf("cannot reach PostgreSQL at TEAROOM_TEST_PG: %v", err)
	}
	core.StoreContract(t, newPGStore)
}
