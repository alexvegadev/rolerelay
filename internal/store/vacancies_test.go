package store

import (
	"context"
	"path/filepath"
	"testing"

	"rolerelay/internal/connectors"
	"rolerelay/internal/db"
	"rolerelay/internal/domain"
)

func TestVacancyDeduplicationAcrossExternalIDAndCanonicalURL(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(ctx, filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s := New(conn)

	// 1. Import from Greenhouse.
	first, err := s.UpsertImportedVacancy(ctx, connectors.ImportedVacancy{
		SourceType:      domain.ProviderGreenhouse,
		Provider:        "greenhouse",
		CompanyBoard:    "acme",
		ExternalJobID:   "7788",
		CanonicalURL:    connectors.CanonicalizeURL("https://boards.greenhouse.io/acme/jobs/7788?gh_src=abc"),
		CompanyName:     "Acme",
		Title:           "Backend Engineer",
		Location:        "Remote",
		WorkArrangement: domain.WorkArrangementRemote,
		Description:     "We are looking for a Go engineer with SQLite experience.",
		ApplicationURL:  "https://boards.greenhouse.io/acme/jobs/7788?gh_src=abc",
		RawJSON:         "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || first.Deduplicated {
		t.Fatalf("expected created=true, got %+v", first)
	}

	// 2. Re-import same Greenhouse job -> deduplicated by external_id.
	second, err := s.UpsertImportedVacancy(ctx, connectors.ImportedVacancy{
		SourceType:      domain.ProviderGreenhouse,
		Provider:        "greenhouse",
		CompanyBoard:    "acme",
		ExternalJobID:   "7788",
		CanonicalURL:    connectors.CanonicalizeURL("https://boards.greenhouse.io/acme/jobs/7788"),
		CompanyName:     "Acme Corp",
		Title:           "Backend Engineer",
		Location:        "Remote",
		WorkArrangement: domain.WorkArrangementRemote,
		Description:     "We are looking for a Go engineer with SQLite experience.",
		ApplicationURL:  "https://boards.greenhouse.io/acme/jobs/7788",
		RawJSON:         "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Deduplicated || second.MatchedBy != "external_id" || second.Vacancy.ID != first.Vacancy.ID {
		t.Fatalf("expected deduplication by external_id, got %+v", second)
	}

	// 3. Manually enter the same posting URL with tracking params -> linked by canonical_url!
	third, err := s.CreateManualVacancy(ctx, domain.ManualVacancyInput{
		CompanyName:    "Acme Corp",
		Title:          "Backend Engineer",
		Location:       "Remote",
		Description:    "We are looking for a Go engineer with SQLite experience. Send CV to hiring@acme.dev",
		ApplicationURL: "https://boards.greenhouse.io/acme/jobs/7788/?utm_source=linkedin#apply",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !third.Deduplicated || third.MatchedBy != "canonical_url" || third.Vacancy.ID != first.Vacancy.ID {
		t.Fatalf("expected deduplication by canonical_url, got %+v", third)
	}
	if third.Vacancy.ApplicationEmail != "hiring@acme.dev" {
		t.Fatalf("expected merged email hiring@acme.dev, got %q", third.Vacancy.ApplicationEmail)
	}
}
