// Claude Account Manager: use several Claude accounts with the Claude Code CLI.
//
// Claude Code keeps its login in two places:
//   - the OAuth tokens: ~/.claude/.credentials.json (Windows, Linux, macOS fallback)
//     or the macOS Keychain item "Claude Code-credentials" / account "claude-code-user"
//   - the account profile: top-level "oauthAccount" key in ~/.claude.json
//
// Everything else (projects/, sessions, history, settings) is account-independent.
// This tool keeps an encrypted copy of each account's login state (DPAPI on Windows,
// Keychain on macOS, secret service on Linux) and swaps it into the live location on
// demand, so the plain `claude` command and `claude --resume` keep working unchanged.
package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	toolVersion  = "2.0.0"
	testedClaude = "2.1.287"
	maxSlots     = 9
)

type flags struct {
	force bool
	email string
}

func main() {
	initConsole()
	fl := flags{}
	var positional []string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--force" || a == "-f" || a == "--yes" || a == "-y":
			fl.force = true
		case strings.HasPrefix(a, "--email="):
			fl.email = strings.TrimPrefix(a, "--email=")
		case a == "--email" && i+1 < len(args):
			fl.email = args[i+1]
			i++
		case a == "-h" || a == "--help" || a == "/?":
			showHelp()
			return
		default:
			positional = append(positional, a)
		}
	}
	cmd := "help"
	if len(positional) > 0 {
		cmd = strings.ToLower(positional[0])
	}
	var rest []string
	if len(positional) > 1 {
		rest = positional[1:]
	}

	defer func() {
		if r := recover(); r != nil {
			if f, ok := r.(fatalError); ok {
				fmt.Println()
				fail(f.msg)
				if f.hint != "" {
					fmt.Println()
					fmt.Println(f.hint)
				}
				fmt.Println()
				os.Exit(f.code)
			}
			panic(r)
		}
	}()

	switch cmd {
	case "setup", "add":
		cmdSetup(fl)
	case "switch", "use":
		cmdSwitch(rest, fl)
	case "status":
		cmdStatus()
	case "list", "ls":
		cmdList()
	case "login":
		cmdLogin(rest, fl)
	case "remove", "rm":
		cmdRemove(rest, fl)
	case "rename":
		cmdRename(rest)
	case "save", "sync":
		cmdSave(fl)
	case "test":
		cmdTest()
	case "version", "--version":
		fmt.Printf("claude-account %s (%s)\n", toolVersion, platformName())
	case "help":
		showHelp()
	default:
		fail(fmt.Sprintf("Unknown command '%s'.", cmd))
		showHelp()
		os.Exit(1)
	}
}

// fatalError is raised with die() and turned into a message + exit code in main.
type fatalError struct {
	msg, hint string
	code      int
}

func die(msg, hint string) {
	panic(fatalError{msg: msg, hint: hint, code: 1})
}

func dieCode(msg, hint string, code int) {
	panic(fatalError{msg: msg, hint: hint, code: code})
}
