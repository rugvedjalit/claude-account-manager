//go:build linux

package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
)

// Linux: each slot is stored in the Secret Service (GNOME Keyring / KWallet via
// libsecret) using the `secret-tool` CLI when it is present and a keyring is running.
// Otherwise 0600 files are used, the same protection Claude Code gives its own
// live credentials file on Linux.

type secretToolStore struct{ fallback fileSecretStore }

func stAttrs(slot int) []string {
	return []string{"app", "claude-account-manager", "slot", strconv.Itoa(slot)}
}

func secretToolWorks() bool {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return false
	}
	// A lookup of a non-existent item succeeds (exit 1, no output) when the service is reachable;
	// without a running keyring/DBus it fails with an error message.
	cmd := exec.Command("secret-tool", "lookup", "app", "claude-account-manager", "probe", "1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()
	return stderr.Len() == 0
}

func (s secretToolStore) Put(slot int, data []byte) error {
	args := append([]string{"store", "--label=Claude Account Manager (account " + strconv.Itoa(slot) + ")"}, stAttrs(slot)...)
	cmd := exec.Command("secret-tool", args...)
	cmd.Stdin = bytes.NewReader(data)
	if out, err := cmd.CombinedOutput(); err != nil {
		warn("Secret Service write failed (" + string(bytes.TrimSpace(out)) + "); storing as a 0600 file instead.")
		return s.fallback.Put(slot, data)
	}
	_ = s.fallback.Delete(slot)
	return nil
}

func (s secretToolStore) Get(slot int) ([]byte, error) {
	out, err := exec.Command("secret-tool", append([]string{"lookup"}, stAttrs(slot)...)...).Output()
	if err == nil && len(out) > 0 {
		return out, nil
	}
	return s.fallback.Get(slot)
}

func (s secretToolStore) Delete(slot int) error {
	_ = exec.Command("secret-tool", append([]string{"clear"}, stAttrs(slot)...)...).Run()
	_ = s.fallback.Delete(slot)
	return nil
}

func (secretToolStore) Describe() string { return "Linux Secret Service via secret-tool" }

func newSecretStore() secretStore {
	if secretToolWorks() {
		return secretToolStore{}
	}
	return fileSecretStore{}
}

var _ = fmt.Sprintf
