package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

var (
	claudeExe     string
	claudeVersion string
)

// resolveClaude locates the claude executable on PATH (or dies with install instructions).
func resolveClaude() {
	if claudeExe != "" {
		return
	}
	p, err := exec.LookPath("claude")
	if err != nil {
		die("Claude Code is not installed (the `claude` command was not found).",
			"Install it first: https://code.claude.com/docs/en/setup\nThen open a new terminal and run this command again.")
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	claudeExe = p
	r := runClaude([]string{"--version"}, nil, false)
	if m := regexp.MustCompile(`(\d+\.\d+\.\d+)`).FindStringSubmatch(r.out); m != nil {
		claudeVersion = m[1]
	} else {
		claudeVersion = "unknown"
	}
}

type runResult struct {
	exit int
	out  string
}

// runClaude runs the real Claude Code binary. Extra env vars apply only to the child.
// Interactive mode hands over stdin/stdout/stderr so login prompts and URLs are visible.
func runClaude(args []string, env map[string]string, interactive bool) runResult {
	resolveClaude()
	var cmd *exec.Cmd
	ext := strings.ToLower(filepath.Ext(claudeExe))
	if runtime.GOOS == "windows" && (ext == ".cmd" || ext == ".bat") {
		cmd = exec.Command("cmd", append([]string{"/c", claudeExe}, args...)...)
	} else {
		cmd = exec.Command(claudeExe, args...)
	}
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if interactive {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		err := cmd.Run()
		return runResult{exit: exitCodeOf(cmd, err)}
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	err := cmd.Run()
	return runResult{exit: exitCodeOf(cmd, err), out: out.String()}
}

func exitCodeOf(cmd *exec.Cmd, err error) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

type authStatus struct {
	LoggedIn         bool   `json:"loggedIn"`
	AuthMethod       string `json:"authMethod"`
	ConfigDirectory  string `json:"configDirectory"`
	Email            string `json:"email"`
	OrgID            string `json:"orgId"`
	OrgName          string `json:"orgName"`
	SubscriptionType string `json:"subscriptionType"`
}

// getAuthStatus runs `claude auth status --json` (a local read; exit 1 when logged out).
func getAuthStatus(configDir string) *authStatus {
	var env map[string]string
	if configDir != "" {
		env = map[string]string{"CLAUDE_CONFIG_DIR": configDir}
	}
	r := runClaude([]string{"auth", "status", "--json"}, env, false)
	i := strings.Index(r.out, "{")
	if i < 0 {
		return nil
	}
	var st authStatus
	if err := json.Unmarshal([]byte(r.out[i:]), &st); err != nil {
		return nil
	}
	return &st
}

// assertClaudeSupported checks the installed Claude Code has the auth subcommands this
// tool relies on and adopts the config directory it reports.
func assertClaudeSupported() {
	resolveClaude()
	r := runClaude([]string{"auth", "--help"}, nil, false)
	if r.exit != 0 || !strings.Contains(r.out, "login") || !strings.Contains(r.out, "status") {
		die(fmt.Sprintf("This Claude Code version (%s) can't be used: it has no 'claude auth' commands.", claudeVersion),
			fmt.Sprintf("Update Claude Code and try again (this tool was tested with version %s).", testedClaude))
	}
	if st := getAuthStatus(""); st != nil && st.ConfigDirectory != "" {
		adoptConfigDir(st.ConfigDirectory)
	}
	if claudeVersion != "unknown" {
		if major(claudeVersion) != major(testedClaude) {
			warn(fmt.Sprintf("You have Claude Code %s, but this tool was tested with %s. It will probably work, but check with: claude-account test", claudeVersion, testedClaude))
		}
	}
}

func major(v string) string {
	if i := strings.Index(v, "."); i > 0 {
		return v[:i]
	}
	return v
}

// claudeRunningPIDs lists running Claude Code processes (native binary only).
func claudeRunningPIDs() []int {
	var pids []int
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq claude.exe", "/FO", "CSV", "/NH").Output()
		if err != nil {
			return nil
		}
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Split(line, "\",\"")
			if len(f) >= 2 && strings.HasPrefix(line, "\"claude.exe") {
				if n, err := strconv.Atoi(strings.Trim(f[1], "\"\r ")); err == nil {
					pids = append(pids, n)
				}
			}
		}
		return pids
	}
	out, err := exec.Command("pgrep", "-x", "claude").Output()
	if err != nil {
		return nil
	}
	for _, s := range strings.Fields(string(out)) {
		if n, err := strconv.Atoi(s); err == nil && n != os.Getpid() {
			pids = append(pids, n)
		}
	}
	return pids
}

// assertNotRunning refuses to swap logins underneath a running Claude Code session.
func assertNotRunning(force bool) {
	if os.Getenv("CLAUDECODE") != "" && !force {
		die("You are running this from inside Claude Code.",
			"Close Claude Code first, then run this again in a normal terminal.\n(Add --yes to skip this check.)")
	}
	pids := claudeRunningPIDs()
	if len(pids) > 0 && !force {
		var ps []string
		for _, p := range pids {
			ps = append(ps, strconv.Itoa(p))
		}
		warn(fmt.Sprintf("Claude Code is still open (process %s).", strings.Join(ps, ", ")))
		fmt.Println("  If it stays open, it may quietly switch you back to the old account.")
		if !yesNo("  Change account anyway?", false) {
			fmt.Println()
			fmt.Println("Cancelled. Close Claude Code and try again.")
			os.Exit(2)
		}
	}
}
