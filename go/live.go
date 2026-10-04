package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Credential parsing
// ---------------------------------------------------------------------------

type credInfo struct {
	SubscriptionType string
	AccessExpires    time.Time
	RefreshExpires   time.Time
}

func epochMs(v interface{}) time.Time {
	if f, ok := v.(float64); ok && f > 0 {
		return time.UnixMilli(int64(f))
	}
	return time.Time{}
}

// parseCredentials validates the Claude Code credential layout and extracts non-secret metadata.
func parseCredentials(text string) *credInfo {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var o map[string]interface{}
	if err := json.Unmarshal([]byte(text), &o); err != nil {
		return nil
	}
	v, ok := o["claudeAiOauth"].(map[string]interface{})
	if !ok {
		return nil
	}
	if _, ok := v["accessToken"].(string); !ok {
		return nil
	}
	if _, ok := v["refreshToken"].(string); !ok {
		return nil
	}
	ci := &credInfo{}
	if s, ok := v["subscriptionType"].(string); ok {
		ci.SubscriptionType = s
	}
	ci.AccessExpires = epochMs(v["expiresAt"])
	ci.RefreshExpires = epochMs(v["refreshTokenExpiresAt"])
	return ci
}

type profile struct {
	AccountUUID string `json:"accountUuid"`
	Email       string `json:"emailAddress"`
	OrgName     string `json:"organizationName"`
	OrgUUID     string `json:"organizationUuid"`
}

func parseProfile(jsonText string) *profile {
	t := strings.TrimSpace(jsonText)
	if t == "" || t == "null" {
		return nil
	}
	var p profile
	if err := json.Unmarshal([]byte(t), &p); err != nil {
		return nil
	}
	if p.AccountUUID == "" && p.Email == "" {
		return nil
	}
	return &p
}

func identityOf(p *profile) string {
	if p == nil {
		return ""
	}
	if p.AccountUUID != "" {
		return p.AccountUUID
	}
	return "email:" + strings.ToLower(p.Email)
}

// ---------------------------------------------------------------------------
// Live state
// ---------------------------------------------------------------------------

type liveState struct {
	HasCredentials  bool
	CredentialsText string
	CredSource      string // "file" or "keychain"
	CredInfo        *credInfo
	OAuthJSON       string
	Profile         *profile
	Identity        string
	Email           string
}

func getLiveState() *liveState {
	st := &liveState{}
	if text, src, err := readLiveCredentials(); err == nil && text != "" {
		st.CredentialsText = text
		st.CredSource = src
		st.CredInfo = parseCredentials(text)
		st.HasCredentials = st.CredInfo != nil
	}
	if b, err := os.ReadFile(liveClaudeJSON); err == nil {
		text := string(b)
		if s, e, found, err := findTopLevelMember(text, "oauthAccount"); err == nil && found {
			st.OAuthJSON = text[s:e]
			st.Profile = parseProfile(st.OAuthJSON)
		}
	}
	st.Identity = identityOf(st.Profile)
	if st.Profile != nil {
		st.Email = st.Profile.Email
	}
	return st
}

// ---------------------------------------------------------------------------
// File helpers
// ---------------------------------------------------------------------------

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".cam-tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		// best effort (Windows ignores most bits)
		_ = err
	}
	if err := os.Rename(tmp, path); err != nil {
		// Windows can refuse to replace an open file via rename; fall back to copy.
		if werr := os.WriteFile(path, data, perm); werr != nil {
			os.Remove(tmp)
			return werr
		}
		os.Remove(tmp)
	}
	return nil
}

func backupLiveFile(path, tag string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if err := os.MkdirAll(backupDir(), 0o700); err != nil {
		return
	}
	name := filepath.Base(path)
	dest := filepath.Join(backupDir(), fmt.Sprintf("%s.%s.%s", name, time.Now().Format("20060102-150405"), tag))
	_ = os.WriteFile(dest, data, 0o600)
	// keep the last 10 per file
	entries, err := os.ReadDir(backupDir())
	if err != nil {
		return
	}
	var mine []os.DirEntry
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), name+".") {
			mine = append(mine, e)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].Name() > mine[j].Name() })
	for i := 10; i < len(mine); i++ {
		os.Remove(filepath.Join(backupDir(), mine[i].Name()))
	}
}

// setLiveFromSlot writes a saved account into Claude Code's live login locations.
func setLiveFromSlot(slot int) (*slotData, error) {
	blob, err := readSlot(slot)
	if err != nil {
		return nil, err
	}
	backupLiveFile(liveClaudeJSON, "pre-switch")
	backupLiveFile(liveCredPath, "pre-switch")
	if err := writeLiveCredentials(blob.Credentials); err != nil {
		return nil, fmt.Errorf("writing credentials: %w", err)
	}
	if blob.OAuthJSON != "" {
		text := "{\n}\n"
		if b, err := os.ReadFile(liveClaudeJSON); err == nil {
			text = string(b)
		}
		updated, err := setTopLevelMember(text, "oauthAccount", blob.OAuthJSON)
		if err != nil {
			return nil, err
		}
		if !json.Valid([]byte(updated)) { // must still parse before we commit it
			return nil, fmt.Errorf("refusing to write %s: result would not be valid JSON", liveClaudeJSON)
		}
		if err := writeFileAtomic(liveClaudeJSON, []byte(updated), 0o600); err != nil {
			return nil, err
		}
	}
	return blob, nil
}
