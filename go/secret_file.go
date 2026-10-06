package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// fileSecretStore is the fallback when no OS keyring is available (headless Linux,
// SSH sessions). Files are mode 0600, the same protection Claude Code gives its own
// live credentials file on Linux.
type fileSecretStore struct{}

func (fileSecretStore) path(slot int) string {
	return filepath.Join(accountsDir(), fmt.Sprintf("account-%d.json", slot))
}

func (f fileSecretStore) Put(slot int, data []byte) error {
	if err := os.MkdirAll(accountsDir(), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(f.path(slot), data, 0o600)
}

func (f fileSecretStore) Get(slot int) ([]byte, error) {
	b, err := os.ReadFile(f.path(slot))
	if err != nil {
		return nil, fmt.Errorf("its saved file is missing (%s)", f.path(slot))
	}
	return b, nil
}

func (f fileSecretStore) Delete(slot int) error {
	p := f.path(slot)
	if st, err := os.Stat(p); err == nil {
		_ = os.WriteFile(p, make([]byte, st.Size()), 0o600) // zero-fill before unlink
	}
	return os.Remove(p)
}

func (fileSecretStore) Describe() string {
	return "a private file only you can read (no keyring found on this computer)"
}
