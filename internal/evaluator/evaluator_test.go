package evaluator

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"rolerelay/internal/domain"
)

type mockRoundTrip func(*http.Request) (*http.Response, error)

func (f mockRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestBilingualEvaluationSelectionAndEvidenceVerification(t *testing.T) {
	ctx := context.Background()
	enCV := domain.CVDocument{
		ID:            10,
		Language:      domain.CVLanguageEN,
		Version:       1,
		Filename:      "cv-en.pdf",
		IsActive:      true,
		ExtractedText: "Ada Lovelace - Senior Backend Engineer.\n6 years building Go services, SQLite storage engines, and REST APIs.\nExperience with React and TypeScript dashboards.",
	}
	esCV := domain.CVDocument{
		ID:            20,
		Language:      domain.CVLanguageES,
		Version:       1,
		Filename:      "cv-es.pdf",
		IsActive:      true,
		ExtractedText: "Ada Lovelace - Ingeniera Backend Senior.\n6 años desarrollando microservicios en Go, SQLite y APIs REST.\nExperiencia con React y TypeScript.",
	}

	profileBoth := domain.Profile{
		ID:              1,
		FullName:        "Ada Lovelace",
		TargetRoles:     []string{"Backend Engineer"},
		WorkArrangement: domain.WorkArrangementRemote,
		LanguageMode:    domain.LanguageModeBoth,
		FitThreshold:    70,
	}

	t.Run("Both mode blocks evaluation when Spanish CV is missing", func(t *testing.T) {
		vac := domain.Vacancy{
			ID:               1,
			Title:            "Senior Go Engineer",
			WorkArrangement:  domain.WorkArrangementRemote,
			DetectedLanguage: "en",
			Description:      "Requirements:\n- Go and SQLite experience",
		}
		res, err := EvaluateVacancy(ctx, profileBoth, []domain.CVDocument{enCV}, vac, nil, NewLocalHeuristicEvaluator())
		if err != nil {
			t.Fatal(err)
		}
		if res.Evaluation.LanguageStatus != domain.LanguageStatusMissingCV {
			t.Fatalf("expected missing_required_cv, got %s", res.Evaluation.LanguageStatus)
		}
		if res.ApplicationState != domain.StatePending {
			t.Fatalf("expected pending state, got %s", res.ApplicationState)
		}
	})

	t.Run("Spanish vacancy selects Spanish CV and English vacancy selects English CV", func(t *testing.T) {
		vacEN := domain.Vacancy{
			ID:               2,
			CompanyName:      "Acme",
			Title:            "Senior Go Engineer",
			WorkArrangement:  domain.WorkArrangementRemote,
			DetectedLanguage: "en",
			ApplicationEmail: "jobs@acme.dev",
			Description:      "Requirements:\n- Go and SQLite\n- REST APIs\nNice to have:\n- React",
		}
		resEN, err := EvaluateVacancy(ctx, profileBoth, []domain.CVDocument{enCV, esCV}, vacEN, nil, NewLocalHeuristicEvaluator())
		if err != nil {
			t.Fatal(err)
		}
		if len(resEN.Evaluation.SelectedCVIDs) != 1 || resEN.Evaluation.SelectedCVIDs[0] != enCV.ID {
			t.Fatalf("expected EN CV %d selected, got %v", enCV.ID, resEN.Evaluation.SelectedCVIDs)
		}
		if !resEN.Evaluation.PassedThreshold || !strings.Contains(resEN.PendingReason, "Qualified") {
			t.Fatalf("expected qualified EN vacancy, got score=%d reason=%q", resEN.Evaluation.FitScore, resEN.PendingReason)
		}

		vacES := domain.Vacancy{
			ID:               3,
			CompanyName:      "TechLatam",
			Title:            "Ingeniera Backend Go",
			WorkArrangement:  domain.WorkArrangementRemote,
			DetectedLanguage: "es",
			ApplicationEmail: "talento@techlatam.com",
			Description:      "Requisitos:\n- Go y SQLite\n- APIs REST\nDeseable:\n- TypeScript",
		}
		resES, err := EvaluateVacancy(ctx, profileBoth, []domain.CVDocument{enCV, esCV}, vacES, nil, NewLocalHeuristicEvaluator())
		if err != nil {
			t.Fatal(err)
		}
		if len(resES.Evaluation.SelectedCVIDs) != 1 || resES.Evaluation.SelectedCVIDs[0] != esCV.ID {
			t.Fatalf("expected ES CV %d selected, got %v", esCV.ID, resES.Evaluation.SelectedCVIDs)
		}
		if !resES.Evaluation.PassedThreshold || !strings.Contains(resES.PendingReason, "Qualified") {
			t.Fatalf("expected qualified ES vacancy, got score=%d reason=%q", resES.Evaluation.FitScore, resES.PendingReason)
		}
	})

	t.Run("Missing evidence for mandatory requirement stays Pending with reviewable explanation", func(t *testing.T) {
		vac := domain.Vacancy{
			ID:               4,
			CompanyName:      "CloudCorp",
			Title:            "Staff Rust & Kubernetes Engineer",
			WorkArrangement:  domain.WorkArrangementRemote,
			DetectedLanguage: "en",
			Description:      "Requirements:\n- Go and SQLite\n- 5+ years Kubernetes and Rust kernel drivers",
		}
		res, err := EvaluateVacancy(ctx, profileBoth, []domain.CVDocument{enCV, esCV}, vac, nil, NewLocalHeuristicEvaluator())
		if err != nil {
			t.Fatal(err)
		}
		if res.ApplicationState != domain.StatePending {
			t.Fatalf("expected pending state for missing mandatory evidence, got %s", res.ApplicationState)
		}
		if !strings.Contains(res.PendingReason, "insufficient verified CV evidence") {
			t.Fatalf("expected reviewable explanation in PendingReason, got %q", res.PendingReason)
		}
	})

	t.Run("Go verifier rejects hallucinated excerpt from external LLM endpoint", func(t *testing.T) {
		llmJSON := `{"choices":[{"message":{"content":"{\"fitScore\":95,\"summary\":\"Great fit\",\"findings\":[{\"requirement\":\"Rust experience\",\"kind\":\"mandatory\",\"met\":true,\"evidenceExcerpt\":\"10 years of Rust systems programming\",\"cvLanguage\":\"en\",\"reason\":\"Claimed match\"}],\"missingAnswers\":[]}"}}]}`
		llmEval := &OpenAICompatibleEvaluator{
			EndpointURL: "https://mock.llm.test/v1",
			Model:       "mock-model",
			HTTPClient: &http.Client{
				Transport: mockRoundTrip(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(llmJSON)),
					}, nil
				}),
			},
		}

		vac := domain.Vacancy{
			ID:               5,
			CompanyName:      "SystemsInc",
			Title:            "Rust Engineer",
			WorkArrangement:  domain.WorkArrangementRemote,
			DetectedLanguage: "en",
			Description:      "Requirements:\n- Rust experience",
		}
		res, err := EvaluateVacancy(ctx, profileBoth, []domain.CVDocument{enCV, esCV}, vac, nil, llmEval)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Evaluation.Findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(res.Evaluation.Findings))
		}
		f := res.Evaluation.Findings[0]
		if f.EvidenceVerified || f.Met {
			t.Fatalf("expected hallucinated excerpt to be rejected by Go verifier, got %+v", f)
		}
		if res.ApplicationState != domain.StatePending {
			t.Fatalf("expected pending state after unverified mandatory claim, got %s", res.ApplicationState)
		}
	})

	t.Run("Confirmed work arrangement mismatch marks Discarded with reason", func(t *testing.T) {
		vac := domain.Vacancy{
			ID:               6,
			CompanyName:      "OfficeCo",
			Title:            "Go Engineer",
			WorkArrangement:  domain.WorkArrangementOnsite,
			DetectedLanguage: "en",
			Description:      "Requirements:\n- Go and SQLite",
		}
		res, err := EvaluateVacancy(ctx, profileBoth, []domain.CVDocument{enCV, esCV}, vac, nil, NewLocalHeuristicEvaluator())
		if err != nil {
			t.Fatal(err)
		}
		if res.ApplicationState != domain.StateDiscarded || !strings.Contains(res.DiscardReason, "Work arrangement mismatch") {
			t.Fatalf("expected discarded with work arrangement mismatch, got state=%s reason=%q", res.ApplicationState, res.DiscardReason)
		}
	})
}
