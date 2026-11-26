//go:build !windows

package credstore

func NewDefaultStore() Store {
	return NewMemoryStore()
}
