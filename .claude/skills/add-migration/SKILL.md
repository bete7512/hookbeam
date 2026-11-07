---
name: add-migration
description: Use for ANY Postgres schema change in this repo — new table, column, index, constraint, enum, or data backfill. Covers creating the Go goose migration, registering it, up/down round-trip, zero-downtime ordering, and syncing models/repos in the same change.
---

# add-migration

## How migrations work here

- Each app owns its migrations and database. For the api app: `api/migrations/`.
- One Go file per version, `api/migrations/NNNN_<snake>.go`; goose takes the version from the 4-digit prefix.
- `registerMigration_NNNN_<Camel>()` / `up_…` / `down_…` are methods on `*service` registering into goose's
  global registry; `registerMigrations()` in `api/migrations/migration.go` lists them. **Unlisted = never runs.**
- Compiled into the service's api binary. `make migrate-up` / `make migrate-down` / `make migrate-status`
  run `go run ./$(APP)/cmd/api -m migrate up|down|status` against `DATABASE_URL` (`.env`); `APP` defaults to `api`.
- Integration tests apply all migrations via `testutil.Postgres(s.T(), migrations.Up)`
  (`github.com/bete7512/hookbeam/api/migrations`).
- Never at app boot. Prod runs `api -m migrate up` as a one-off task **before** rollout, so the
  previous app revision runs on the new schema. `update_updated_at_column()` comes from 0001; reuse it.

## Steps

1. `make db-up && make migrate-status` — confirm local DB is current.
2. Copy the latest `api/migrations/NNNN_*.go` to the next number (e.g. `0003_<snake>.go`) and rename its
   `registerMigration_` / `up_` / `down_` methods to `NNNN_<Camel>`.
3. Fill `up` and `down` from a template below. Index on existing table → switch to NoTx (C).
4. Add `s.registerMigration_NNNN_<Camel>()` as the last line of `registerMigrations()`.
5. `make migrate-up && make migrate-down && make migrate-up` — all succeed; `make migrate-status` shows it applied.
6. Same change: `api/models/<x>.go` `db:` tags and every column const / INSERT / `RETURNING` in `api/repos/<x>.go`
   (`userColumns`, `userListColumns`) — `pgx.RowToStructByName` fails on any unmatched column.
7. `make test-integration` (Docker) — proves migrations apply from zero and repos match.
8. Run the `verify-change` skill before reporting done.

## Templates

### A. New table (tx)

```go
func (s *service) up_0003_CreateProjects(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS projects (
  id         BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  owner_id   BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  name       TEXT        NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS projects_owner_id_idx ON projects (owner_id);
CREATE TRIGGER update_projects_updated_at BEFORE UPDATE ON projects FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();
	`)
	return err
}

func (s *service) down_0003_CreateProjects(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
DROP TRIGGER IF EXISTS update_projects_updated_at ON projects;
DROP TABLE IF EXISTS projects;
	`)
	return err
}
```

Index every FK column (Postgres doesn't); plain `CREATE INDEX` is fine on a new, empty table.
Choose `ON DELETE CASCADE | RESTRICT | SET NULL` deliberately. Ids are always `BIGINT GENERATED ALWAYS AS IDENTITY`, FKs `BIGINT`.

### B. Add a column (tx)

```go
// up
ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_url TEXT NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS locale TEXT NOT NULL DEFAULT 'en';
// down
ALTER TABLE users DROP COLUMN IF EXISTS locale;
ALTER TABLE users DROP COLUMN IF EXISTS avatar_url;
```

Nullable or `NOT NULL DEFAULT <constant>` is metadata-only (PG11+). A volatile default (`clock_timestamp()`,
`random()`) rewrites the table: add nullable, backfill (D), then set the default.

### C. Index on an existing table (NoTx)

A copied migration uses `AddMigrationContext` + `*sql.Tx`. Change **both** — `CONCURRENTLY`
cannot run inside a transaction. One statement per NoTx migration (no atomicity to lean on).

```go
func (s *service) registerMigration_0004_AddProjectsNameIndex() {
	goose.AddMigrationNoTxContext(s.up_0004_AddProjectsNameIndex, s.down_0004_AddProjectsNameIndex)
}

func (s *service) up_0004_AddProjectsNameIndex(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE INDEX CONCURRENTLY IF NOT EXISTS projects_name_idx ON projects (name)`)
	return err
}

func (s *service) down_0004_AddProjectsNameIndex(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DROP INDEX CONCURRENTLY IF EXISTS projects_name_idx`)
	return err
}
```

A failed `CONCURRENTLY` build leaves an `INVALID` index that `IF NOT EXISTS` then skips: find it with
`SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;`, drop, retry.

### D. Backfill (NoTx, batched)

Never one `UPDATE` over a large table (long locks, WAL spike, replica lag). Batch, one implicit tx each:

```go
func (s *service) up_0005_BackfillUsersDisplayName(ctx context.Context, db *sql.DB) error {
	for {
		res, err := db.ExecContext(ctx, `
UPDATE users SET display_name = name
WHERE id IN (SELECT id FROM users WHERE display_name IS NULL LIMIT 1000 FOR UPDATE SKIP LOCKED)`)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
	}
}
```

Down for a pure backfill is usually `return nil` (say so). Each batch fires the `updated_at` trigger.

## Zero-downtime (expand / contract)

1. **Expand**: add the new column/table nullable. Old code ignores it.
2. Deploy code that writes both old and new, reads old.
3. **Backfill** (D).
4. Enforce: constraints, then switch reads to new.
5. **Contract**, in a later release: drop the old column once no running revision reads it.

- Never rename a column or drop one the running revision reads; add new + backfill + drop later.
- `SET NOT NULL` on a big table scans under `ACCESS EXCLUSIVE`. Instead:
  `ADD CONSTRAINT users_display_name_nn CHECK (display_name IS NOT NULL) NOT VALID;` → next
  migration `VALIDATE CONSTRAINT users_display_name_nn;` → `SET NOT NULL` (PG12+ reuses the
  valid check, no scan) → `DROP CONSTRAINT users_display_name_nn`.
- FK on existing table: `ADD CONSTRAINT … FOREIGN KEY … NOT VALID`, then `VALIDATE` separately.
- Unique: `CREATE UNIQUE INDEX CONCURRENTLY x_uniq …` (NoTx), then `ADD CONSTRAINT x_uniq UNIQUE USING INDEX x_uniq`.
- Big data or DDL change not obvious from the diff → a new file in `docs/decisions/`.

## Common mistakes

- Register call missing from `registerMigrations()` → goose never sees it; `migrate-status` won't list it.
- Skipping or duplicating a version number: use the latest number + 1 (a duplicate panics at registration).
- Editing a migration already applied anywhere shared → never; write a new one.
- `CONCURRENTLY` in a tx migration → `cannot run inside a transaction block`.
- Empty or `-- TODO` down → round-trip "passes" but rollback is broken.
- Recreating/dropping `update_updated_at_column()` outside 0001, or setting `updated_at` by hand.
- Model `db:` tags or repo column consts not updated → `RowToStructByName` errors at runtime.
