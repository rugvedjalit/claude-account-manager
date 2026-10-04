package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	configDir      string // Claude Code config directory (CLAUDE_CONFIG_DIR or ~/.claude)
	liveCredPath   string // <configDir>/.credentials.json
	liveClaudeJSON string // ~/.claude.json, or <configDir>/.claude.json under CLAUDE_CONFIG_DIR
)

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		h = os.Getenv("HOME")
	}
	return h
}

func init() {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		adoptConfigDir(d)
	} else {
		adoptConfigDir(filepath.Join(homeDir(), ".claude"))
	}
}

// adoptConfigDir sets the live file locations following Claude Code's own rules.
func adoptConfigDir(dir string) {
	dir = strings.TrimRight(dir, `\/`)
	configDir = dir
	liveCredPath = filepath.Join(dir, ".credentials.json")
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		liveClaudeJSON = filepath.Join(dir, ".claude.json")
	} else {
		liveClaudeJSON = filepath.Join(homeDir(), ".claude.json")
	}
}

// storeDir is where this tool keeps its manifest, encrypted slots and backups.
func storeDir() string {
	if d := os.Getenv("CLAUDE_ACCOUNT_HOME"); d != "" {
		return d
	}
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(homeDir(), "AppData", "Local")
		}
		return filepath.Join(base, "claude-account")
	case "darwin":
		return filepath.Join(homeDir(), "Library", "Application Support", "claude-account")
	default:
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(homeDir(), ".config")
		}
		return filepath.Join(base, "claude-account")
	}
}

func accountsDir() string { return filepath.Join(storeDir(), "accounts") }
func backupDir() string   { return filepath.Join(storeDir(), "backups") }
func tmpRoot() string     { return filepath.Join(storeDir(), "tmp") }
func manifestPath() string { return filepath.Join(storeDir(), "config.json") }
