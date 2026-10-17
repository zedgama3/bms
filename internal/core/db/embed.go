package db

import (
	"embed"
	"io/fs"
)

// The migrator's own SQL lives in postgres/sql and its bootstrap set in
// postgres/migrations (postgres-sql skill: plain SQL in embedded files).
var (
	//go:embed postgres/sql/*.sql
	sqlFiles embed.FS
	//go:embed postgres/migrations
	migrationFiles embed.FS
)

// SQL text for the statements this package runs itself.
var (
	sqlReadonlyProbe   = mustSQL("readonly_probe.sql")
	sqlTableExists     = mustSQL("table_exists.sql")
	sqlAppliedAll      = mustSQL("applied_all.sql")
	sqlAppliedForSet   = mustSQL("applied_for_set.sql")
	sqlInsertApplied   = mustSQL("insert_applied.sql")
	sqlInsertBootstrap = mustSQL("insert_bootstrap.sql")
	sqlAdvisoryTryLock = mustSQL("advisory_try_lock.sql")
	sqlAdvisoryUnlock  = mustSQL("advisory_unlock.sql")
)

// bootstrapFiles is the root of the "db" set: 0001_schema_migration.sql at its top.
var bootstrapFiles = mustSub(migrationFiles, "postgres/migrations")

// mustSQL reads an embedded file. It panics only when the binary was built without the
// file, which the embed directive makes impossible.
func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("postgres/sql/" + name)
	if err != nil {
		panic("db: embedded SQL missing: " + name)
	}
	return string(b)
}

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("db: embedded directory missing: " + dir)
	}
	return sub
}
