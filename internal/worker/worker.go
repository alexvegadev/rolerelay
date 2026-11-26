package worker

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"rolerelay/internal/credstore"
	"rolerelay/internal/mailer"
	"rolerelay/internal/store"
)

type Status struct {
	State           string    `json:"state"`
	Paused          bool      `json:"paused"`
	LastError       string    `json:"lastError,omitempty"`
	NeedsReconnect  bool      `json:"needsReconnect"`
	StartedAt       time.Time `json:"startedAt"`
	LastHeartbeatAt time.Time `json:"lastHeartbeatAt"`
}

type Dependencies struct {
	Store     *store.Store
	CredStore credstore.Store
	Mailer    mailer.Mailer
	Interval  time.Duration
}

type Worker struct {
	mu              sync.RWMutex
	dispatchMu      sync.Mutex
	state           string
	paused          bool
	lastError       string
	needsReconnect  bool
	startedAt       time.Time
	lastHeartbeatAt time.Time
	interval        time.Duration
	store           *store.Store
	credStore       credstore.Store
	mailer          mailer.Mailer
	wakeCh          chan struct{}
	done            chan struct{}
}

func New(interval time.Duration) *Worker {
	return NewWithDeps(Dependencies{Interval: interval})
}

func NewWithDeps(deps Dependencies) *Worker {
	interval := deps.Interval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	m := deps.Mailer
	if m == nil {
		m = mailer.NewSMTPMailer()
	}
	return &Worker{
		state:     "idle",
		interval:  interval,
		store:     deps.Store,
		credStore: deps.CredStore,
		mailer:    m,
		wakeCh:    make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
}

func (w *Worker) Notify() {
	select {
	case w.wakeCh <- struct{}{}:
	default:
	}
}

func (w *Worker) Start(ctx context.Context) {
	now := time.Now().UTC()

	// Recover any interrupted attempts from a prior crash/restart before claiming new work.
	if w.store != nil {
		if summary, err := w.store.RecoverInterruptedAttempts(ctx); err != nil {
			log.Printf("worker recovery error: %v", err)
		} else if summary.MarkedUncertain > 0 || summary.MarkedRetryable > 0 {
			log.Printf("worker startup recovery: uncertain=%d retryable=%d", summary.MarkedUncertain, summary.MarkedRetryable)
		}
		if prof, err := w.store.GetOrCreateProfile(ctx); err == nil {
			w.mu.Lock()
			w.paused = prof.SendingPaused
			w.mu.Unlock()
		}
	}

	w.mu.Lock()
	w.startedAt = now
	w.lastHeartbeatAt = now
	if w.paused {
		w.state = "paused"
	} else {
		w.state = "running"
	}
	w.mu.Unlock()

	go func() {
		defer close(w.done)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		// Run an initial cycle immediately on startup so queued work resumes right away.
		w.runCycle(ctx)

		for {
			select {
			case <-ctx.Done():
				w.mu.Lock()
				w.state = "stopped"
				w.mu.Unlock()
				return
			case <-w.wakeCh:
				w.runCycle(ctx)
			case t := <-ticker.C:
				w.mu.Lock()
				w.lastHeartbeatAt = t.UTC()
				if w.paused {
					w.state = "paused"
				} else {
					w.state = "running"
				}
				w.mu.Unlock()
				w.runCycle(ctx)
			}
		}
	}()
}

func (w *Worker) runCycle(ctx context.Context) {
	if w.store == nil || ctx.Err() != nil {
		return
	}
	if _, err := w.ProcessOnce(ctx); err != nil {
		log.Printf("worker dispatch cycle error: %v", err)
	}
}

// ProcessOnce attempts to claim and dispatch at most one queued application.
// Returns true if an application was claimed and processed.
func (w *Worker) ProcessOnce(ctx context.Context) (bool, error) {
	if w.store == nil {
		return false, nil
	}
	w.dispatchMu.Lock()
	defer w.dispatchMu.Unlock()

	w.mu.RLock()
	paused := w.paused
	w.mu.RUnlock()
	if paused {
		return false, nil
	}

	emailCfg, err := w.store.GetEmailSettings(ctx)
	if err != nil {
		return false, err
	}
	if !emailCfg.AutoSendEnabled || emailCfg.SendingPaused {
		return false, nil
	}
	if strings.TrimSpace(emailCfg.Host) == "" {
		return false, nil
	}

	// Verify OS credentials exist before claiming queue work.
	var password string
	if strings.TrimSpace(emailCfg.Username) != "" {
		if w.credStore == nil {
			w.setReconnectStatus(true, "OS credential store unavailable")
			return false, nil
		}
		cred, err := w.credStore.Get(credstore.DefaultSMTPTarget)
		if errors.Is(err, credstore.ErrNotFound) {
			w.setReconnectStatus(true, "Sending account credentials missing in OS credential store; reconnect your sending account.")
			return false, nil
		}
		if err != nil {
			w.setReconnectStatus(true, err.Error())
			return false, err
		}
		password = cred.Secret
	}
	w.setReconnectStatus(false, "")

	claimed, err := w.store.ClaimNextQueuedApplication(ctx, store.DefaultMaxSendAttempts)
	if err != nil || claimed == nil {
		return false, err
	}

	smtpCfg := mailer.SMTPConfig{
		Host:      emailCfg.Host,
		Port:      emailCfg.Port,
		Username:  emailCfg.Username,
		Password:  password,
		FromName:  emailCfg.FromName,
		FromEmail: emailCfg.FromEmail,
		TLSMode:   emailCfg.TLSMode,
	}
	msg := mailer.Message{
		To:          claimed.Application.Recipient,
		Subject:     claimed.Application.Subject,
		Body:        claimed.Application.Body,
		Attachments: claimed.Attachments,
	}

	sendRes, sendErr := w.mailer.Send(ctx, smtpCfg, msg, func() error {
		return w.store.MarkAttemptSending(ctx, claimed.AttemptID)
	})
	if sendErr == nil {
		if err := w.store.CompleteAttemptSent(ctx, claimed.AttemptID, claimed.Application.ID, sendRes.MessageID); err != nil {
			return true, err
		}
		return true, nil
	}

	var delivErr *mailer.DeliveryError
	if errors.As(sendErr, &delivErr) {
		switch delivErr.Kind {
		case mailer.ErrorKindUncertain:
			_ = w.store.CompleteAttemptUncertain(ctx, claimed.AttemptID, claimed.Application.ID, delivErr.Error())
			return true, nil
		case mailer.ErrorKindPreAcceptancePermanent:
			_ = w.store.CompleteAttemptFailed(ctx, claimed.AttemptID, claimed.Application.ID, delivErr.Error())
			return true, nil
		default:
			_ = w.store.CompleteAttemptRetryable(ctx, claimed.AttemptID, claimed.Application.ID, delivErr.Error(), claimed.AttemptNumber, store.DefaultMaxSendAttempts)
			return true, nil
		}
	}

	// Unknown error defaults to uncertain if already in sending, otherwise retryable.
	_ = w.store.CompleteAttemptRetryable(ctx, claimed.AttemptID, claimed.Application.ID, sendErr.Error(), claimed.AttemptNumber, store.DefaultMaxSendAttempts)
	return true, nil
}

func (w *Worker) Pause() Status {
	w.mu.Lock()
	w.paused = true
	if w.state != "stopped" {
		w.state = "paused"
	}
	st := w.statusLocked()
	w.mu.Unlock()
	if w.store != nil {
		_ = w.store.SetSendingPaused(context.Background(), true)
	}
	return st
}

func (w *Worker) Resume() Status {
	w.mu.Lock()
	w.paused = false
	if w.state != "stopped" {
		w.state = "running"
	}
	st := w.statusLocked()
	w.mu.Unlock()
	if w.store != nil {
		_ = w.store.SetSendingPaused(context.Background(), false)
	}
	w.Notify()
	return st
}

func (w *Worker) SyncPausedFromDB(paused bool) {
	w.mu.Lock()
	w.paused = paused
	if w.state != "stopped" {
		if paused {
			w.state = "paused"
		} else {
			w.state = "running"
		}
	}
	w.mu.Unlock()
	if !paused {
		w.Notify()
	}
}

func (w *Worker) Status() Status {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.statusLocked()
}

func (w *Worker) Wait() {
	<-w.done
}

// WithExclusive pauses worker dispatches while fn runs (used during live SQLite backup restore).
func (w *Worker) WithExclusive(fn func() error) error {
	w.dispatchMu.Lock()
	defer w.dispatchMu.Unlock()
	return fn()
}

func (w *Worker) setReconnectStatus(needsReconnect bool, errMsg string) {
	w.mu.Lock()
	w.needsReconnect = needsReconnect
	w.lastError = errMsg
	w.mu.Unlock()
}

func (w *Worker) statusLocked() Status {
	return Status{
		State:           w.state,
		Paused:          w.paused,
		LastError:       w.lastError,
		NeedsReconnect:  w.needsReconnect,
		StartedAt:       w.startedAt,
		LastHeartbeatAt: w.lastHeartbeatAt,
	}
}
