package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	appdb "rolerelay/internal/db"
)

const MaxBackupSizeBytes = 200 * 1024 * 1024 // 200 MiB

var (
	sqliteMagicHeader = []byte("SQLite format 3\x00")
	ErrInvalidBackup  = errors.New("invalid RoleRelay SQLite backup file")
	requiredTables    = []string{
		"profiles",
		"cv_documents",
		"vacancies",
		"evaluations",
		"applications",
		"application_cvs",
		"submission_attempts",
		"state_changes",
		"schema_migrations",
	}
)

type RestoreSummary struct {
	SchemaVersion    int    `json:"schemaVersion"`
	CVCount          int    `json:"cvCount"`
	VacancyCount     int    `json:"vacancyCount"`
	EvaluationCount  int    `json:"evaluationCount"`
	ApplicationCount int    `json:"applicationCount"`
	AttemptCount     int    `json:"attemptCount"`
	StateChangeCount int    `json:"stateChangeCount"`
	SafetyBackupPath string `json:"safetyBackupPath,omitempty"`
}

// ExportConsistentBackup writes a transactionally consistent, self-contained single-file
// SQLite snapshot using SQLite's online VACUUM INTO command and verifies its integrity.
func (s *Store) ExportConsistentBackup(ctx context.Context, destPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return errors.New("database is not open")
	}
	absDest, err := filepath.Abs(destPath)
	if err != nil {
		return fmt.Errorf("resolve backup destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absDest), 0o755); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	_ = os.Remove(absDest)

	escaped := strings.ReplaceAll(absDest, "'", "''")
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", escaped)); err != nil {
		return fmt.Errorf("create consistent SQLite backup: %w", err)
	}

	if err := verifySQLiteIntegrity(ctx, absDest); err != nil {
		_ = os.Remove(absDest)
		return fmt.Errorf("verify exported backup integrity: %w", err)
	}
	return nil
}

// ValidateBackupFile checks SQLite magic header, integrity, and RoleRelay schema tables
// without touching the live database.
func ValidateBackupFile(ctx context.Context, stagingPath string) error {
	f, err := os.Open(stagingPath)
	if err != nil {
		return fmt.Errorf("%w: open file: %v", ErrInvalidBackup, err)
	}
	header := make([]byte, len(sqliteMagicHeader))
	n, readErr := io.ReadFull(f, header)
	_ = f.Close()
	if readErr != nil || n < len(sqliteMagicHeader) || !bytes.Equal(header, sqliteMagicHeader) {
		return fmt.Errorf("%w: file is not a SQLite 3 database", ErrInvalidBackup)
	}

	if err := verifySQLiteIntegrity(ctx, stagingPath); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBackup, err)
	}

	conn, err := sql.Open("sqlite", filepath.Clean(stagingPath))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBackup, err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	for _, tbl := range requiredTables {
		var name string
		err := conn.QueryRowContext(
			ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`,
			tbl,
		).Scan(&name)
		if err != nil {
			return fmt.Errorf("%w: missing required table %q", ErrInvalidBackup, tbl)
		}
	}

	// Ensure forward-compatible migrations succeed on the staged copy before replacing live DB.
	if err := appdb.Migrate(ctx, conn); err != nil {
		return fmt.Errorf("%w: migrate backup schema: %v", ErrInvalidBackup, err)
	}
	return nil
}

// RestoreFromBackup validates a staged SQLite backup, saves a pre-restore safety snapshot,
// closes the live DB to release Windows file locks, swaps in the restored file atomically,
// reopens SQLite, and runs interrupted-attempt recovery.
func (s *Store) RestoreFromBackup(ctx context.Context, stagingPath string) (RestoreSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dbPath == "" || s.dbPath == ":memory:" {
		return RestoreSummary{}, errors.New("live restore requires a file-backed SQLite database")
	}

	if err := ValidateBackupFile(ctx, stagingPath); err != nil {
		return RestoreSummary{}, err
	}

	// Create a pre-restore safety copy of the current database before closing.
	safetyPath := s.dbPath + ".pre-restore.bak"
	if s.db != nil {
		_ = os.Remove(safetyPath)
		escapedSafety := strings.ReplaceAll(safetyPath, "'", "''")
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", escapedSafety)); err != nil {
			safetyPath = ""
		}
		if err := s.db.Close(); err != nil {
			return RestoreSummary{}, fmt.Errorf("close live database before restore: %w", err)
		}
		s.db = nil
	}

	// Remove stale WAL/SHM sidecars so SQLite does not replay old WAL frames onto the restored DB.
	_ = os.Remove(s.dbPath + "-wal")
	_ = os.Remove(s.dbPath + "-shm")

	// Copy staged backup into same directory as dbPath, then atomic rename over live file.
	tmpTarget := s.dbPath + ".restore.tmp"
	if err := copyFileContents(stagingPath, tmpTarget); err != nil {
		// Attempt to reopen original DB on failure.
		if reopened, openErr := appdb.Open(ctx, s.dbPath); openErr == nil {
			s.db = reopened
		}
		return RestoreSummary{}, fmt.Errorf("stage restored database: %w", err)
	}
	if err := os.Rename(tmpTarget, s.dbPath); err != nil {
		_ = os.Remove(tmpTarget)
		if reopened, openErr := appdb.Open(ctx, s.dbPath); openErr == nil {
			s.db = reopened
		}
		return RestoreSummary{}, fmt.Errorf("replace live database with backup: %w", err)
	}

	reopened, err := appdb.Open(ctx, s.dbPath)
	if err != nil {
		return RestoreSummary{}, fmt.Errorf("reopen restored database: %w", err)
	}
	s.db = reopened

	// Ensure any in-flight 'sending' attempt captured in the backup is safely marked 'uncertain'.
	if _, err := s.RecoverInterruptedAttempts(ctx); err != nil {
		return RestoreSummary{}, fmt.Errorf("recover interrupted attempts after restore: %w", err)
	}

	ver, _ := appdb.SchemaVersion(ctx, s.db)
	summary := RestoreSummary{
		SchemaVersion:    ver,
		SafetyBackupPath: safetyPath,
	}
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cv_documents`).Scan(&summary.CVCount)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vacancies`).Scan(&summary.VacancyCount)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM evaluations`).Scan(&summary.EvaluationCount)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM applications`).Scan(&summary.ApplicationCount)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM submission_attempts`).Scan(&summary.AttemptCount)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM state_changes`).Scan(&summary.StateChangeCount)

	return summary, nil
}

func verifySQLiteIntegrity(ctx context.Context, path string) error {
	conn, err := sql.Open("sqlite", filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("open sqlite for integrity check: %w", err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	var result string
	if err := conn.QueryRowContext(ctx, `PRAGMA integrity_check;`).Scan(&result); err != nil {
		return fmt.Errorf("PRAGMA integrity_check failed: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(result), "ok") {
		return fmt.Errorf("SQLite integrity check reported: %s", result)
	}
	return nil
}

func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}
