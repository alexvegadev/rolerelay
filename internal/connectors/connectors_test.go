package connectors

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"rolerelay/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestCanonicalizeURL(t *testing.T) {
	a := CanonicalizeURL("https://Boards.Greenhouse.io/acme/jobs/12345/?utm_source=linkedin&gh_src=abc123#apply")
	b := CanonicalizeURL("https://boards.greenhouse.io/acme/jobs/12345")
	if a != b {
		t.Fatalf("expected canonical URLs to match: %q vs %q", a, b)
	}
}

func TestGreenhouseFetchJobs(t *testing.T) {
	payload := `{"jobs":[{"id":9001,"title":"Senior Go Engineer","absolute_url":"https://boards.greenhouse.io/acme/jobs/9001?gh_src=xyz","location":{"name":"Remote - Americas"},"content":"&lt;p&gt;Requirements:&lt;/p&gt;&lt;ul&gt;&lt;li&gt;5+ years Go&lt;/li&gt;&lt;/ul&gt;&lt;p&gt;Send resume to jobs@acme.dev&lt;/p&gt;"}]}`

	client := &GreenhouseClient{
		BaseURL: "https://mock.greenhouse.test",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if !strings.Contains(r.URL.String(), "/v1/boards/acme/jobs?content=true") {
					t.Fatalf("unexpected url: %s", r.URL.String())
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(payload)),
				}, nil
			}),
		},
	}

	jobs, err := client.FetchJobs(context.Background(), "acme", "Acme Corp")
	if err != nil {
		t.Fatalf("FetchJobs failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	got := jobs[0]
	if got.ExternalJobID != "9001" || got.WorkArrangement != domain.WorkArrangementRemote {
		t.Fatalf("unexpected greenhouse job: %+v", got)
	}
	if got.ApplicationEmail != "jobs@acme.dev" {
		t.Fatalf("expected email jobs@acme.dev, got %q", got.ApplicationEmail)
	}
	if got.CanonicalURL != "https://boards.greenhouse.io/acme/jobs/9001" {
		t.Fatalf("unexpected canonical url: %q", got.CanonicalURL)
	}
}

func TestLeverFetchJobs(t *testing.T) {
	payload := `[{"id":"lv-42","text":"Ingeniero Backend","hostedUrl":"https://jobs.lever.co/globant/lv-42?lever-source=web","applyUrl":"https://jobs.lever.co/globant/lv-42/apply","workplaceType":"hybrid","categories":{"location":"Buenos Aires"},"descriptionPlain":"Buscamos desarrollador backend.","lists":[{"text":"Requisitos","content":"<ul><li>Experiencia en Go y SQLite</li></ul>"}]}]`

	client := &LeverClient{
		BaseURL: "https://mock.lever.test",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if !strings.Contains(r.URL.String(), "/v0/postings/globant?mode=json") {
					t.Fatalf("unexpected url: %s", r.URL.String())
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(payload)),
				}, nil
			}),
		},
	}

	jobs, err := client.FetchJobs(context.Background(), "globant", "Globant")
	if err != nil {
		t.Fatalf("FetchJobs failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	got := jobs[0]
	if got.ExternalJobID != "lv-42" || got.WorkArrangement != domain.WorkArrangementHybrid {
		t.Fatalf("unexpected lever job: %+v", got)
	}
	if !strings.Contains(got.Description, "Experiencia en Go y SQLite") {
		t.Fatalf("expected list items in description, got %q", got.Description)
	}
	if got.CanonicalURL != "https://jobs.lever.co/globant/lv-42" {
		t.Fatalf("unexpected canonical url: %q", got.CanonicalURL)
	}
}
