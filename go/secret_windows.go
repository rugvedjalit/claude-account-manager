//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// Windows: each slot is a DPAPI (CryptProtectData, current user) blob on disk.
// Same file names, entropy and payload as the original PowerShell version, so
// accounts saved by it are readable here.

var dpapiEntropy = []byte("claude-account-manager/v1")

type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32           = syscall.NewLazyDLL("crypt32.dll")
	procProtectData   = crypt32.NewProc("CryptProtectData")
	procUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFree     = kernel32DLL.NewProc("LocalFree")
)

func blobOf(b []byte) dataBlob {
	if len(b) == 0 {
		return dataBlob{}
	}
	return dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func dpapi(proc *syscall.LazyProc, in []byte) ([]byte, error) {
	inBlob, entBlob := blobOf(in), blobOf(dpapiEntropy)
	var out dataBlob
	r, _, err := proc.Call(uintptr(unsafe.Pointer(&inBlob)), 0, uintptr(unsafe.Pointer(&entBlob)), 0, 0, 0, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return append([]byte(nil), unsafe.Slice(out.pbData, out.cbData)...), nil
}

type dpapiStore struct{}

func (dpapiStore) path(slot int) string {
	return filepath.Join(accountsDir(), fmt.Sprintf("account-%d.dpapi", slot))
}

func (d dpapiStore) Put(slot int, data []byte) error {
	enc, err := dpapi(procProtectData, data)
	if err != nil {
		return fmt.Errorf("Windows encryption failed: %w", err)
	}
	if err := os.MkdirAll(accountsDir(), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(d.path(slot), enc, 0o600)
}

func (d dpapiStore) Get(slot int) ([]byte, error) {
	b, err := os.ReadFile(d.path(slot))
	if err != nil {
		return nil, errors.New("its saved file is missing (" + d.path(slot) + ")")
	}
	plain, err := dpapi(procUnprotectData, b)
	if err != nil {
		return nil, errors.New("it can't be unlocked. It was saved by a different Windows user or on another computer")
	}
	return plain, nil
}

func (d dpapiStore) Delete(slot int) error {
	p := d.path(slot)
	if st, err := os.Stat(p); err == nil {
		_ = os.WriteFile(p, make([]byte, st.Size()), 0o600)
	}
	return os.Remove(p)
}

func (dpapiStore) Describe() string {
	return "Windows encryption (DPAPI), only your Windows user can open them (" + accountsDir() + ")"
}

func newSecretStore() secretStore { return dpapiStore{} }
