package credstore

import (
	"errors"
	"sync"
)

const DefaultSMTPTarget = "RoleRelay:smtp_sending_account"

var ErrNotFound = errors.New("credential not found in OS credential store")

type Credential struct {
	Target   string
	Username string
	Secret   string
}

type Store interface {
	Set(target, username, secret string) error
	Get(target string) (Credential, error)
	Delete(target string) error
	BackendName() string
}

type MemoryStore struct {
	mu    sync.RWMutex
	items map[string]Credential
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		items: make(map[string]Credential),
	}
}

func (m *MemoryStore) Set(target, username, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[target] = Credential{
		Target:   target,
		Username: username,
		Secret:   secret,
	}
	return nil
}

func (m *MemoryStore) Get(target string) (Credential, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.items[target]
	if !ok {
		return Credential{}, ErrNotFound
	}
	return c, nil
}

func (m *MemoryStore) Delete(target string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, target)
	return nil
}

func (m *MemoryStore) BackendName() string {
	return "memory"
}
