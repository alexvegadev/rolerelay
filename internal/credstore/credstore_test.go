package credstore

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestMemoryStoreRoundTrip(t *testing.T) {
	st := NewMemoryStore()
	target := "RoleRelay:memory_test"

	if _, err := st.Get(target); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound before Set, got %v", err)
	}
	if err := st.Set(target, "user@example.com", "secret-123"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	got, err := st.Get(target)
	if err != nil || got.Username != "user@example.com" || got.Secret != "secret-123" {
		t.Fatalf("unexpected Get result: %+v err=%v", got, err)
	}
	if err := st.Delete(target); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := st.Get(target); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Delete, got %v", err)
	}
}

func TestDefaultStoreRoundTrip(t *testing.T) {
	st := NewDefaultStore()
	target := fmt.Sprintf("RoleRelay:unit_test_%d", time.Now().UnixNano())
	defer func() {
		_ = st.Delete(target)
	}()

	if _, err := st.Get(target); err != nil && !errors.Is(err, ErrNotFound) {
		if strings.Contains(strings.ToLower(err.Error()), "access is denied") {
			t.Skipf("OS credential store blocked by sandbox environment: %v", err)
		}
		t.Fatalf("expected ErrNotFound before Set, got %v", err)
	}

	if err := st.Set(target, "test@example.com", "s3cr3t-pass!"); err != nil {
		t.Fatalf("Set failed (%s): %v", st.BackendName(), err)
	}

	got, err := st.Get(target)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Username != "test@example.com" || got.Secret != "s3cr3t-pass!" {
		t.Fatalf("unexpected credential returned: %+v", got)
	}

	if err := st.Delete(target); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := st.Get(target); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Delete, got %v", err)
	}
}
