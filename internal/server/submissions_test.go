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

	"rolerelay/internal/config"
	"rolerelay/internal/credstore"
	"rolerelay/internal/db"
	"rolerelay/internal/domain"
	"rolerelay/internal/mailer"
	"rolerelay/internal/store"
)

type stubMailer struct {
	sent []mailer.Message
}

func (s *stubMailer) Send(_ context.Context, _ mailer.SMTPConfig, msg mailer.Message, onBeforeData func() error) (mailer.SendResult, error) {
	if onBeforeData != nil {
		if err := onBeforeData(); err != nil {
			return mailer.SendResult{}, err
		}
	}
	s.sent = append(s.sent, msg)
	return mailer.SendResult{MessageID: "<stub-1@test>"}, nil
}

func TestSubmissionSettingsCredStoreAndManualStateAPI(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.sqlite")
	conn, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	creds := credstore.NewMemoryStore()
	sm := &stubMailer{}
	srv := New(Options{
		Version:       "0.3.0-test",
		StoragePaths:  config.StoragePaths{Mode: config.StorageModePortable, DBPath: dbPath},
		SchemaVersion: 1,
		Address:       "127.0.0.1:41730",
		Store:         store.New(conn),
		CredStore:     creds,
		Mailer:        sm,
	})
	h := srv.Handler()

	// 1. Save SMTP settings; password must go to OS credstore and never return in JSON.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/settings/email", strings.NewReader(`{
		"host":"127.0.0.1",
		"port":2525,
		"username":"ada@example.com",
		"password":"top-secret-password",
		"fromName":"Ada",
		"fromEmail":"ada@example.com",
		"tlsMode":"none",
		"autoSendEnabled":true,
		"sendingPaused":false,
		"dailySendLimit":10
	}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings/email failed: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "top-secret-password") {
		t.Fatalf("password leaked in API response")
	}

	cred, err := creds.Get(credstore.DefaultSMTPTarget)
	if err != nil || cred.Secret != "top-secret-password" {
		t.Fatalf("expected secret stored in credstore, got %+v err=%v", cred, err)
	}

	// 2. Test SMTP connection endpoint.
	testRec := httptest.NewRecorder()
	h.ServeHTTP(testRec, httptest.NewRequest(http.MethodPost, "/api/settings/email/test", strings.NewReader(`{"recipient":"inbox@example.com"}`)))
	if testRec.Code != http.StatusOK || len(sm.sent) != 1 {
		t.Fatalf("POST /api/settings/email/test failed: %d %s sent=%d", testRec.Code, testRec.Body.String(), len(sm.sent))
	}

	// 3. Simulate moving DB to a new machine where OS credstore lacks the password -> needsReconnect=true.
	_ = creds.Delete(credstore.DefaultSMTPTarget)
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/api/settings/email", nil))
	var getResp struct {
		Settings domain.EmailSettings `json:"settings"`
	}
	_ = json.Unmarshal(getRec.Body.Bytes(), &getResp)
	if !getResp.Settings.NeedsReconnect || getResp.Settings.HasCredentials {
		t.Fatalf("expected needsReconnect=true when OS cred missing, got %+v", getResp.Settings)
	}

	// 4. Create external-form vacancy, evaluate, and record manual submission as Sent.
	_ = uploadCVRecorder(t, h, "en", "ada-en.pdf", makeMinimalPDF("Ada Lovelace Senior Go Backend Engineer SQLite REST APIs"))
	vacRec := httptest.NewRecorder()
	h.ServeHTTP(vacRec, httptest.NewRequest(http.MethodPost, "/api/vacancies", strings.NewReader(`{
		"companyName":"PortalCo",
		"title":"Senior Go Engineer",
		"location":"Remote",
		"workArrangement":"remote",
		"applicationUrl":"https://boards.greenhouse.io/portalco/jobs/42",
		"description":"We are looking for a backend engineer.\nRequirements:\n- Go and SQLite"
	}`)))
	var vacResp struct {
		Item domain.VacancyWithEvaluation `json:"item"`
	}
	_ = json.Unmarshal(vacRec.Body.Bytes(), &vacResp)

	evalRec := httptest.NewRecorder()
	h.ServeHTTP(evalRec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/vacancies/%d/evaluate", vacResp.Item.Vacancy.ID), nil))
	var evalResp struct {
		Item domain.VacancyWithEvaluation `json:"item"`
	}
	_ = json.Unmarshal(evalRec.Body.Bytes(), &evalResp)
	appID := evalResp.Item.Application.ID

	stateRec := httptest.NewRecorder()
	h.ServeHTTP(stateRec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/applications/%d/state", appID), strings.NewReader(`{
		"state":"sent",
		"reason":"Completed official Greenhouse form manually"
	}`)))
	if stateRec.Code != http.StatusOK {
		t.Fatalf("POST /api/applications/%d/state failed: %d %s", appID, stateRec.Code, stateRec.Body.String())
	}
	var stateResp struct {
		Application domain.ApplicationDetail `json:"application"`
	}
	_ = json.Unmarshal(stateRec.Body.Bytes(), &stateResp)
	if stateResp.Application.Application.State != domain.StateSent {
		t.Fatalf("expected state=sent, got %s", stateResp.Application.Application.State)
	}
}
