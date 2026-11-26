package langselect

import (
	"testing"

	"rolerelay/internal/domain"
)

func TestDetectRequestedAndListingLanguage(t *testing.T) {
	if got := DetectRequestedLanguage("Please submit your CV in English only."); got != "en" {
		t.Fatalf("expected requested=en, got %q", got)
	}
	if got := DetectRequestedLanguage("Por favor enviar tu CV en español."); got != "es" {
		t.Fatalf("expected requested=es, got %q", got)
	}
	if got := DetectRequestedLanguage("Please attach your CV in English and Spanish."); got != "both" {
		t.Fatalf("expected requested=both, got %q", got)
	}

	if got := DetectListingLanguage("Senior Backend Engineer", "We are looking for a strong Go engineer with 5+ years of experience building scalable APIs."); got != "en" {
		t.Fatalf("expected detected=en, got %q", got)
	}
	if got := DetectListingLanguage("Ingeniero Backend Senior", "Buscamos un desarrollador con experiencia excluyente en Go y SQLite para trabajar en nuestro equipo remoto."); got != "es" {
		t.Fatalf("expected detected=es, got %q", got)
	}
	if got := DetectListingLanguage("Dev", "Go SQLite API"); got != "ambiguous" {
		t.Fatalf("expected detected=ambiguous for short language-neutral text, got %q", got)
	}
}

func TestSelectCVsRules(t *testing.T) {
	enCV := domain.CVDocument{ID: 1, Language: domain.CVLanguageEN, Version: 1, Filename: "cv-en.pdf", IsActive: true}
	esCV := domain.CVDocument{ID: 2, Language: domain.CVLanguageES, Version: 1, Filename: "cv-es.pdf", IsActive: true}

	t.Run("both mode requires both CVs", func(t *testing.T) {
		profile := domain.Profile{LanguageMode: domain.LanguageModeBoth}
		vac := domain.Vacancy{DetectedLanguage: "en"}
		res := SelectCVs(profile, []domain.CVDocument{enCV}, vac, true, "")
		if res.Status != domain.LanguageStatusMissingCV {
			t.Fatalf("expected missing_required_cv, got %+v", res)
		}
	})

	t.Run("both mode selects Spanish CV for Spanish vacancy and English CV for English vacancy", func(t *testing.T) {
		profile := domain.Profile{LanguageMode: domain.LanguageModeBoth}

		resES := SelectCVs(profile, []domain.CVDocument{enCV, esCV}, domain.Vacancy{DetectedLanguage: "es"}, true, "")
		if resES.Status != domain.LanguageStatusOK || len(resES.SelectedCVs) != 1 || resES.SelectedCVs[0].Language != domain.CVLanguageES {
			t.Fatalf("expected ES CV selected, got %+v", resES)
		}

		resEN := SelectCVs(profile, []domain.CVDocument{enCV, esCV}, domain.Vacancy{DetectedLanguage: "en"}, true, "")
		if resEN.Status != domain.LanguageStatusOK || len(resEN.SelectedCVs) != 1 || resEN.SelectedCVs[0].Language != domain.CVLanguageEN {
			t.Fatalf("expected EN CV selected, got %+v", resEN)
		}
	})

	t.Run("explicit requested language overrides listing language", func(t *testing.T) {
		profile := domain.Profile{LanguageMode: domain.LanguageModeBoth}
		vac := domain.Vacancy{RequestedLanguage: "en", DetectedLanguage: "es"}
		res := SelectCVs(profile, []domain.CVDocument{enCV, esCV}, vac, true, "")
		if res.Status != domain.LanguageStatusOK || res.SelectedCVs[0].Language != domain.CVLanguageEN {
			t.Fatalf("expected EN CV due to explicit request, got %+v", res)
		}
	})

	t.Run("both requested attaches both CVs when channel supports it", func(t *testing.T) {
		profile := domain.Profile{LanguageMode: domain.LanguageModeBoth}
		vac := domain.Vacancy{RequestedLanguage: "both", DetectedLanguage: "en"}
		res := SelectCVs(profile, []domain.CVDocument{enCV, esCV}, vac, true, "")
		if res.Status != domain.LanguageStatusOK || len(res.SelectedCVs) != 2 {
			t.Fatalf("expected 2 CVs attached, got %+v", res)
		}
	})

	t.Run("ambiguous listing routes to review panel", func(t *testing.T) {
		profile := domain.Profile{LanguageMode: domain.LanguageModeBoth}
		vac := domain.Vacancy{DetectedLanguage: "ambiguous"}
		res := SelectCVs(profile, []domain.CVDocument{enCV, esCV}, vac, true, "")
		if res.Status != domain.LanguageStatusAmbiguous || !res.NeedsReview || len(res.MissingAnswers) != 1 {
			t.Fatalf("expected ambiguous review case, got %+v", res)
		}
	})

	t.Run("language mismatch outside configured mode", func(t *testing.T) {
		profile := domain.Profile{LanguageMode: domain.LanguageModeEN}
		vac := domain.Vacancy{DetectedLanguage: "es"}
		res := SelectCVs(profile, []domain.CVDocument{enCV}, vac, true, "")
		if res.Status != domain.LanguageStatusMismatch || !res.IsMismatch {
			t.Fatalf("expected mismatch, got %+v", res)
		}
	})
}
