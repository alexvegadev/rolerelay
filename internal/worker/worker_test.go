package worker

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"rolerelay/internal/credstore"
	"rolerelay/internal/db"
	"rolerelay/internal/domain"
	"rolerelay/internal/evaluator"
	"rolerelay/internal/mailer"
	"rolerelay/internal/store"
)

type fakeMailer struct {
	sent []mailer.Message
	fn   func(ctx context.Context, cfg mailer.SMTPConfig, msg mailer.Message, onBeforeData func() error) (mailer.SendResult, error)
}

func (f *fakeMailer) Send(ctx context.Context, cfg mailer.SMTPConfig, msg mailer.Message, onBeforeData func() error) (mailer.SendResult, error) {
	if f.fn != nil {
		return f.fn(ctx, cfg, msg, onBeforeData)
	}
	if onBeforeData != nil {
		if err := onBeforeData(); err != nil {
			return mailer.SendResult{}, err
		}
	}
	f.sent = append(f.sent, msg)
	return mailer.SendResult{MessageID: "<fake-msg-1@test>"}, nil
}

func setupWorkerTest(t *testing.T) (*store.Store, *credstore.MemoryStore, domain.Application, []byte) {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Open(ctx, filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	repo := store.New(conn)
	creds := credstore.NewMemoryStore()

	if _, err := repo.UpdateProfile(ctx, domain.ProfileUpdate{
		FullName:        "Ada Lovelace",
		Email:           "ada@example.com",
		TargetRoles:     []string{"Backend Engineer"},
		Location:        "Remote",
		WorkArrangement: domain.WorkArrangementRemote,
		LanguageMode:    domain.LanguageModeEN,
		FitThreshold:    70,
		DailySendLimit:  5,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.SaveEmailSettings(ctx, domain.EmailSettings{
		Host:            "127.0.0.1",
		Port:            2525,
		Username:        "ada@example.com",
		FromName:        "Ada Lovelace",
		FromEmail:       "ada@example.com",
		TLSMode:         "none",
		AutoSendEnabled: true,
		SendingPaused:   false,
		DailySendLimit:  5,
	}); err != nil {
		t.Fatal(err)
	}
	if err := creds.Set(credstore.DefaultSMTPTarget, "ada@example.com", "secret-pass"); err != nil {
		t.Fatal(err)
	}

	pdfBytes := []byte("%PDF-1.4\nAda Lovelace Go SQLite REST APIs")
	if _, err := repo.SaveCV(ctx, store.SaveCVParams{
		Language:      domain.CVLanguageEN,
		Filename:      "ada-cv-en.pdf",
		PageCount:     1,
		ExtractedText: "Ada Lovelace Senior Go Backend Engineer SQLite REST APIs",
		Content:       pdfBytes,
	}); err != nil {
		t.Fatal(err)
	}

	vacRes, err := repo.CreateManualVacancy(ctx, domain.ManualVacancyInput{
		CompanyName:      "Acme",
		Title:            "Senior Go Engineer",
		Location:         "Remote",
		WorkArrangement:  domain.WorkArrangementRemote,
		ApplicationEmail: "jobs@acme.test",
		Description:      "We are looking for a backend engineer.\nRequirements:\n- Go and SQLite\n- REST APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	prof, _ := repo.GetOrCreateProfile(ctx)
	cvs, _ := repo.ListCVs(ctx)
	saveParams, err := evaluator.EvaluateVacancy(ctx, prof, cvs, vacRes.Vacancy, nil, evaluator.NewLocalHeuristicEvaluator())
	if err != nil {
		t.Fatal(err)
	}
	_, app, err := repo.SaveEvaluationAndApplication(ctx, saveParams)
	if err != nil {
		t.Fatal(err)
	}
	return repo, creds, app, pdfBytes
}

func TestWorkerSendsOnceWithCorrectAttachment(t *testing.T) {
	ctx := context.Background()
	repo, creds, app, pdfBytes := setupWorkerTest(t)

	fm := &fakeMailer{}
	w := NewWithDeps(Dependencies{Store: repo, CredStore: creds, Mailer: fm, Interval: time.Second})

	didWork, err := w.ProcessOnce(ctx)
	if err != nil || !didWork {
		t.Fatalf("expected ProcessOnce to dispatch queued app, didWork=%v err=%v", didWork, err)
	}
	if len(fm.sent) != 1 {
		t.Fatalf("expected exactly 1 email sent, got %d", len(fm.sent))
	}
	if fm.sent[0].To != "jobs@acme.test" || len(fm.sent[0].Attachments) != 1 {
		t.Fatalf("unexpected email payload: %+v", fm.sent[0])
	}
	if fm.sent[0].Attachments[0].Filename != "ada-cv-en.pdf" || !bytes.Equal(fm.sent[0].Attachments[0].Content, pdfBytes) {
		t.Fatalf("attachment content mismatch")
	}

	// Second call must NOT send a duplicate.
	didWork2, err := w.ProcessOnce(ctx)
	if err != nil || didWork2 {
		t.Fatalf("expected no duplicate send, didWork=%v err=%v", didWork2, err)
	}

	detail, err := repo.GetApplicationDetailed(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Application.State != domain.StateSent {
		t.Fatalf("expected application state=sent, got %s", detail.Application.State)
	}
}

func TestWorkerRestartDuringDispatchLeavesUncertainPending(t *testing.T) {
	ctx := context.Background()
	repo, creds, app, _ := setupWorkerTest(t)

	// Simulate an abrupt process kill RIGHT AFTER onBeforeData() marks attempt 'sending' in SQLite.
	claimed, err := repo.ClaimNextQueuedApplication(ctx, store.DefaultMaxSendAttempts)
	if err != nil || claimed == nil {
		t.Fatalf("expected queued application to be claimed, err=%v", err)
	}
	if err := repo.MarkAttemptSending(ctx, claimed.AttemptID); err != nil {
		t.Fatal(err)
	}

	// Now simulate app restart: startup recovery runs before any new work is claimed.
	summary, err := repo.RecoverInterruptedAttempts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.MarkedUncertain != 1 {
		t.Fatalf("expected 1 attempt marked uncertain on restart, got %+v", summary)
	}

	detail, err := repo.GetApplicationDetailed(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Application.State != domain.StatePending || detail.Application.QueuedForSend || !detail.HasUncertainAttempt {
		t.Fatalf("expected Pending + unqueued + uncertain after restart, got state=%s queued=%v uncertain=%v",
			detail.Application.State, detail.Application.QueuedForSend, detail.HasUncertainAttempt)
	}

	// Restarted worker MUST NOT auto-resend an uncertain application!
	fm := &fakeMailer{}
	w := NewWithDeps(Dependencies{Store: repo, CredStore: creds, Mailer: fm, Interval: time.Second})
	didWork, err := w.ProcessOnce(ctx)
	if err != nil || didWork || len(fm.sent) != 0 {
		t.Fatalf("restarted worker must not auto-resend uncertain attempt! didWork=%v sent=%d err=%v", didWork, len(fm.sent), err)
	}
}

func TestWorkerRetriesPreAcceptanceFailureThenSucceeds(t *testing.T) {
	ctx := context.Background()
	repo, creds, app, _ := setupWorkerTest(t)

	calls := 0
	fm := &fakeMailer{
		fn: func(_ context.Context, _ mailer.SMTPConfig, msg mailer.Message, onBeforeData func() error) (mailer.SendResult, error) {
			calls++
			if calls == 1 {
				// Fails BEFORE onBeforeData (pre-acceptance transient error).
				return mailer.SendResult{}, &mailer.DeliveryError{
					Kind: mailer.ErrorKindPreAcceptanceRetryable,
					Err:  errors.New("temporary 421 busy"),
				}
			}
			_ = onBeforeData()
			return mailer.SendResult{MessageID: "<retry-ok@test>"}, nil
		},
	}

	w := NewWithDeps(Dependencies{Store: repo, CredStore: creds, Mailer: fm, Interval: time.Second})
	if _, err := w.ProcessOnce(ctx); err != nil {
		t.Fatal(err)
	}
	detail1, _ := repo.GetApplicationDetailed(ctx, app.ID)
	if detail1.Application.State != domain.StatePending || !detail1.Application.QueuedForSend {
		t.Fatalf("expected app to remain pending & queued for retry, got %+v", detail1.Application)
	}

	if _, err := w.ProcessOnce(ctx); err != nil {
		t.Fatal(err)
	}
	detail2, _ := repo.GetApplicationDetailed(ctx, app.ID)
	if detail2.Application.State != domain.StateSent || len(detail2.SubmissionAttempts) != 2 {
		t.Fatalf("expected sent on 2nd attempt, got state=%s attempts=%d", detail2.Application.State, len(detail2.SubmissionAttempts))
	}
}
