package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"rolerelay/internal/config"
	"rolerelay/internal/connectors"
	"rolerelay/internal/db"
	"rolerelay/internal/domain"
	"rolerelay/internal/store"
)

type roundTripFn func(*http.Request) (*http.Response, error)

func (f roundTripFn) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMilestone2BilingualImportSelectionAndEvaluationAPI(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.sqlite")
	conn, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	mockGH := &connectors.GreenhouseClient{
		BaseURL: "https://mock.greenhouse.test",
		HTTPClient: &http.Client{
			Transport: roundTripFn(func(*http.Request) (*http.Response, error) {
				body := `{"jobs":[
					{"id":101,"title":"Senior Go Backend Engineer","absolute_url":"https://boards.greenhouse.io/acme/jobs/101?gh_src=feed","location":{"name":"Remote"},"content":"<p>We are looking for a backend engineer.</p><p>Requirements:</p><ul><li>Go and SQLite</li><li>REST APIs</li></ul><p>Send CV to jobs@acme.dev</p>"},
					{"id":102,"title":"Ingeniera Backend Go","absolute_url":"https://boards.greenhouse.io/acme/jobs/102","location":{"name":"Remoto"},"content":"<p>Buscamos una ingeniera backend con experiencia.</p><p>Requisitos:</p><ul><li>Go y SQLite</li><li>Kubernetes y Rust kernel drivers</li></ul>"}
				]}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			}),
		},
	}

	srv := New(Options{
		Version:       "0.2.0-test",
		StoragePaths:  config.StoragePaths{Mode: config.StorageModePortable, DBPath: dbPath},
		SchemaVersion: 1,
		Address:       "127.0.0.1:41730",
		Store:         store.New(conn),
		Greenhouse:    mockGH,
	})
	h := srv.Handler()

	// 1. Configure profile in Both mode.
	putReq := httptest.NewRequest(http.MethodPut, "/api/profile", strings.NewReader(`{
		"fullName":"Ada Lovelace",
		"email":"ada@example.com",
		"targetRoles":["Backend Engineer"],
		"location":"Remote",
		"workArrangement":"remote",
		"languageMode":"both",
		"fitThreshold":70,
		"dailySendLimit":10
	}`))
	putRec := httptest.NewRecorder()
	h.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/profile failed: %d %s", putRec.Code, putRec.Body.String())
	}

	// 2. Upload ONLY English CV first.
	enPDF := makeMinimalPDF("Ada Lovelace Senior Backend Engineer Go SQLite REST APIs TypeScript")
	if rec := uploadCVRecorder(t, h, "en", "ada-en.pdf", enPDF); rec.Code != http.StatusCreated {
		t.Fatalf("upload EN CV failed: %d %s", rec.Code, rec.Body.String())
	}

	// 3. Add Greenhouse board and sync English + Spanish jobs.
	addBoardRec := httptest.NewRecorder()
	h.ServeHTTP(addBoardRec, httptest.NewRequest(http.MethodPost, "/api/boards", strings.NewReader(`{"provider":"greenhouse","companyBoard":"acme","displayName":"Acme"}`)))
	if addBoardRec.Code != http.StatusCreated {
		t.Fatalf("POST /api/boards failed: %d %s", addBoardRec.Code, addBoardRec.Body.String())
	}
	var boardResp struct {
		Board domain.JobBoard `json:"board"`
	}
	_ = json.Unmarshal(addBoardRec.Body.Bytes(), &boardResp)

	syncRec := httptest.NewRecorder()
	h.ServeHTTP(syncRec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/boards/%d/sync", boardResp.Board.ID), nil))
	if syncRec.Code != http.StatusOK {
		t.Fatalf("POST /api/boards/sync failed: %d %s", syncRec.Code, syncRec.Body.String())
	}
	var syncResp struct {
		Created      int                            `json:"created"`
		Deduplicated int                            `json:"deduplicated"`
		Vacancies    []domain.VacancyWithEvaluation `json:"vacancies"`
	}
	_ = json.Unmarshal(syncRec.Body.Bytes(), &syncResp)
	if syncResp.Created != 2 || len(syncResp.Vacancies) != 2 {
		t.Fatalf("expected 2 created vacancies, got %+v", syncResp)
	}

	var enVacID, esVacID int64
	for _, item := range syncResp.Vacancies {
		if item.Vacancy.DetectedLanguage == "en" {
			enVacID = item.Vacancy.ID
		} else if item.Vacancy.DetectedLanguage == "es" {
			esVacID = item.Vacancy.ID
		}
	}
	if enVacID == 0 || esVacID == 0 {
		t.Fatalf("expected both EN and ES vacancies detected, got en=%d es=%d", enVacID, esVacID)
	}

	// 4. Evaluating in Both mode WITHOUT Spanish CV must report missing_required_cv.
	evalBeforeES := httptest.NewRecorder()
	h.ServeHTTP(evalBeforeES, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/vacancies/%d/evaluate", enVacID), nil))
	if evalBeforeES.Code != http.StatusOK {
		t.Fatalf("evaluate before ES CV failed: %d %s", evalBeforeES.Code, evalBeforeES.Body.String())
	}
	var beforeResp struct {
		Item domain.VacancyWithEvaluation `json:"item"`
	}
	_ = json.Unmarshal(evalBeforeES.Body.Bytes(), &beforeResp)
	if beforeResp.Item.LatestEvaluation.LanguageStatus != domain.LanguageStatusMissingCV {
		t.Fatalf("expected missing_required_cv when Spanish CV missing in Both mode, got %s", beforeResp.Item.LatestEvaluation.LanguageStatus)
	}

	// 5. Upload Spanish CV now.
	esPDF := makeMinimalPDF("Ada Lovelace Ingeniera Backend Senior experiencia en Go SQLite y APIs REST")
	if rec := uploadCVRecorder(t, h, "es", "ada-es.pdf", esPDF); rec.Code != http.StatusCreated {
		t.Fatalf("upload ES CV failed: %d %s", rec.Code, rec.Body.String())
	}

	// 6. Evaluate English vacancy -> selects EN CV and qualifies.
	evalEN := httptest.NewRecorder()
	h.ServeHTTP(evalEN, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/vacancies/%d/evaluate", enVacID), nil))
	var enEvalResp struct {
		Item domain.VacancyWithEvaluation `json:"item"`
	}
	_ = json.Unmarshal(evalEN.Body.Bytes(), &enEvalResp)
	if len(enEvalResp.Item.LatestEvaluation.SelectedCVs) != 1 || enEvalResp.Item.LatestEvaluation.SelectedCVs[0].Language != domain.CVLanguageEN {
		t.Fatalf("expected EN CV selected for EN vacancy, got %+v", enEvalResp.Item.LatestEvaluation.SelectedCVs)
	}
	if !enEvalResp.Item.LatestEvaluation.PassedThreshold {
		t.Fatalf("expected EN vacancy to pass threshold, got %+v", enEvalResp.Item.LatestEvaluation)
	}

	// 7. Evaluate Spanish vacancy (which requires Kubernetes + Rust kernel drivers missing from CV)
	//    -> selects ES CV and stays Pending with reviewable explanation!
	evalES := httptest.NewRecorder()
	h.ServeHTTP(evalES, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/vacancies/%d/evaluate", esVacID), nil))
	var esEvalResp struct {
		Item domain.VacancyWithEvaluation `json:"item"`
	}
	_ = json.Unmarshal(evalES.Body.Bytes(), &esEvalResp)
	if len(esEvalResp.Item.LatestEvaluation.SelectedCVs) != 1 || esEvalResp.Item.LatestEvaluation.SelectedCVs[0].Language != domain.CVLanguageES {
		t.Fatalf("expected ES CV selected for ES vacancy, got %+v", esEvalResp.Item.LatestEvaluation.SelectedCVs)
	}
	if esEvalResp.Item.Application.State != domain.StatePending {
		t.Fatalf("expected Pending state for missing mandatory evidence, got %s", esEvalResp.Item.Application.State)
	}
	if !strings.Contains(esEvalResp.Item.Application.PendingReason, "insufficient verified CV evidence") {
		t.Fatalf("expected reviewable explanation in pendingReason, got %q", esEvalResp.Item.Application.PendingReason)
	}
}
