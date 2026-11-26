package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"rolerelay/internal/credstore"
	"rolerelay/internal/domain"
	"rolerelay/internal/mailer"
	"rolerelay/internal/store"
)

func (s *Server) handleListApplications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	apps, err := s.opts.Store.ListApplicationsDetailed(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "applications_list_failed", err.Error())
		return
	}
	emailCfg, err := s.loadEmailSettingsWithCredStatus(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "email_settings_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"applications":  apps,
		"emailSettings": emailCfg,
	})
}

func (s *Server) handleGetApplication(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid application ID.")
		return
	}
	detail, err := s.opts.Store.GetApplicationDetailed(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Application not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "application_load_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"application": detail})
}

func (s *Server) handleQueueApplication(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid application ID.")
		return
	}
	var payload struct {
		ConfirmNotSent bool `json:"confirmNotSent"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&payload)
	}

	detail, err := s.opts.Store.QueueApplication(r.Context(), id, payload.ConfirmNotSent)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Application not found.")
		case errors.Is(err, store.ErrAlreadySent):
			writeError(w, http.StatusConflict, "already_sent", err.Error())
		case errors.Is(err, store.ErrUncertainPending):
			writeError(w, http.StatusConflict, "uncertain_attempt", err.Error())
		case errors.Is(err, store.ErrNotEligibleToQueue):
			writeError(w, http.StatusUnprocessableEntity, "not_eligible", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "queue_failed", err.Error())
		}
		return
	}

	if s.opts.Worker != nil {
		s.opts.Worker.Notify()
	}
	writeJSON(w, http.StatusOK, map[string]any{"application": detail})
}

func (s *Server) handleUpdateApplicationState(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid application ID.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var payload struct {
		State  domain.ApplicationState `json:"state"`
		Reason string                  `json:"reason"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", fmt.Sprintf("Invalid state payload: %v", err))
		return
	}

	detail, err := s.opts.Store.RecordApplicationState(r.Context(), id, payload.State, payload.Reason, "user")
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Application not found.")
			return
		}
		var valErr *store.ValidationError
		if errors.As(err, &valErr) {
			writeError(w, http.StatusUnprocessableEntity, "validation_error", valErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "state_update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"application": detail})
}

func (s *Server) handleGetEmailSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.loadEmailSettingsWithCredStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "email_settings_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": cfg})
}

func (s *Server) handleUpdateEmailSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var payload domain.EmailSettings
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", fmt.Sprintf("Invalid email settings payload: %v", err))
		return
	}

	ctx := r.Context()
	cs := s.resolveCredStore()

	// Store password securely in the OS credential store (never in SQLite).
	if strings.TrimSpace(payload.Password) != "" {
		if err := cs.Set(credstore.DefaultSMTPTarget, strings.TrimSpace(payload.Username), payload.Password); err != nil {
			writeError(w, http.StatusInternalServerError, "credstore_write_failed", fmt.Sprintf("Failed to save sending credentials in OS credential store: %v", err))
			return
		}
	}

	if _, err := s.opts.Store.SaveEmailSettings(ctx, payload); err != nil {
		var valErr *store.ValidationError
		if errors.As(err, &valErr) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"code":  "validation_error",
				"field": valErr.Field,
				"error": valErr.Error(),
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "email_settings_save_failed", err.Error())
		return
	}

	if s.opts.Worker != nil {
		s.opts.Worker.SyncPausedFromDB(payload.SendingPaused)
		if payload.AutoSendEnabled && !payload.SendingPaused {
			s.opts.Worker.Notify()
		}
	}

	cfg, err := s.loadEmailSettingsWithCredStatus(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "email_settings_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": cfg})
}

func (s *Server) handleDeleteEmailCredentials(w http.ResponseWriter, r *http.Request) {
	cs := s.resolveCredStore()
	if err := cs.Delete(credstore.DefaultSMTPTarget); err != nil {
		writeError(w, http.StatusInternalServerError, "credstore_delete_failed", err.Error())
		return
	}
	// Disable auto-send when credentials are disconnected.
	_ = s.opts.Store.SetSendingPreferences(r.Context(), false, true)
	if s.opts.Worker != nil {
		s.opts.Worker.SyncPausedFromDB(true)
	}
	cfg, err := s.loadEmailSettingsWithCredStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "email_settings_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": cfg})
}

func (s *Server) handleTestEmailConnection(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var payload struct {
		Recipient string `json:"recipient"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	}

	ctx := r.Context()
	cfg, err := s.loadEmailSettingsWithCredStatus(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "email_settings_failed", err.Error())
		return
	}
	if strings.TrimSpace(cfg.Host) == "" {
		writeError(w, http.StatusUnprocessableEntity, "smtp_unconfigured", "Configure SMTP host and port before running a connection test.")
		return
	}

	var password string
	if strings.TrimSpace(cfg.Username) != "" {
		cred, err := s.resolveCredStore().Get(credstore.DefaultSMTPTarget)
		if errors.Is(err, credstore.ErrNotFound) {
			writeError(w, http.StatusUnprocessableEntity, "reconnect_required", "Sending credentials are missing in the OS credential store. Re-enter your password and save.")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "credstore_read_failed", err.Error())
			return
		}
		password = cred.Secret
	}

	recipient := strings.TrimSpace(payload.Recipient)
	if recipient == "" {
		recipient = strings.TrimSpace(cfg.FromEmail)
	}
	if recipient == "" {
		recipient = strings.TrimSpace(cfg.Username)
	}
	if recipient == "" {
		writeError(w, http.StatusUnprocessableEntity, "missing_recipient", "Provide a test recipient email address.")
		return
	}

	m := s.opts.Mailer
	if m == nil {
		m = mailer.NewSMTPMailer()
	}
	res, err := m.Send(ctx, mailer.SMTPConfig{
		Host:      cfg.Host,
		Port:      cfg.Port,
		Username:  cfg.Username,
		Password:  password,
		FromName:  cfg.FromName,
		FromEmail: cfg.FromEmail,
		TLSMode:   cfg.TLSMode,
	}, mailer.Message{
		To:      recipient,
		Subject: "RoleRelay SMTP connection test",
		Body:    "This test message confirms your RoleRelay sending account is connected and ready.",
	}, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "smtp_test_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"recipient": recipient,
		"messageId": res.MessageID,
	})
}

func (s *Server) loadEmailSettingsWithCredStatus(ctx context.Context) (domain.EmailSettings, error) {
	cfg, err := s.opts.Store.GetEmailSettings(ctx)
	if err != nil {
		return domain.EmailSettings{}, err
	}
	cs := s.resolveCredStore()
	cfg.CredentialBackend = cs.BackendName()
	cfg.Password = ""

	if strings.TrimSpace(cfg.Username) != "" {
		if _, err := cs.Get(credstore.DefaultSMTPTarget); err == nil {
			cfg.HasCredentials = true
		} else if errors.Is(err, credstore.ErrNotFound) && strings.TrimSpace(cfg.Host) != "" {
			// Host/username exist in SQLite (e.g. after moving portable DB to another machine)
			// while the OS credential store on this machine doesn't hold the secret.
			cfg.NeedsReconnect = true
		}
	} else if strings.TrimSpace(cfg.Host) != "" {
		cfg.HasCredentials = true
	}
	return cfg, nil
}

func (s *Server) resolveCredStore() credstore.Store {
	if s.opts.CredStore != nil {
		return s.opts.CredStore
	}
	return credstore.NewDefaultStore()
}
