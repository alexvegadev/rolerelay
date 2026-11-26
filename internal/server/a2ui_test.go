package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"rolerelay/internal/a2ui"
	"rolerelay/internal/config"
	"rolerelay/internal/db"
	"rolerelay/internal/domain"
	"rolerelay/internal/store"
)

func TestMilestone4A2UITwoPendingCasesPersistAndReevaluate(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.sqlite")
	conn, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	srv := New(Options{
		Version:       "0.4.0-test",
		StoragePaths:  config.StoragePaths{Mode: config.StorageModePortable, DBPath: dbPath},
		SchemaVersion: 1,
		Address:       "127.0.0.1:41730",
		Store:         store.New(conn),
	})
	h := srv.Handler()

	// Setup Both mode + auto-send enabled + EN & ES CVs.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/profile", strings.NewReader(`{
		"fullName":"Ada Lovelace",
		"email":"ada@example.com",
		"targetRoles":["Backend Engineer"],
		"location":"Remote",
		"workArrangement":"remote",
		"languageMode":"both",
		"fitThreshold":70,
		"dailySendLimit":10
	}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/profile failed: %d", rec.Code)
	}
	_ = srv.opts.Store.SetSendingPreferences(ctx, true, false)

	_ = uploadCVRecorder(t, h, "en", "ada-en.pdf", makeMinimalPDF("Ada Lovelace Senior Go Backend Engineer SQLite REST APIs"))
	_ = uploadCVRecorder(t, h, "es", "ada-es.pdf", makeMinimalPDF("Ada Lovelace Ingeniera Backend Senior Go SQLite APIs REST"))

	// === CASE 1: Missing salary expectation ===
	v1Rec := httptest.NewRecorder()
	h.ServeHTTP(v1Rec, httptest.NewRequest(http.MethodPost, "/api/vacancies", strings.NewReader(`{
		"companyName":"SalaryCo",
		"title":"Senior Go Engineer",
		"location":"Remote",
		"workArrangement":"remote",
		"applicationEmail":"jobs@salaryco.test",
		"description":"We are looking for a backend engineer.\nRequirements:\n- Go and SQLite\n- REST APIs\nPlease include your salary expectation."
	}`)))
	var v1 struct {
		Item domain.VacancyWithEvaluation `json:"item"`
	}
	_ = json.Unmarshal(v1Rec.Body.Bytes(), &v1)

	ev1Rec := httptest.NewRecorder()
	h.ServeHTTP(ev1Rec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/vacancies/%d/evaluate", v1.Item.Vacancy.ID), nil))
	var ev1 struct {
		Item domain.VacancyWithEvaluation `json:"item"`
	}
	_ = json.Unmarshal(ev1Rec.Body.Bytes(), &ev1)
	app1ID := ev1.Item.Application.ID
	if ev1.Item.Application.State != domain.StatePending || ev1.Item.Application.QueuedForSend {
		t.Fatalf("expected Case 1 Pending & unqueued before salary answer, got %+v", ev1.Item.Application)
	}

	// Fetch A2UI panel for Case 1.
	p1Rec := httptest.NewRecorder()
	h.ServeHTTP(p1Rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/applications/%d/a2ui", app1ID), nil))
	if p1Rec.Code != http.StatusOK {
		t.Fatalf("GET a2ui Case 1 failed: %d %s", p1Rec.Code, p1Rec.Body.String())
	}
	var p1 struct {
		Panel a2ui.PanelResponse `json:"panel"`
	}
	_ = json.Unmarshal(p1Rec.Body.Bytes(), &p1)
	if p1.Panel.CatalogID != a2ui.CatalogID || !p1.Panel.HasInputs {
		t.Fatalf("unexpected Case 1 panel: %+v", p1.Panel)
	}

	// Submit invalid salary -> 422.
	badSalRec := httptest.NewRecorder()
	h.ServeHTTP(badSalRec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/applications/%d/a2ui/event", app1ID), strings.NewReader(`{
		"version":"1.0",
		"action":{"name":"save_review_answers","surfaceId":"s1","sourceComponentId":"submit-review-btn","context":{"salaryAmount":"0","salaryCurrency":["USD"]}}
	}`)))
	if badSalRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for zero salary, got %d", badSalRec.Code)
	}

	// Submit valid salary -> persisted + Go reevaluates -> now queued!
	goodSalRec := httptest.NewRecorder()
	h.ServeHTTP(goodSalRec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/applications/%d/a2ui/event", app1ID), strings.NewReader(`{
		"version":"1.0",
		"action":{"name":"save_review_answers","surfaceId":"s1","sourceComponentId":"submit-review-btn","context":{"salaryAmount":"120000","salaryCurrency":["USD"]}}
	}`)))
	if goodSalRec.Code != http.StatusOK {
		t.Fatalf("POST valid salary failed: %d %s", goodSalRec.Code, goodSalRec.Body.String())
	}
	var salResp struct {
		Application domain.ApplicationDetail `json:"application"`
	}
	_ = json.Unmarshal(goodSalRec.Body.Bytes(), &salResp)
	if !salResp.Application.Application.QueuedForSend || !salResp.Application.CanQueue {
		t.Fatalf("expected Case 1 queued after Go reevaluation, got %+v", salResp.Application.Application)
	}
	if !strings.Contains(salResp.Application.Application.Body, "USD 120000") {
		t.Fatalf("expected email body to include salary expectation, got %q", salResp.Application.Application.Body)
	}

	// === CASE 2: Ambiguous CV language using the SAME catalog ===
	v2Rec := httptest.NewRecorder()
	h.ServeHTTP(v2Rec, httptest.NewRequest(http.MethodPost, "/api/vacancies", strings.NewReader(`{
		"companyName":"AmbigCo",
		"title":"Go SQLite Dev",
		"location":"Remote",
		"workArrangement":"remote",
		"applicationEmail":"jobs@ambigco.test",
		"description":"Go SQLite REST APIs"
	}`)))
	var v2 struct {
		Item domain.VacancyWithEvaluation `json:"item"`
	}
	_ = json.Unmarshal(v2Rec.Body.Bytes(), &v2)

	ev2Rec := httptest.NewRecorder()
	h.ServeHTTP(ev2Rec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/vacancies/%d/evaluate", v2.Item.Vacancy.ID), nil))
	var ev2 struct {
		Item domain.VacancyWithEvaluation `json:"item"`
	}
	_ = json.Unmarshal(ev2Rec.Body.Bytes(), &ev2)
	app2ID := ev2.Item.Application.ID
	if ev2.Item.LatestEvaluation.LanguageStatus != domain.LanguageStatusAmbiguous || ev2.Item.Application.QueuedForSend {
		t.Fatalf("expected Case 2 ambiguous & unqueued before CV language choice, got %+v", ev2.Item.LatestEvaluation)
	}

	p2Rec := httptest.NewRecorder()
	h.ServeHTTP(p2Rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/applications/%d/a2ui", app2ID), nil))
	var p2 struct {
		Panel a2ui.PanelResponse `json:"panel"`
	}
	_ = json.Unmarshal(p2Rec.Body.Bytes(), &p2)
	if p2.Panel.CatalogID != p1.Panel.CatalogID || !p2.Panel.HasInputs {
		t.Fatalf("expected Case 2 to share identical catalog ID %q, got %+v", p1.Panel.CatalogID, p2.Panel)
	}

	langEventRec := httptest.NewRecorder()
	h.ServeHTTP(langEventRec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/applications/%d/a2ui/event", app2ID), strings.NewReader(`{
		"version":"1.0",
		"action":{"name":"save_review_answers","surfaceId":"s2","sourceComponentId":"submit-review-btn","context":{"cvLanguage":["es"]}}
	}`)))
	if langEventRec.Code != http.StatusOK {
		t.Fatalf("POST cv_language event failed: %d %s", langEventRec.Code, langEventRec.Body.String())
	}
	var langResp struct {
		Application domain.ApplicationDetail `json:"application"`
	}
	_ = json.Unmarshal(langEventRec.Body.Bytes(), &langResp)
	if langResp.Application.LatestEvaluation.LanguageStatus != domain.LanguageStatusOK {
		t.Fatalf("expected language status=ok after Go reevaluation, got %+v", langResp.Application.LatestEvaluation)
	}
	if len(langResp.Application.Application.AttachedCVs) != 1 || langResp.Application.Application.AttachedCVs[0].Language != domain.CVLanguageES {
		t.Fatalf("expected ES CV attached after A2UI choice, got %+v", langResp.Application.Application.AttachedCVs)
	}
	if !langResp.Application.Application.QueuedForSend {
		t.Fatalf("expected Case 2 queued after Go reevaluation, got %+v", langResp.Application.Application)
	}
}
