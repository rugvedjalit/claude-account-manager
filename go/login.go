package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type capturedLogin struct {
	Credentials string
	OAuthJSON   string
}

// isolatedLogin runs `claude auth login` against a throw-away CLAUDE_CONFIG_DIR, so the
// current live login is never logged out, then captures and scrubs the result.
func isolatedLogin(email string) (*capturedLogin, error) {
	if err := os.MkdirAll(tmpRoot(), 0o700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(tmpRoot(), "login-")
	if err != nil {
		return nil, err
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	defer func() {
		cred := filepath.Join(dir, ".credentials.json")
		if st, err := os.Stat(cred); err == nil {
			_ = os.WriteFile(cred, make([]byte, st.Size()), 0o600)
		}
		cleanupIsolatedCredentials(dir)
		_ = os.RemoveAll(dir)
	}()

	args := []string{"auth", "login"}
	if email != "" {
		args = append(args, "--email", email)
	}
	r := runClaude(args, map[string]string{"CLAUDE_CONFIG_DIR": dir}, true)
	if r.exit != 0 {
		return nil, fmt.Errorf("sign-in was not finished (Claude exited with code %d)", r.exit)
	}
	credText, err := isolatedCredentials(dir)
	if err != nil {
		return nil, err
	}
	if parseCredentials(credText) == nil {
		return nil, errors.New("Claude saved the login in a format this tool doesn't recognize")
	}
	oauthJSON := ""
	if b, err := os.ReadFile(filepath.Join(dir, ".claude.json")); err == nil {
		if s, e, found, err := findTopLevelMember(string(b), "oauthAccount"); err == nil && found {
			oauthJSON = string(b)[s:e]
		}
	}
	if parseProfile(oauthJSON) == nil {
		// Fallback: build a minimal profile from `claude auth status`.
		if st := getAuthStatus(dir); st != nil && st.Email != "" {
			b, _ := json.Marshal(map[string]string{"emailAddress": st.Email, "organizationUuid": st.OrgID, "organizationName": st.OrgName})
			oauthJSON = string(b)
		}
	}
	if parseProfile(oauthJSON) == nil {
		return nil, errors.New("sign-in finished, but the account email couldn't be read")
	}
	return &capturedLogin{Credentials: credText, OAuthJSON: oauthJSON}, nil
}
