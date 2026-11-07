// migrate.go performs the one-time format-2/3 → format-4 migration: it reads
// a retired SQLite workspace (the v1 single-room model, or the SQLite room
// tree), writes the JSONL layout beside it, deletes the database, and
// rewrites the marker. The pure-Go SQLite driver exists in the release
// binary only for this path; after the migration the old layout is never
// read again.
package store

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/williamfzc/tearoom/internal/core"

	_ "github.com/glebarez/go-sqlite"
)

// migrateSQLWorkspace exports one format-2 or format-3 SQLite workspace into
// the JSONL layout. It is idempotent until the marker rewrite: a failed
// attempt leaves the marker untouched and the next open re-runs it.
func migrateSQLWorkspace(root string, format int) error {
	db, err := sql.Open("sqlite", filepath.Join(root, "room.db"))
	if err != nil {
		return fmt.Errorf("open the format-%d workspace database: %w", format, err)
	}
	defer db.Close()

	rooms, err := readSQLRooms(db, format)
	if err != nil {
		return err
	}
	records, err := readSQLRecords(db, format)
	if err != nil {
		return err
	}
	ledger, err := readSQLLedger(db, format)
	if err != nil {
		return err
	}

	roomsPath := filepath.Join(root, roomsDir)
	if err := os.MkdirAll(roomsPath, 0o755); err != nil {
		return err
	}
	for _, room := range rooms {
		dir := filepath.Join(roomsPath, room.Name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := writeJSONFile(filepath.Join(dir, "room.json"), room); err != nil {
			return err
		}
		if err := writeLines(filepath.Join(dir, "records.jsonl"), records[room.Name]); err != nil {
			return err
		}
		if err := writeLines(filepath.Join(dir, "idempotency.jsonl"), ledger[room.Name]); err != nil {
			return err
		}
		if err := syncDirectory(dir); err != nil {
			return err
		}
	}
	if err := syncDirectory(roomsPath); err != nil {
		return err
	}

	// The export is complete and durable: the database and its side files
	// are dead state now.
	if err := db.Close(); err != nil {
		return err
	}
	for _, name := range []string{"room.db", "room.db-wal", "room.db-shm"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := syncDirectory(root); err != nil {
		return err
	}
	return writeMarker(root, marker{Format: formatVersion, Backend: jsonlLayout})
}

// readSQLRooms returns the room identities of the retired layout.
func readSQLRooms(db *sql.DB, format int) ([]roomFile, error) {
	query := "SELECT name, parent, created_at FROM rooms"
	if format == 2 {
		query = "SELECT id, NULL, created_at FROM rooms"
	}
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("read the format-%d rooms: %w", format, err)
	}
	defer rows.Close()
	rooms := []roomFile{}
	for rows.Next() {
		var name, parent sql.NullString
		var createdAt any
		if err := rows.Scan(&name, &parent, &createdAt); err != nil {
			return nil, fmt.Errorf("read the format-%d rooms: %w", format, err)
		}
		created, err := scanTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("read the format-%d rooms: %w", format, err)
		}
		rooms = append(rooms, roomFile{Name: name.String, Parent: parent.String, Created: created})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(rooms, func(i, j int) bool { return rooms[i].Name < rooms[j].Name })
	return rooms, nil
}

// readSQLRecords returns every record of the retired layout, grouped by room
// and ordered by sequence.
func readSQLRecords(db *sql.DB, format int) (map[string][]core.Record, error) {
	authorColumn := "author"
	if format == 2 {
		authorColumn = "actor"
	}
	rows, err := db.Query("SELECT room_id, seq, " + authorColumn + ", at, content FROM events ORDER BY room_id, seq")
	if err != nil {
		return nil, fmt.Errorf("read the format-%d records: %w", format, err)
	}
	defer rows.Close()
	records := map[string][]core.Record{}
	for rows.Next() {
		var roomID string
		var seq uint64
		var author, content string
		var at any
		if err := rows.Scan(&roomID, &seq, &author, &at, &content); err != nil {
			return nil, fmt.Errorf("read the format-%d records: %w", format, err)
		}
		timestamp, err := scanTime(at)
		if err != nil {
			return nil, fmt.Errorf("read the format-%d records: %w", format, err)
		}
		records[roomID] = append(records[roomID], core.Record{
			Room: roomID, Seq: seq, Author: author, At: timestamp, Content: json.RawMessage(content),
		})
	}
	return records, rows.Err()
}

// readSQLLedger returns every retry-ledger entry of the retired layout,
// grouped by room.
func readSQLLedger(db *sql.DB, format int) (map[string][]ledgerFile, error) {
	authorColumn := "author"
	if format == 2 {
		authorColumn = "actor"
	}
	rows, err := db.Query("SELECT room_id, " + authorColumn + ", key, fingerprint, event_seq FROM append_idempotency")
	if err != nil {
		return nil, fmt.Errorf("read the format-%d ledger: %w", format, err)
	}
	defer rows.Close()
	ledger := map[string][]ledgerFile{}
	for rows.Next() {
		var roomID string
		var seq uint64
		var author, key string
		var fingerprint []byte
		if err := rows.Scan(&roomID, &author, &key, &fingerprint, &seq); err != nil {
			return nil, fmt.Errorf("read the format-%d ledger: %w", format, err)
		}
		ledger[roomID] = append(ledger[roomID], ledgerFile{
			Author: author, Key: key, Fingerprint: hex.EncodeToString(fingerprint), Seq: seq,
		})
	}
	return ledger, rows.Err()
}

// scanTime accepts what the retired databases stored: a time.Time handed
// back by the driver, or one of the textual timestamp forms SQLite keeps.
func scanTime(value any) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v.UTC(), nil
	case []byte:
		return parseTime(string(v))
	case string:
		return parseTime(v)
	default:
		return time.Time{}, fmt.Errorf("unexpected timestamp %v", value)
	}
}

func parseTime(text string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, text); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable timestamp %q", text)
}

// writeLines writes one JSON value per line, durably.
func writeLines[T any](path string, values []T) error {
	var data []byte
	for _, value := range values {
		line, err := json.Marshal(value)
		if err != nil {
			return err
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	if len(data) == 0 {
		return nil
	}
	return durableReplace(path, data, 0o644)
}
