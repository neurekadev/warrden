package cooldown

import (
	"context"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

const schemaVersion = 1

func newMigrations() (*migrate.Migrations, error) {
	migrations := migrate.NewMigrations()
	err := migrations.Register(
		func(ctx context.Context, db *bun.DB) error {
			if _, err := db.ExecContext(ctx, `
CREATE TABLE cooldowns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance TEXT NOT NULL,
    category TEXT NOT NULL,
    item_id INTEGER NOT NULL,
    searched_at_utc INTEGER NOT NULL
);
CREATE UNIQUE INDEX cooldowns_instance_category_item_id_uq
    ON cooldowns (instance, category, item_id);
CREATE INDEX cooldowns_searched_at_utc_idx
    ON cooldowns (searched_at_utc);
PRAGMA user_version = 1;
`); err != nil {
				return err
			}
			return nil
		},
		func(ctx context.Context, db *bun.DB) error {
			_, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS cooldowns; PRAGMA user_version = 0;`)
			return err
		},
	)
	if err != nil {
		return nil, err
	}
	return migrations, nil
}
