package store

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"rolerelay/internal/db"
	"rolerelay/internal/domain"
)

func TestProfileAndCVPersistenceAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.sqlite")

	// Session 1: configure profile and upload English & Spanish CVs.
	conn1, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db session 1: %v", err)
	}
	s1 := New(conn1)

	profile, err := s1.UpdateProfile(ctx, domain.ProfileUpdate{
		FullName:        "Ada Lovelace",
		Email:           "ada@example.com",
		TargetRoles:     []string{"Backend Engineer", "Go Developer", "backend engineer"},
		Location:        "Buenos Aires / Remote",
		WorkArrangement: domain.WorkArrangementRemote,
		LanguageMode:    domain.LanguageModeBoth,
		FitThreshold:    75,
		DailySendLimit:  15,
	})
	if err != nil {
		t.Fatalf("UpdateProfile failed: %v", err)
	}
	if len(profile.TargetRoles) != 2 {
		t.Fatalf("expected deduplicated 2 target roles, got %v", profile.TargetRoles)
	}

	pdfBytesEN := []byte("%PDF-1.4\nEnglish CV bytes")
	savedEN, err := s1.SaveCV(ctx, SaveCVParams{
		Language:      domain.CVLanguageEN,
		Filename:      "ada-cv-en.pdf",
		PageCount:     1,
		ExtractedText: "Ada Lovelace - Senior Go Backend Engineer",
		Content:       pdfBytesEN,
	})
	if err != nil {
		t.Fatalf("SaveCV EN failed: %v", err)
	}
	if savedEN.Version != 1 || !savedEN.IsActive {
		t.Fatalf("unexpected saved EN CV: %+v", savedEN)
	}

	// Both mode should still report missing Spanish CV.
	cvs1, err := s1.ListCVs(ctx)
	if err != nil {
		t.Fatalf("ListCVs failed: %v", err)
	}
	readiness1 := ComputeReadiness(profile, cvs1)
	if readiness1.Ready {
		t.Fatalf("expected readiness=false before Spanish CV is uploaded")
	}
	if len(readiness1.MissingLanguages) != 1 || readiness1.MissingLanguages[0] != domain.CVLanguageES {
		t.Fatalf("expected missing [es], got %v", readiness1.MissingLanguages)
	}

	pdfBytesES := []byte("%PDF-1.4\nSpanish CV bytes")
	savedES, err := s1.SaveCV(ctx, SaveCVParams{
		Language:      domain.CVLanguageES,
		Filename:      "ada-cv-es.pdf",
		PageCount:     2,
		ExtractedText: "Ada Lovelace - Ingeniera Backend Go",
		Content:       pdfBytesES,
	})
	if err != nil {
		t.Fatalf("SaveCV ES failed: %v", err)
	}

	if err := conn1.Close(); err != nil {
		t.Fatalf("close session 1 db: %v", err)
	}

	// Session 2: simulate full app restart by reopening SQLite.
	conn2, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db session 2: %v", err)
	}
	defer conn2.Close()
	s2 := New(conn2)

	reopenedProfile, err := s2.GetOrCreateProfile(ctx)
	if err != nil {
		t.Fatalf("GetOrCreateProfile after restart: %v", err)
	}
	if reopenedProfile.FullName != "Ada Lovelace" || reopenedProfile.LanguageMode != domain.LanguageModeBoth {
		t.Fatalf("unexpected reopened profile: %+v", reopenedProfile)
	}

	reopenedCVs, err := s2.ListCVs(ctx)
	if err != nil {
		t.Fatalf("ListCVs after restart: %v", err)
	}
	if len(reopenedCVs) != 2 {
		t.Fatalf("expected 2 CVs after restart, got %d", len(reopenedCVs))
	}

	readiness2 := ComputeReadiness(reopenedProfile, reopenedCVs)
	if !readiness2.Ready {
		t.Fatalf("expected readiness=true after both CVs saved, issues=%v", readiness2.Issues)
	}

	fileEN, err := s2.GetCVFile(ctx, savedEN.ID)
	if err != nil {
		t.Fatalf("GetCVFile EN failed: %v", err)
	}
	if !bytes.Equal(fileEN.Content, pdfBytesEN) {
		t.Fatalf("reopened EN PDF bytes mismatch")
	}

	fileES, err := s2.GetCVFile(ctx, savedES.ID)
	if err != nil {
		t.Fatalf("GetCVFile ES failed: %v", err)
	}
	if !bytes.Equal(fileES.Content, pdfBytesES) {
		t.Fatalf("reopened ES PDF bytes mismatch")
	}
}
