// Package cooldown stores search cooldown state in SQLite through Bun.
package cooldown

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/migrate"
	_ "modernc.org/sqlite"
)

type debugger interface {
	Debug(string, string, ...string)
}

type cooldownRow struct {
	bun.BaseModel `bun:"table:cooldowns,alias:c"`

	ID            int64  `bun:",pk,autoincrement"`
	Instance      string `bun:",notnull"`
	Category      string `bun:",notnull"`
	ItemID        int    `bun:"item_id,notnull"`
	SearchedAtUTC int64  `bun:"searched_at_utc,notnull"`
}

// Store is a concurrency-safe Bun-backed cooldown store.
type Store struct {
	db    *bun.DB
	debug debugger
	now   func() time.Time
}

// Open opens the configured database, replacing a markerless pre-Go database.
func Open(ctx context.Context, path string, debug debugger) (*Store, bool, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, false, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, false, fmt.Errorf("create database directory: %w", err)
	}

	reset := false
	if _, err := os.Stat(absPath); err == nil {
		marked, version, inspectErr := inspect(ctx, absPath)
		if inspectErr != nil {
			return nil, false, fmt.Errorf("inspect existing database: %w", inspectErr)
		}
		if marked && version > schemaVersion {
			return nil, false, fmt.Errorf("database schema version %d is newer than supported version %d", version, schemaVersion)
		}
		if !marked {
			if version != 0 {
				return nil, false, fmt.Errorf("database has schema version %d but no Bun migration marker; refusing automatic reset", version)
			}
			if err := removeDatabase(absPath); err != nil {
				return nil, false, fmt.Errorf("remove pre-Go database: %w", err)
			}
			reset = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, fmt.Errorf("stat database: %w", err)
	}

	sqldb, err := sql.Open("sqlite", dsn(absPath, false))
	if err != nil {
		return nil, false, fmt.Errorf("open sqlite: %w", err)
	}
	sqldb.SetMaxOpenConns(1)
	sqldb.SetMaxIdleConns(1)
	if err := sqldb.PingContext(ctx); err != nil {
		_ = sqldb.Close()
		return nil, false, fmt.Errorf("connect sqlite: %w", err)
	}

	db := bun.NewDB(sqldb, sqlitedialect.New())
	migrations, err := newMigrations()
	if err != nil {
		_ = db.Close()
		return nil, false, fmt.Errorf("register migrations: %w", err)
	}
	migrator := migrate.NewMigrator(db, migrations)
	if err := migrator.Init(ctx); err != nil {
		_ = db.Close()
		return nil, false, fmt.Errorf("initialize migrations: %w", err)
	}
	if _, err := migrator.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, false, fmt.Errorf("migrate database: %w", err)
	}
	if err := validateSchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, false, fmt.Errorf("validate database schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, false, fmt.Errorf("enable WAL: %w", err)
	}

	return &Store{db: db, debug: debug, now: time.Now}, reset, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// CleanExpired removes entries strictly older than the cooldown boundary.
func (s *Store) CleanExpired(ctx context.Context, instance, category string, duration time.Duration) error {
	cutoff := s.now().UTC().Add(-duration).UnixMilli()
	result, err := s.db.NewDelete().Model((*cooldownRow)(nil)).
		Where("instance = ?", instance).Where("category = ?", category).
		Where("searched_at_utc < ?", cutoff).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete expired cooldowns: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count deleted cooldowns: %w", err)
	}
	if count > 0 && s.debug != nil {
		s.debug.Debug(strings.ToLower(instance)+".cooldown", fmt.Sprintf("Cleaned %d expired cooldown entries for %s", count, category))
	}
	return nil
}

// IDs returns the item IDs currently on cooldown.
func (s *Store) IDs(ctx context.Context, instance, category string) (map[int]struct{}, error) {
	var ids []int
	if err := s.db.NewSelect().Model((*cooldownRow)(nil)).Column("item_id").
		Where("instance = ?", instance).Where("category = ?", category).Scan(ctx, &ids); err != nil {
		return nil, fmt.Errorf("select cooldown IDs: %w", err)
	}
	result := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		result[id] = struct{}{}
	}
	if s.debug != nil {
		s.debug.Debug(strings.ToLower(instance)+".cooldown", fmt.Sprintf("%d items on cooldown for %s", len(result), category))
	}
	return result, nil
}

// Mark records distinct item IDs as searched in one transaction.
func (s *Store) Mark(ctx context.Context, instance, category string, itemIDs []int) error {
	seen := make(map[int]struct{}, len(itemIDs))
	rows := make([]cooldownRow, 0, len(itemIDs))
	now := s.now().UTC().UnixMilli()
	for _, id := range itemIDs {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		rows = append(rows, cooldownRow{Instance: instance, Category: category, ItemID: id, SearchedAtUTC: now})
	}
	if len(rows) > 0 {
		if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			_, err := tx.NewInsert().Model(&rows).Exec(ctx)
			return err
		}); err != nil {
			return fmt.Errorf("insert cooldowns: %w", err)
		}
	}
	if s.debug != nil {
		s.debug.Debug(strings.ToLower(instance)+".cooldown", fmt.Sprintf("Marked %d items as searched for %s", len(rows), category))
	}
	return nil
}

