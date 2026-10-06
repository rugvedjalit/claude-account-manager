//go:build darwin

package main

import (
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
)

// macOS: each slot is a generic password in the login Keychain
// (service "claude-account-manager", account "slot-N"). Falls back to 0600 files
// when the Keychain is unavailable (for example a locked Keychain over SSH).

const camKeychainService = "claude-account-manager"

type keychainStore struct{ fallback fileSecretStore }

func slotAccount(slot int) string { return fmt.Sprintf("slot-%d", slot) }

func (k keychainStore) Put(slot int, data []byte) error {
	cmd := exec.Command("security", "add-generic-password", "-U", "-a", slotAccount(slot), "-s", camKeychainService,
		"-l", fmt.Sprintf("Claude Account Manager (account %d)", slot), "-X", hex.EncodeToString(data))
	if out, err := cmd.CombinedOutput(); err != nil {
		warn("Couldn't save to the Keychain (" + strings.TrimSpace(string(out)) + "), so it was saved in a private file instead.")
		return k.fallback.Put(slot, data)
	}
	_ = k.fallback.Delete(slot) // no stale plaintext copy
	return nil
}

func (k keychainStore) Get(slot int) ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password", "-a", slotAccount(slot), "-s", camKeychainService, "-w").Output()
	if err == nil && len(out) > 0 {
		return []byte(strings.TrimRight(string(out), "\n")), nil
	}
	return k.fallback.Get(slot)
}

func (k keychainStore) Delete(slot int) error {
	_ = exec.Command("security", "delete-generic-password", "-a", slotAccount(slot), "-s", camKeychainService).Run()
	_ = k.fallback.Delete(slot)
	return nil
}

func (keychainStore) Describe() string {
	return "the macOS Keychain (look for \"" + camKeychainService + "\")"
}

func newSecretStore() secretStore {
	if _, err := exec.LookPath("security"); err != nil {
		return fileSecretStore{}
	}
	return keychainStore{}
}
