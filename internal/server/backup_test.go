package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"rolerelay/internal/config"
	"rolerelay/internal/credstore"
	"rolerelay/internal/db"
	"rolerelay/internal/domain"
	"rolerelay/internal/store"
)

func TestMilestone5AllFiveStatesBackupExportAndRestoreAPI(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	// === Installation A ===
	dbA := filepath.Join(tmp, "a", "app.sqlite")
	if err := config.EnsureDataDir(config.StoragePaths{DataDir: filepath.Dir(dbA)}); err != nil {
		t.Fatal(err)
	}
	connA, err := db.Open(ctx, dbA)
	if err != nil {
		t.Fatal(err)
	}
	storeA := store.NewWithPath(connA, dbA)
	defer storeA.Close()

	srvA := New(Options{
		Version:       "1.0.0-test",
		StoragePaths:  config.StoragePaths{Mode: config.StorageModePortable, DBPath: dbA},
		SchemaVersion: 1,
		Address:       "127.0.0.1:41730",
		Store:         storeA,
		CredStore:     credstore.NewMemoryStore(),
	})
	hA := srvA.Handler()

	// Setup Both mode + EN & ES CVs.
	hA.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/profile", strings.NewReader(`{
		"fullName":"Ada Lovelace","email":"ada@example.com","targetRoles":["Backend Engineer"],
		"location":"Remote","workArrangement":"remote","languageMode":"both","fitThreshold":70,"dailySendLimit":10
	}`)))
	pdfEN := makeMinimalPDF("Ada Lovelace Senior Go Backend Engineer SQLite REST APIs")
	pdfES := makeMinimalPDF("Ada Lovelace Ingeniera Backend Senior Go SQLite APIs REST")
	upEN := uploadCVRecorder(t, hA, "en", "ada-en.pdf", pdfEN)
	upES := uploadCVRecorder(t, hA, "es", "ada-es.pdf", pdfES)
	var cvENResp, cvESResp struct{ CV domain.CVDocument }
	_ = json.Unmarshal(upEN.Body.Bytes(), &cvENResp)
	_ = json.Unmarshal(upES.Body.Bytes(), &cvESResp)

	// Create 5 vacancies and exercise all 5 application states.
	targetStates := []struct {
		company string
		state   domain.ApplicationState
		reason  string
	}{
		{"PendingCo", domain.StatePending, "Awaiting salary answer"},
		{"SentCo", domain.StateSent, "Submitted via email"},
		{"ProgressCo", domain.StateInProgress, "Recruiter confirmed review"},
		{"InterviewCo", domain.StateInterviewPending, "Technical interview scheduled"},
		{"DiscardCo", domain.StateDiscarded, "Role closed by employer"},
	}

	for i, tc := range targetStates {
		vRec := httptest.NewRecorder()
		hA.ServeHTTP(vRec, httptest.NewRequest(http.MethodPost, "/api/vacancies", strings.NewReader(fmt.Sprintf(`{
			"companyName":"%s","title":"Go Engineer %d","location":"Remote","workArrangement":"remote",
			"applicationEmail":"jobs%d@example.test",
			"description":"We are looking for a backend engineer.\nRequirements:\n- Go and SQLite\n- REST APIs"
		}`, tc.company, i+1, i+1))))
		var v struct{ Item domain.VacancyWithEvaluation }
		_ = json.Unmarshal(vRec.Body.Bytes(), &v)

		evRec := httptest.NewRecorder()
		hA.ServeHTTP(evRec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/vacancies/%d/evaluate", v.Item.Vacancy.ID), nil))
		var ev struct{ Item domain.VacancyWithEvaluation }
		_ = json.Unmarshal(evRec.Body.Bytes(), &ev)

		if tc.state != domain.StatePending {
			stRec := httptest.NewRecorder()
			hA.ServeHTTP(stRec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/applications/%d/state", ev.Item.Application.ID), strings.NewReader(fmt.Sprintf(`{"state":"%s","reason":"%s"}`, tc.state, tc.reason))))
			if stRec.Code != http.StatusOK {
				t.Fatalf("failed updating state %s: %d %s", tc.state, stRec.Code, stRec.Body.String())
			}
		}
	}

	// Export consistent SQLite backup over HTTP.
	expRec := httptest.NewRecorder()
	hA.ServeHTTP(expRec, httptest.NewRequest(http.MethodGet, "/api/backup/export", nil))
	if expRec.Code != http.StatusOK {
		t.Fatalf("GET /api/backup/export failed: %d %s", expRec.Code, expRec.Body.String())
	}
	backupBytes := expRec.Body.Bytes()
	if len(backupBytes) < 1024 {
		t.Fatalf("exported backup suspiciously small: %d bytes", len(backupBytes))
	}

	// === Installation B (fresh second installation) ===
	dbB := filepath.Join(tmp, "b", "app.sqlite")
	if err := config.EnsureDataDir(config.StoragePaths{DataDir: filepath.Dir(dbB)}); err != nil {
		t.Fatal(err)
	}
	connB, err := db.Open(ctx, dbB)
	if err != nil {
		t.Fatal(err)
	}
	storeB := store.NewWithPath(connB, dbB)
	defer storeB.Close()

	srvB := New(Options{
		Version:       "1.0.0-test",
		StoragePaths:  config.StoragePaths{Mode: config.StorageModeInstalled, DBPath: dbB},
		SchemaVersion: 1,
		Address:       "127.0.0.1:41731",
		Store:         storeB,
		CredStore:     credstore.NewMemoryStore(),
	})
	hB := srvB.Handler()

	// Restore exported backup onto Installation B via multipart POST.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", "rolerelay-backup.sqlite")
	_, _ = part.Write(backupBytes)
	_ = mw.Close()

	restReq := httptest.NewRequest(http.MethodPost, "/api/backup/restore", &body)
	restReq.Header.Set("Content-Type", mw.FormDataContentType())
	restRec := httptest.NewRecorder()
	hB.ServeHTTP(restRec, restReq)
	if restRec.Code != http.StatusOK {
		t.Fatalf("POST /api/backup/restore failed: %d %s", restRec.Code, restRec.Body.String())
	}

	// Verify all 5 states recovered on Installation B.
	appsRec := httptest.NewRecorder()
	hB.ServeHTTP(appsRec, httptest.NewRequest(http.MethodGet, "/api/applications", nil))
	var appsResp struct {
		Applications []domain.ApplicationDetail `json:"applications"`
	}
	_ = json.Unmarshal(appsRec.Body.Bytes(), &appsResp)
	seenStates := map[domain.ApplicationState]bool{}
	for _, a := range appsResp.Applications {
		seenStates[a.Application.State] = true
		if a.LatestEvaluation == nil || len(a.LatestEvaluation.Findings) == 0 {
			t.Fatalf("expected evaluation evidence recovered for app %d", a.Application.ID)
		}
	}
	for _, tc := range targetStates {
		if !seenStates[tc.state] {
			t.Fatalf("missing recovered state %q on second installation; got %+v", tc.state, seenStates)
		}
	}

	// Verify both EN and ES CV PDFs recovered byte-for-byte on Installation B.
	enFileRec := httptest.NewRecorder()
	hB.ServeHTTP(enFileRec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/cvs/%d/file", cvENResp.CV.ID), nil))
	if !bytes.Equal(enFileRec.Body.Bytes(), pdfEN) {
		t.Fatalf("EN CV bytes mismatch on restored installation")
	}
	esFileRec := httptest.NewRecorder()
	hB.ServeHTTP(esFileRec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/cvs/%d/file", cvESResp.CV.ID), nil))
	if !bytes.Equal(esFileRec.Body.Bytes(), pdfES) {
		t.Fatalf("ES CV bytes mismatch on restored installation")
	}
}