// Clear removes a category and its grouped variants, optionally for one instance.
func (s *Store) Clear(ctx context.Context, category, instance string) (int, error) {
	categories := []string{category, category + "_Season", category + "_Artist"}
	query := s.db.NewDelete().Model((*cooldownRow)(nil)).Where("category IN (?)", bun.List(categories))
	if instance != "" {
		query = query.Where("instance = ?", instance)
	}
	result, err := query.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("clear cooldowns: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count cleared cooldowns: %w", err)
	}
	if s.debug != nil {
		name := instance
		if name == "" {
			name = "all"
		}
		s.debug.Debug(strings.ToLower(name)+".cooldown", fmt.Sprintf("Cleared %d cooldown entries for %s", count, category))
	}
	return int(count), nil
}

func inspect(ctx context.Context, path string) (bool, int, error) {
	db, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		return false, 0, err
	}
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil {
		return false, 0, err
	}
	if integrity != "ok" {
		return false, 0, fmt.Errorf("SQLite integrity check failed: %s", integrity)
	}
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return false, 0, err
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'bun_migrations'").Scan(&count); err != nil {
		return false, 0, err
	}
	if count == 0 {
		return false, version, nil
	}
	if version > schemaVersion {
		return true, version, nil
	}
	rows, err := db.QueryContext(ctx, "SELECT name FROM bun_migrations ORDER BY id")
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = rows.Close() }()
	applied := false
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, 0, err
		}
		if name != "1" {
			return false, 0, fmt.Errorf("bun migration metadata contains unsupported migration %q", name)
		}
		applied = true
	}
	if err := rows.Err(); err != nil {
		return false, 0, err
	}
	var columns int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('bun_migrations') WHERE name IN ('id', 'name', 'group_id', 'migrated_at')`).Scan(&columns); err != nil {
		return false, 0, err
	}
	if columns != 4 {
		return false, 0, fmt.Errorf("bun migration metadata schema is invalid")
	}
	if (version == schemaVersion) != applied {
		return false, 0, fmt.Errorf("bun migration metadata and schema version are inconsistent")
	}
	return true, version, nil
}

func validateSchema(ctx context.Context, db *bun.DB) error {
	checks := []struct {
		query string
		want  int
	}{
		{`SELECT COUNT(*) FROM pragma_table_info('cooldowns')`, 5},
		{`SELECT COUNT(*) FROM pragma_table_info('cooldowns') WHERE name = 'id' AND lower(type) = 'integer' AND pk = 1`, 1},
		{`SELECT COUNT(*) FROM pragma_table_info('cooldowns') WHERE name IN ('instance', 'category') AND lower(type) = 'text' AND "notnull" = 1`, 2},
		{`SELECT COUNT(*) FROM pragma_table_info('cooldowns') WHERE name IN ('item_id', 'searched_at_utc') AND lower(type) = 'integer' AND "notnull" = 1`, 2},
		{`SELECT COUNT(*) FROM pragma_index_list('cooldowns') WHERE name = 'cooldowns_instance_category_item_id_uq' AND "unique" = 1`, 1},
		{`SELECT COUNT(*) FROM pragma_index_list('cooldowns') WHERE name = 'cooldowns_searched_at_utc_idx' AND "unique" = 0`, 1},
		{`SELECT COUNT(*) FROM pragma_index_info('cooldowns_instance_category_item_id_uq')`, 3},
		{`SELECT COUNT(*) FROM pragma_index_info('cooldowns_instance_category_item_id_uq') WHERE (seqno = 0 AND name = 'instance') OR (seqno = 1 AND name = 'category') OR (seqno = 2 AND name = 'item_id')`, 3},
		{`SELECT COUNT(*) FROM pragma_index_info('cooldowns_searched_at_utc_idx') WHERE seqno = 0 AND name = 'searched_at_utc'`, 1},
	}
	for _, check := range checks {
		var got int
		if err := db.QueryRowContext(ctx, check.query).Scan(&got); err != nil {
			return err
		}
		if got != check.want {
			return fmt.Errorf("unexpected cooldown schema")
		}
	}
	return nil
}

func removeDatabase(path string) error {
	for _, target := range []string{path + "-wal", path + "-shm", path} {
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func dsn(path string, readWriteOnly bool) string {
	slashPath := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	u := url.URL{Scheme: "file", Path: slashPath}
	query := u.Query()
	if readWriteOnly {
		query.Set("mode", "rw")
	}
	query.Add("_pragma", "busy_timeout(5000)")
	if !readWriteOnly {
		query.Add("_pragma", "journal_mode(WAL)")
	}
	u.RawQuery = query.Encode()
	return u.String()
}
