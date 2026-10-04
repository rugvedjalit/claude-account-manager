//go:build !darwin

package main

import "os"

// On Windows and Linux, Claude Code keeps the login in <configDir>/.credentials.json.

func readLiveCredentials() (text, source string, err error) {
	b, err := os.ReadFile(liveCredPath)
	if err != nil {
		return "", "", err
	}
	return string(b), "file", nil
}

func writeLiveCredentials(text string) error {
	return writeFileAtomic(liveCredPath, []byte(text), 0o600)
}

// isolatedCredentials reads the login that `claude auth login` wrote into a throw-away config dir.
func isolatedCredentials(dir string) (string, error) {
	b, err := os.ReadFile(dir + string(os.PathSeparator) + ".credentials.json")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func cleanupIsolatedCredentials(dir string) {}

func liveCredentialDescription() string { return liveCredPath }
