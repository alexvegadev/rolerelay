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
	"sync/atomic"
	"testing"
	"time"

	"rolerelay/internal/config"
	"rolerelay/internal/db"
	"rolerelay/internal/domain"
	"rolerelay/internal/store"
	"rolerelay/internal/worker"
)

func makeMinimalPDF(text string) []byte {
	var stream string
	if text != "" {
		escaped := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(text)
		stream = fmt.Sprintf("BT\n/F1 12 Tf\n72 720 Td\n(%s) Tj\nET\n", escaped)
	}
	objs := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n",
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>\nendobj\n",
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n",
		fmt.Sprintf("5 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n", len(stream), stream),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs)+1)
	for i, obj := range objs {
		offsets[i+1] = buf.Len()
		buf.WriteString(obj)
	}
	xref := buf.Len()
	buf.WriteString(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(objs)+1))
	for i := 1; i <= len(objs); i++ {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[i]))
	}
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref))
	return buf.Bytes()
}

func TestMilestone1APIUploadRestartReopenAndExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbPath := filepath.Join(t.TempDir(), "data", "app.sqlite")
	paths := config.StoragePaths{
		Mode:    config.StorageModePortable,
		DataDir: filepath.Dir(dbPath),
		DBPath:  dbPath,
	}
	if err := config.EnsureDataDir(paths); err != nil {
		t.Fatal(err)
	}

	// Boot 1
	conn1, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	conn1Closed := false
	defer func() {
		if !conn1Closed {
			_ = conn1.Close()
		}
	}()

	w1 := worker.New(50 * time.Millisecond)
	w1.Start(ctx)
	var exited atomic.Bool
	srv1 := New(Options{
		Version:       "0.1.0-test",
		StoragePaths:  paths,
		SchemaVersion: 1,
		Address:       "127.0.0.1:41730",
		Store:         store.New(conn1),
		Worker:        w1,
		OnExit:        func() { exited.Store(true) },
	})
	h1 := srv1.Handler()

	// 1. Update profile to English mode.
	updateBody := `{"fullName":"Jane Doe","email":"jane@example.com","targetRoles":["Backend Engineer"],"location":"Remote","workArrangement":"remote","languageMode":"en","fitThreshold":75,"dailySendLimit":10}`
	req := httptest.NewRequest(http.MethodPut, "/api/profile", strings.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h1.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on PUT /api/profile, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Upload an unextractable (scanned/empty) PDF and verify clear 422 error.
	emptyPDF := makeMinimalPDF("")
	recEmpty := uploadCVRecorder(t, h1, "en", "scanned.pdf", emptyPDF)
	if recEmpty.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unextractable PDF, got %d: %s", recEmpty.Code, recEmpty.Body.String())
	}
	var errPayload map[string]any
	if err := json.Unmarshal(recEmpty.Body.Bytes(), &errPayload); err != nil {
		t.Fatal(err)
	}
	if errPayload["code"] != "no_extractable_text" {
		t.Fatalf("expected code=no_extractable_text, got %+v", errPayload)
	}

	// 3. Upload a valid text PDF CV.
	validPDF := makeMinimalPDF("Jane Doe Senior Go Engineer 7 years experience")
	recOK := uploadCVRecorder(t, h1, "en", "jane-cv-en.pdf", validPDF)
	if recOK.Code != http.StatusCreated {
		t.Fatalf("expected 201 on valid CV upload, got %d: %s", recOK.Code, recOK.Body.String())
	}
	var uploadResult struct {
		CV        domain.CVDocument       `json:"cv"`
		Readiness domain.ProfileReadiness `json:"readiness"`
	}
	if err := json.Unmarshal(recOK.Body.Bytes(), &uploadResult); err != nil {
		t.Fatal(err)
	}
	if !uploadResult.Readiness.Ready {
		t.Fatalf("expected readiness=true after EN CV upload, issues=%v", uploadResult.Readiness.Issues)
	}

	// 4. Reject cross-origin mutating requests to loopback.
	badOriginReq := httptest.NewRequest(http.MethodPost, "/api/app/exit", nil)
	badOriginReq.Header.Set("Origin", "https://evil.example")
	badOriginRec := httptest.NewRecorder()
	h1.ServeHTTP(badOriginRec, badOriginReq)
	if badOriginRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin POST, got %d", badOriginRec.Code)
	}

	// 5. Trigger Exit control and verify callback fires.
	exitReq := httptest.NewRequest(http.MethodPost, "/api/app/exit", nil)
	exitRec := httptest.NewRecorder()
	h1.ServeHTTP(exitRec, exitReq)
	if exitRec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 on /api/app/exit, got %d", exitRec.Code)
	}
	time.Sleep(250 * time.Millisecond)
	if !exited.Load() {
		t.Fatalf("expected OnExit callback to be triggered")
	}

	if err := conn1.Close(); err != nil {
		t.Fatal(err)
	}
	conn1Closed = true

	// Boot 2: restart server against the same SQLite file and reopen saved CV.
	conn2, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()

	srv2 := New(Options{
		Version:       "0.1.0-test",
		StoragePaths:  paths,
		SchemaVersion: 1,
		Address:       "127.0.0.1:41730",
		Store:         store.New(conn2),
		Worker:        w1,
	})
	h2 := srv2.Handler()

	fileReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/cvs/%d/file", uploadResult.CV.ID), nil)
	fileRec := httptest.NewRecorder()
	h2.ServeHTTP(fileRec, fileReq)
	if fileRec.Code != http.StatusOK {
		t.Fatalf("expected 200 reopening saved CV file, got %d", fileRec.Code)
	}
	if ct := fileRec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("expected application/pdf, got %q", ct)
	}
	if !bytes.Equal(fileRec.Body.Bytes(), validPDF) {
		t.Fatalf("reopened PDF bytes did not match original uploaded PDF")
	}
}

func uploadCVRecorder(t *testing.T, handler http.Handler, lang, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("language", lang); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/cvs", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
