package cooldown

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenReplacesMarkerlessDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cooldowns.db")

	db, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE legacy_cooldowns (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, reset, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if !reset {
		t.Fatal("expected markerless database to be replaced")
	}
	assertTable(t, store.db.DB, "cooldowns", true)
	assertTable(t, store.db.DB, "bun_migrations", true)
	assertTable(t, store.db.DB, "legacy_cooldowns", false)
}

func TestStoreCooldownLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cooldowns.db")
	store, reset, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reset {
		t.Fatal("new database must not be reported as reset")
	}
	t.Cleanup(func() { _ = store.Close() })
	if got := store.db.DB.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("max open connections = %d, want 1", got)
	}
	var journalMode string
	if err := store.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode = %q, want wal", journalMode)
	}
	var busyTimeout int
	if err := store.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy timeout = %d, want 5000", busyTimeout)
	}
	var migrationName string
	if err := store.db.QueryRowContext(ctx, "SELECT name FROM bun_migrations ORDER BY id DESC LIMIT 1").Scan(&migrationName); err != nil {
		t.Fatal(err)
	}
	if migrationName != "1" {
		t.Fatalf("migration name = %q, want version 1", migrationName)
	}

	now := time.Date(2026, time.August, 22, 12, 0, 0, 123_000_000, time.FixedZone("test", -7*60*60))
	store.now = func() time.Time { return now }
	if err := store.Mark(ctx, "Sonarr", "Missing", []int{3, 1, 3}); err != nil {
		t.Fatal(err)
	}
	ids, err := store.IDs(ctx, "Sonarr", "Missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("got %d IDs, want 2", len(ids))
	}
	if _, ok := ids[1]; !ok {
		t.Fatal("missing ID 1")
	}
	if _, ok := ids[3]; !ok {
		t.Fatal("missing ID 3")
	}

	if err := store.Mark(ctx, "Sonarr", "Missing", []int{1}); err == nil {
		t.Fatal("expected unique constraint error for an existing cooldown")
	}
	if err := store.Mark(ctx, "Sonarr", "Missing", []int{4, 1}); err == nil {
		t.Fatal("expected batch containing an existing cooldown to fail")
	}
	ids, err = store.IDs(ctx, "Sonarr", "Missing")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := ids[4]; exists {
		t.Fatal("failed batch was partially inserted")
	}

	boundary := now.Add(-time.Hour).UTC().UnixMilli()
	if _, err := store.db.ExecContext(ctx, "UPDATE cooldowns SET searched_at_utc = ? WHERE item_id = 1", boundary); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE cooldowns SET searched_at_utc = ? WHERE item_id = 3", boundary-1); err != nil {
		t.Fatal(err)
	}
	if err := store.CleanExpired(ctx, "Sonarr", "Missing", time.Hour); err != nil {
		t.Fatal(err)
	}
	ids, err = store.IDs(ctx, "Sonarr", "Missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 {
		t.Fatalf("got %v after cleanup, want the boundary item only", ids)
	}
	if _, ok := ids[1]; !ok {
		t.Fatal("entry exactly on the expiration boundary was removed")
	}

	if err := store.Mark(ctx, "Sonarr", "Missing_Season", []int{1001}); err != nil {
		t.Fatal(err)
	}
	if err := store.Mark(ctx, "Other", "Missing_Artist", []int{9}); err != nil {
		t.Fatal(err)
	}
	cleared, err := store.Clear(ctx, "Missing", "Sonarr")
	if err != nil {
		t.Fatal(err)
	}
	if cleared != 2 {
		t.Fatalf("cleared %d rows, want 2", cleared)
	}
	other, err := store.IDs(ctx, "Other", "Missing_Artist")
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 {
		t.Fatal("instance-scoped clear removed another instance")
	}
	if err := store.Mark(ctx, "Other", "Upgrade", nil); err != nil {
		t.Fatalf("empty mark: %v", err)
	}
}

func TestOpenRetainsSupportedDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cooldowns.db")
	first, _, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Mark(ctx, "Radarr", "Missing", []int{7}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, reset, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if reset {
		t.Fatal("supported Go database was reset")
	}
	ids, err := second.IDs(ctx, "Radarr", "Missing")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids[7]; !ok {
		t.Fatal("retained database lost its cooldown")
	}
}

func TestOpenRunsPendingSupportedMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cooldowns.db")
	store, _, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM bun_migrations; DROP TABLE cooldowns; PRAGMA user_version = 0"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, reset, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if reset {
		t.Fatal("pending Go migration was mistaken for a pre-Go database")
	}
	assertTable(t, reopened.db.DB, "cooldowns", true)
}

func TestOpenRefusesDamagedSupportedSchema(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cooldowns.db")
	store, _, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP INDEX cooldowns_searched_at_utc_idx"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, reset, err := Open(ctx, path, nil)
	if reopened != nil {
		_ = reopened.Close()
		t.Fatal("damaged schema was opened")
	}
	if reset || err == nil || !strings.Contains(err.Error(), "validate database schema") {
		t.Fatalf("reset=%t err=%v", reset, err)
	}
}

func TestRemoveDatabaseDeletesLiteralSidecars(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cooldowns.db")
	for _, target := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.WriteFile(target, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeDatabase(path); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("target %q still exists or stat failed: %v", target, err)
		}
	}
}

func TestOpenRefusesAmbiguousOrNewerDatabase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(*testing.T, string)
		want  string
	}{
		{
			name: "corrupt",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("not sqlite"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: "inspect existing database",
		},
		{
			name: "newer",
			setup: func(t *testing.T, path string) {
				t.Helper()
				db, err := sql.Open("sqlite", dsn(path, false))
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = db.Close() }()
				if _, err := db.ExecContext(context.Background(), "CREATE TABLE bun_migrations (id INTEGER); PRAGMA user_version = 2"); err != nil {
					t.Fatal(err)
				}
			},
			want: "newer than supported",
		},
		{
			name: "markerless schema version",
			setup: func(t *testing.T, path string) {
				t.Helper()
				db, err := sql.Open("sqlite", dsn(path, false))
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = db.Close() }()
				if _, err := db.ExecContext(context.Background(), "CREATE TABLE legacy (id INTEGER); PRAGMA user_version = 1"); err != nil {
					t.Fatal(err)
				}
			},
			want: "no Bun migration marker",
		},
		{
			name: "unsupported Bun migration",
			setup: func(t *testing.T, path string) {
				t.Helper()
				db, err := sql.Open("sqlite", dsn(path, false))
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = db.Close() }()
				if _, err := db.ExecContext(context.Background(), "CREATE TABLE bun_migrations (id INTEGER PRIMARY KEY, name TEXT); INSERT INTO bun_migrations (id, name) VALUES (1, '2')"); err != nil {
					t.Fatal(err)
				}
			},
			want: "unsupported migration",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "cooldowns.db")
			test.setup(t, path)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			store, _, err := Open(context.Background(), path, nil)
			if store != nil {
				_ = store.Close()
				t.Fatal("unexpected store")
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got error %v, want containing %q", err, test.want)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(after) != len(before) {
				t.Fatal("refused database was replaced")
			}
		})
	}
}

func assertTable(t *testing.T, db *sql.DB, table string, want bool) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if got := count == 1; got != want {
		t.Fatalf("table %q exists = %t, want %t", table, got, want)
	}
}
