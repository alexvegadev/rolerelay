package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	appdb "rolerelay/internal/db"
	"rolerelay/internal/domain"
)

func TestExportAndRestoreConsistentBackupAcrossInstallations(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	// Installation A
	dbA := filepath.Join(tmp, "install-a", "app.sqlite")
	if err := os.MkdirAll(filepath.Dir(dbA), 0o755); err != nil {
		t.Fatal(err)
	}
	connA, err := appdb.Open(ctx, dbA)
	if err != nil {
		t.Fatal(err)
	}
	storeA := NewWithPath(connA, dbA)
	defer storeA.Close()

	if _, err := storeA.UpdateProfile(ctx, domain.ProfileUpdate{
		FullName:        "Ada Lovelace",
		Email:           "ada@example.com",
		TargetRoles:     []string{"Backend Engineer"},
		Location:        "Remote",
		WorkArrangement: domain.WorkArrangementRemote,
		LanguageMode:    domain.LanguageModeBoth,
		FitThreshold:    70,
		DailySendLimit:  10,
	}); err != nil {
		t.Fatal(err)
	}

	pdfEN := []byte("%PDF-1.4\nAda EN CV bytes")
	pdfES := []byte("%PDF-1.4\nAda ES CV bytes")
	cvEN, err := storeA.SaveCV(ctx, SaveCVParams{
		Language: domain.CVLanguageEN, Filename: "ada-en.pdf", PageCount: 1,
		ExtractedText: "Ada Lovelace Go SQLite", Content: pdfEN,
	})
	if err != nil {
		t.Fatal(err)
	}
	cvES, err := storeA.SaveCV(ctx, SaveCVParams{
		Language: domain.CVLanguageES, Filename: "ada-es.pdf", PageCount: 1,
		ExtractedText: "Ada Lovelace Go SQLite Espanol", Content: pdfES,
	})
	if err != nil {
		t.Fatal(err)
	}

	vacRes, err := storeA.CreateManualVacancy(ctx, domain.ManualVacancyInput{
		CompanyName:      "Acme",
		Title:            "Senior Go Engineer",
		Location:         "Remote",
		WorkArrangement:  domain.WorkArrangementRemote,
		ApplicationEmail: "jobs@acme.test",
		Description:      "Requirements:\n- Go and SQLite",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, app, err := storeA.SaveEvaluationAndApplication(ctx, SaveEvaluationParams{
		Evaluation: domain.Evaluation{
			VacancyID:       vacRes.Vacancy.ID,
			ProfileID:       1,
			SelectedCVIDs:   []int64{cvEN.ID},
			ModelName:       "local-test",
			FitScore:        95,
			PassedFilters:   true,
			PassedThreshold: true,
			LanguageStatus:  domain.LanguageStatusOK,
			Summary:         "Verified Go + SQLite evidence",
			Findings: []domain.RequirementFinding{
				{Requirement: "Go and SQLite", Kind: domain.RequirementMandatory, Met: true, EvidenceVerified: true, EvidenceExcerpt: "Ada Lovelace Go SQLite", Reason: "Matched"},
			},
			MissingAnswers: []domain.MissingAnswer{},
		},
		SelectedCVRoles:  []string{"primary"},
		ApplicationState: domain.StatePending,
		Channel:          "email",
		Recipient:        "jobs@acme.test",
		Subject:          "Application: Senior Go Engineer",
		Body:             "Hello Acme",
		PendingReason:    "Qualified",
		StateReason:      "Qualified",
		StateSource:      "system",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Record a sent attempt + state change.
	if _, err := storeA.RecordApplicationState(ctx, app.ID, domain.StateInterviewPending, "Technical interview scheduled", "user"); err != nil {
		t.Fatal(err)
	}

	// Export consistent backup from Installation A.
	backupFile := filepath.Join(tmp, "exported-backup.sqlite")
	if err := storeA.ExportConsistentBackup(ctx, backupFile); err != nil {
		t.Fatalf("ExportConsistentBackup failed: %v", err)
	}

	// Installation B (fresh empty DB)
	dbB := filepath.Join(tmp, "install-b", "app.sqlite")
	if err := os.MkdirAll(filepath.Dir(dbB), 0o755); err != nil {
		t.Fatal(err)
	}
	connB, err := appdb.Open(ctx, dbB)
	if err != nil {
		t.Fatal(err)
	}
	storeB := NewWithPath(connB, dbB)
	defer storeB.Close()

	// Reject corrupt non-SQLite file cleanly without breaking live DB.
	corruptPath := filepath.Join(tmp, "not-a-db.sqlite")
	_ = os.WriteFile(corruptPath, []byte("corrupt garbage"), 0o644)
	if _, err := storeB.RestoreFromBackup(ctx, corruptPath); !errors.Is(err, ErrInvalidBackup) {
		t.Fatalf("expected ErrInvalidBackup for corrupt file, got %v", err)
	}

	// Restore valid backup onto Installation B.
	summary, err := storeB.RestoreFromBackup(ctx, backupFile)
	if err != nil {
		t.Fatalf("RestoreFromBackup failed: %v", err)
	}
	if summary.CVCount != 2 || summary.VacancyCount != 1 || summary.EvaluationCount != 1 || summary.ApplicationCount != 1 {
		t.Fatalf("unexpected restore summary: %+v", summary)
	}

	// Verify both CV PDFs, evaluation evidence, and state history recovered identically.
	fileEN, err := storeB.GetCVFile(ctx, cvEN.ID)
	if err != nil || !bytes.Equal(fileEN.Content, pdfEN) {
		t.Fatalf("EN CV file did not recover cleanly: err=%v", err)
	}
	fileES, err := storeB.GetCVFile(ctx, cvES.ID)
	if err != nil || !bytes.Equal(fileES.Content, pdfES) {
		t.Fatalf("ES CV file did not recover cleanly: err=%v", err)
	}

	restoredApp, err := storeB.GetApplicationDetailed(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restoredApp.Application.State != domain.StateInterviewPending {
		t.Fatalf("expected restored state=interview_pending, got %s", restoredApp.Application.State)
	}
	if restoredApp.LatestEvaluation == nil || len(restoredApp.LatestEvaluation.Findings) != 1 || !restoredApp.LatestEvaluation.Findings[0].EvidenceVerified {
		t.Fatalf("expected restored verified evaluation evidence, got %+v", restoredApp.LatestEvaluation)
	}
}
