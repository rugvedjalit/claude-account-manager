package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Manifest (config.json): names and metadata, never secrets.
// Format is shared with the original PowerShell version.
// ---------------------------------------------------------------------------

type accountEntry struct {
	Name             string `json:"name"`
	Identity         string `json:"identity"`
	Email            string `json:"email"`
	OrgName          string `json:"orgName"`
	SubscriptionType string `json:"subscriptionType"`
	SavedAt          string `json:"savedAt"`
	UpdatedAt        string `json:"updatedAt"`
}

type manifest struct {
	Version  int                      `json:"version"`
	Active   *int                     `json:"active"`
	Accounts map[string]*accountEntry `json:"accounts"`
}

func readManifest() *manifest {
	m := &manifest{Version: 1, Accounts: map[string]*accountEntry{}}
	b, err := os.ReadFile(manifestPath())
	if err != nil {
		return m
	}
	if err := json.Unmarshal(b, m); err != nil {
		die("The account manager config is corrupt: "+manifestPath(), "Delete it and run: claude-account setup")
	}
	if m.Accounts == nil {
		m.Accounts = map[string]*accountEntry{}
	}
	return m
}

func saveManifest(m *manifest) {
	if err := os.MkdirAll(storeDir(), 0o700); err != nil {
		die("Cannot create "+storeDir()+": "+err.Error(), "")
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := writeFileAtomic(manifestPath(), b, 0o600); err != nil {
		die("Cannot write "+manifestPath()+": "+err.Error(), "")
	}
}

func (m *manifest) slots() []int {
	var out []int
	for k := range m.Accounts {
		if n, err := strconv.Atoi(k); err == nil {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func (m *manifest) entry(slot int) *accountEntry { return m.Accounts[strconv.Itoa(slot)] }

func (m *manifest) has(slot int) bool { return m.entry(slot) != nil }

func (m *manifest) name(slot int) string {
	if e := m.entry(slot); e != nil && e.Name != "" {
		return e.Name
	}
	return fmt.Sprintf("Account %d", slot)
}

func (m *manifest) findByIdentity(id string) (int, bool) {
	if id == "" {
		return 0, false
	}
	for _, s := range m.slots() {
		if m.entry(s).Identity == id {
			return s, true
		}
	}
	return 0, false
}

// resolveSlotArg accepts a slot number or an account name (case-insensitive).
func (m *manifest) resolveSlotArg(arg string) (int, bool) {
	if n, err := strconv.Atoi(arg); err == nil {
		return n, true
	}
	for _, s := range m.slots() {
		if strings.EqualFold(m.name(s), arg) {
			return s, true
		}
	}
	return 0, false
}

func (m *manifest) nextFreeSlot() (int, bool) {
	for i := 1; i <= maxSlots; i++ {
		if !m.has(i) {
			return i, true
		}
	}
	return 0, false
}

func (m *manifest) setActive(slot int) { s := slot; m.Active = &s }

// ---------------------------------------------------------------------------
// Encrypted slots
// ---------------------------------------------------------------------------

// slotPayload is what gets encrypted. Field names match the PowerShell version.
type slotPayload struct {
	Format      int    `json:"format"`
	Credentials string `json:"credentials"`
	OAuthJSON   string `json:"oauthAccountJson"`
	SavedAt     string `json:"savedAt"`
}

type slotData struct {
	Credentials string
	OAuthJSON   string
	SavedAt     string
	CredInfo    *credInfo
	Profile     *profile
}

func nowISO() string { return time.Now().Format(time.RFC3339) }

func saveSlot(m *manifest, slot int, credText, oauthJSON, name string) {
	info := parseCredentials(credText)
	if info == nil {
		die("Credential data is not in the expected Claude Code format (claudeAiOauth.accessToken/refreshToken).", "")
	}
	prof := parseProfile(oauthJSON)
	payload, _ := json.Marshal(slotPayload{Format: 1, Credentials: credText, OAuthJSON: oauthJSON, SavedAt: nowISO()})
	if err := secrets().Put(slot, payload); err != nil {
		die(fmt.Sprintf("Cannot save account %d: %v", slot, err), "")
	}
	existing := m.entry(slot)
	e := &accountEntry{SubscriptionType: info.SubscriptionType, UpdatedAt: nowISO(), SavedAt: nowISO()}
	switch {
	case name != "":
		e.Name = name
	case existing != nil && existing.Name != "":
		e.Name = existing.Name
	default:
		e.Name = fmt.Sprintf("Account %d", slot)
	}
	if existing != nil && existing.SavedAt != "" {
		e.SavedAt = existing.SavedAt
	}
	if prof != nil {
		e.Identity = identityOf(prof)
		e.Email = prof.Email
		e.OrgName = prof.OrgName
	}
	m.Accounts[strconv.Itoa(slot)] = e
	saveManifest(m)
}

func readSlot(slot int) (*slotData, error) {
	raw, err := secrets().Get(slot)
	if err != nil {
		return nil, fmt.Errorf("cannot read the saved credentials for account %d: %v\nRun: claude-account login %d", slot, err, slot)
	}
	var p slotPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("saved data for account %d is corrupt. Run: claude-account login %d", slot, slot)
	}
	return &slotData{Credentials: p.Credentials, OAuthJSON: p.OAuthJSON, SavedAt: p.SavedAt,
		CredInfo: parseCredentials(p.Credentials), Profile: parseProfile(p.OAuthJSON)}, nil
}

func removeSlot(m *manifest, slot int) {
	_ = secrets().Delete(slot)
	delete(m.Accounts, strconv.Itoa(slot))
	if m.Active != nil && *m.Active == slot {
		m.Active = nil
	}
	saveManifest(m)
}

// syncLiveToStore copies the live login (which Claude Code may have refreshed) back into its slot.
func syncLiveToStore(m *manifest, live *liveState, quiet bool) (int, bool) {
	if !live.HasCredentials || live.Identity == "" {
		return 0, false
	}
	slot, found := m.findByIdentity(live.Identity)
	if !found {
		if !quiet {
			warn(fmt.Sprintf("The current Claude Code login (%s) is not a saved account, so it was not saved. Run 'claude-account save' to add it.", live.Email))
		}
		return 0, false
	}
	saveSlot(m, slot, live.CredentialsText, live.OAuthJSON, "")
	return slot, true
}

// ---------------------------------------------------------------------------
// Secret backend selection
// ---------------------------------------------------------------------------

type secretStore interface {
	Put(slot int, data []byte) error
	Get(slot int) ([]byte, error)
	Delete(slot int) error
	Describe() string
}

var secretBackend secretStore

func secrets() secretStore {
	if secretBackend == nil {
		secretBackend = newSecretStore()
	}
	return secretBackend
}
