package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenAndMigrateIdempotent(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.sqlite")

	conn, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}

	ver, err := SchemaVersion(ctx, conn)
	if err != nil {
		t.Fatalf("SchemaVersion failed: %v", err)
	}
	if ver < 1 {
		t.Fatalf("expected schema version >= 1, got %d", ver)
	}

	expectedTables := []string{
		"profiles",
		"cv_documents",
		"job_boards",
		"vacancies",
		"evaluations",
		"applications",
		"application_cvs",
		"submission_attempts",
		"state_changes",
		"settings",
		"schema_migrations",
	}
	for _, table := range expectedTables {
		var name string
		err := conn.QueryRowContext(
			ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`,
			table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("expected table %q to exist: %v", table, err)
		}
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("close first connection: %v", err)
	}

	// Reopening the same database should succeed idempotently.
	conn2, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer conn2.Close()

	ver2, err := SchemaVersion(ctx, conn2)
	if err != nil {
		t.Fatalf("SchemaVersion after reopen failed: %v", err)
	}
	if ver2 != ver {
		t.Fatalf("expected schema version %d, got %d", ver, ver2)
	}
}
