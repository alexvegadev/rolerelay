package a2ui

import (
	"testing"

	"rolerelay/internal/domain"
)

func TestBuildAndValidateTwoPendingCasesWithOneCatalog(t *testing.T) {
	profile := domain.Profile{
		ID:           1,
		LanguageMode: domain.LanguageModeBoth,
	}
	cvs := []domain.CVDocument{
		{ID: 1, Language: domain.CVLanguageEN, Version: 1, Filename: "ada-en.pdf", IsActive: true},
		{ID: 2, Language: domain.CVLanguageES, Version: 1, Filename: "ada-es.pdf", IsActive: true},
	}

	t.Run("Case 1: Missing salary expectation panel + validation", func(t *testing.T) {
		detail := domain.ApplicationDetail{
			Application: domain.Application{
				ID:            10,
				State:         domain.StatePending,
				PendingReason: "Pending review: missing required answer(s): Expected salary.",
				A2UIAnswers:   map[string]any{},
			},
			Vacancy: domain.Vacancy{ID: 100, Title: "Senior Go Engineer"},
			LatestEvaluation: &domain.Evaluation{
				Findings: []domain.RequirementFinding{
					{
						Requirement:      "Go and SQLite",
						Kind:             domain.RequirementMandatory,
						Met:              true,
						EvidenceVerified: true,
						EvidenceExcerpt:  "6 years Go and SQLite",
						Reason:           "Matched in EN CV",
					},
				},
				MissingAnswers: []domain.MissingAnswer{
					{Key: "salary_expectation", Label: "Expected salary", Kind: "salary", Required: true},
				},
			},
		}

		panel := BuildPendingReviewPanel(detail, profile, cvs)
		if panel.CatalogID != CatalogID || !panel.HasInputs || len(panel.Messages) != 1 {
			t.Fatalf("unexpected salary panel: %+v", panel)
		}
		if panel.Messages[0].Version != "1.0" || panel.Messages[0].CreateSurface == nil {
			t.Fatalf("expected A2UI v1.0 createSurface message")
		}

		// Reject invalid negative salary.
		_, _, err := ValidateAndExtractAnswers(ActionPayload{
			Name: "save_review_answers",
			Context: map[string]any{
				"salaryAmount":   "-100",
				"salaryCurrency": []any{"USD"},
			},
		}, detail, profile, cvs)
		if err == nil {
			t.Fatalf("expected validation error for negative salary")
		}

		// Accept valid salary + currency.
		answers, summary, err := ValidateAndExtractAnswers(ActionPayload{
			Name: "save_review_answers",
			Context: map[string]any{
				"salaryAmount":   "125000",
				"salaryCurrency": []any{"USD"},
			},
		}, detail, profile, cvs)
		if err != nil {
			t.Fatalf("expected valid salary answer, got err=%v", err)
		}
		sal, ok := answers["salary_expectation"].(map[string]any)
		if !ok || sal["currency"] != "USD" || sal["amount"].(float64) != 125000 {
			t.Fatalf("unexpected salary answer: %+v (%s)", answers, summary)
		}
	})

	t.Run("Case 2: Ambiguous CV language panel + validation using same catalog", func(t *testing.T) {
		detail := domain.ApplicationDetail{
			Application: domain.Application{
				ID:            11,
				State:         domain.StatePending,
				PendingReason: "Vacancy language is ambiguous.",
				A2UIAnswers:   map[string]any{},
			},
			Vacancy: domain.Vacancy{ID: 101, Title: "Go SQLite Dev"},
			LatestEvaluation: &domain.Evaluation{
				LanguageStatus: domain.LanguageStatusAmbiguous,
				MissingAnswers: []domain.MissingAnswer{
					{Key: "cv_language", Label: "CV language", Kind: "cv_language", Required: true, Options: []string{"en", "es"}},
				},
			},
		}

		panel := BuildPendingReviewPanel(detail, profile, cvs)
		if panel.CatalogID != CatalogID || !panel.HasInputs {
			t.Fatalf("expected same catalog ID %q and inputs for CV language panel", CatalogID)
		}

		answers, _, err := ValidateAndExtractAnswers(ActionPayload{
			Name: "save_review_answers",
			Context: map[string]any{
				"cvLanguage": []any{"es"},
			},
		}, detail, profile, cvs)
		if err != nil {
			t.Fatalf("expected valid CV language choice, got %v", err)
		}
		if answers["cv_language"] != "es" {
			t.Fatalf("expected cv_language=es, got %+v", answers)
		}
	})
}
