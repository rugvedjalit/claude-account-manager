//go:build darwin

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// On macOS, Claude Code keeps the login in the Keychain as a generic password:
//   service: "Claude Code-credentials"            (default config dir)
//            "Claude Code-credentials-<sha256(CLAUDE_CONFIG_DIR)[:8]>"  (custom config dir)
//   account: "claude-code-user"
//   password: the same JSON that .credentials.json holds elsewhere
// When the Keychain is unavailable it falls back to <configDir>/.credentials.json (0600).
// (Scheme taken from the Claude Code binary; see README "Limitations".)

const keychainAccount = "claude-code-user"

func keychainService(dir string) string {
	if os.Getenv("CLAUDE_CONFIG_DIR") == "" && dir == configDir {
		return "Claude Code-credentials"
	}
	sum := sha256.Sum256([]byte(dir))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8]
}

func keychainRead(service string) (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-a", keychainAccount, "-w", "-s", service).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func keychainWrite(service, text string) error {
	hexData := hex.EncodeToString([]byte(text))
	cmd := exec.Command("security", "add-generic-password", "-U", "-a", keychainAccount, "-s", service, "-X", hexData)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("security add-generic-password failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func keychainDelete(service string) {
	_ = exec.Command("security", "delete-generic-password", "-a", keychainAccount, "-s", service).Run()
}

func readLiveCredentials() (text, source string, err error) {
	if t, e := keychainRead(keychainService(configDir)); e == nil && t != "" {
		return t, "keychain", nil
	}
	b, e := os.ReadFile(liveCredPath)
	if e != nil {
		return "", "", errors.New("no login found in the Keychain or the login file")
	}
	return string(b), "file", nil
}

func writeLiveCredentials(text string) error {
	// Prefer the Keychain (what Claude Code reads first); fall back to the file.
	if err := keychainWrite(keychainService(configDir), text); err == nil {
		// A stale fallback file would shadow nothing, but keep it consistent anyway.
		if _, statErr := os.Stat(liveCredPath); statErr == nil {
			_ = writeFileAtomic(liveCredPath, []byte(text), 0o600)
		}
		return nil
	}
	return writeFileAtomic(liveCredPath, []byte(text), 0o600)
}

func isolatedCredentials(dir string) (string, error) {
	if t, err := keychainRead(keychainService(dir)); err == nil && t != "" {
		return t, nil
	}
	// Claude Code normalizes the path (NFC) before hashing; try a cleaned variant too.
	if t, err := keychainRead(keychainService(filepath.Clean(dir))); err == nil && t != "" {
		return t, nil
	}
	b, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	if err != nil {
		return "", errors.New("sign-in finished, but no login was found in the Keychain or the login file")
	}
	return string(b), nil
}

func cleanupIsolatedCredentials(dir string) {
	keychainDelete(keychainService(dir))
	keychainDelete(keychainService(filepath.Clean(dir)))
}

func liveCredentialDescription() string {
	return fmt.Sprintf("Keychain item %q (fallback %s)", keychainService(configDir), liveCredPath)
}
