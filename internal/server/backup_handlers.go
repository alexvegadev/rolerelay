package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"rolerelay/internal/store"
)

func (s *Server) handleExportBackup(w http.ResponseWriter, r *http.Request) {
	tmpDir, err := os.MkdirTemp("", "rolerelay-export-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backup_temp_failed", err.Error())
		return
	}
	defer os.RemoveAll(tmpDir)

	exportPath := filepath.Join(tmpDir, "backup.sqlite")
	if err := s.opts.Store.ExportConsistentBackup(r.Context(), exportPath); err != nil {
		writeError(w, http.StatusInternalServerError, "backup_export_failed", err.Error())
		return
	}

	info, err := os.Stat(exportPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backup_stat_failed", err.Error())
		return
	}
	file, err := os.Open(exportPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backup_open_failed", err.Error())
		return
	}
	defer file.Close()

	filename := fmt.Sprintf("rolerelay-backup-%s.sqlite", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, store.MaxBackupSizeBytes+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart", "Unable to read backup file upload (max 200 MB).")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "Choose a RoleRelay SQLite backup file (.sqlite) to restore.")
		return
	}
	defer file.Close()

	tmpDir, err := os.MkdirTemp("", "rolerelay-restore-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restore_temp_failed", err.Error())
		return
	}
	defer os.RemoveAll(tmpDir)

	stagingPath := filepath.Join(tmpDir, "uploaded.sqlite")
	out, err := os.OpenFile(stagingPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restore_stage_failed", err.Error())
		return
	}
	written, copyErr := io.Copy(out, io.LimitReader(file, store.MaxBackupSizeBytes+1))
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		writeError(w, http.StatusBadRequest, "restore_read_failed", "Failed to read uploaded backup file.")
		return
	}
	if written > store.MaxBackupSizeBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "Backup file exceeds the 200 MB limit.")
		return
	}

	ctx := r.Context()
	var summary store.RestoreSummary
	doRestore := func() error {
		var rErr error
		summary, rErr = s.opts.Store.RestoreFromBackup(ctx, stagingPath)
		return rErr
	}

	if s.opts.Worker != nil {
		err = s.opts.Worker.WithExclusive(doRestore)
	} else {
		err = doRestore()
	}
	if err != nil {
		if errors.Is(err, store.ErrInvalidBackup) {
			writeError(w, http.StatusUnprocessableEntity, "invalid_backup", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "restore_failed", err.Error())
		return
	}

	// Sync worker pause state from restored DB.
	emailCfg, _ := s.loadEmailSettingsWithCredStatus(ctx)
	if s.opts.Worker != nil {
		s.opts.Worker.SyncPausedFromDB(emailCfg.SendingPaused)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"restored":      true,
		"summary":       summary,
		"emailSettings": emailCfg,
	})
}
