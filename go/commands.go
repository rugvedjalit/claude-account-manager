package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// help
// ---------------------------------------------------------------------------

func helpSection(t string) { fmt.Println(); fmt.Println(paint(cYellow, t)) }

func helpRow(cmd, desc, note string) {
	line := "  " + paint(cCyan, fmt.Sprintf("%-34s", cmd)) + desc
	if note != "" {
		line += paint(cGray, "  "+note)
	}
	fmt.Println(line)
}

func showHelp() {
	title(fmt.Sprintf("Claude Account Manager %s", toolVersion))
	func() {
		defer func() { recover() }()
		m := readManifest()
		live := getLiveState()
		cur, has := m.findByIdentity(live.Identity)
		fmt.Print("Right now:  ")
		switch {
		case has:
			fmt.Print(paint(cGreen, fmt.Sprintf("%s (%s)", m.name(cur), live.Email)))
		case live.HasCredentials:
			fmt.Print(paint(cYellow, live.Email+" - not saved yet, run: claude-account save"))
		default:
			fmt.Print(paint(cYellow, "not logged in"))
		}
		fmt.Println(paint(cGray, fmt.Sprintf("   |   Saved accounts: %d", len(m.slots()))))
		for _, s := range m.slots() {
			mark := " "
			if has && s == cur {
				mark = "*"
			}
			fmt.Println(paint(cGray, fmt.Sprintf("            %s [%d] %-14s %s", mark, s, m.name(s), m.entry(s).Email)))
		}
	}()

	helpSection("EVERY DAY")
	helpRow("claude", "Start Claude Code with the active account", "(unchanged)")
	helpRow("claude-account switch", "Pick another account from a menu", "(exit Claude Code first)")
	helpRow("claude-account switch 2", "Switch straight to account 2 (or a name)", "")
	helpRow("claude --resume", "Continue the same conversation on the new account", "")

	helpSection("ADD / REMOVE ACCOUNTS")
	helpRow("claude-account setup", "First-time setup, or add another account via browser login", "")
	helpRow("claude-account save", "Add the account Claude Code is logged in as right now", "(after /login)")
	helpRow("claude-account remove 2", "Forget account 2", "")
	helpRow("claude-account rename 2 Personal", "Name an account; then: claude-account switch Personal", "")

	helpSection("CHECK / FIX")
	helpRow("claude-account status", "Active account, saved accounts, when each login expires", "")
	helpRow("claude-account list", "Short list of saved accounts", "")
	helpRow("claude-account login 2", "Log account 2 in again", "(when status says EXPIRED)")
	helpRow("claude-account test", "Send one tiny prompt to prove the active account works", "")

	helpSection("OPTIONS")
	helpRow("--force   (or -y)", "Skip confirmation questions", "")
	helpRow("--email you@example.com", "Pre-fill the email on the login page", "(setup, login)")

	helpSection("EXAMPLE: account 1 hits its usage limit")
	fmt.Println("  1. Exit Claude Code")
	fmt.Println("  2. " + paint(cCyan, "claude-account switch") + "   and pick 2")
	fmt.Println("  3. " + paint(cCyan, "claude --resume") + "         same conversation, now on account 2")
	fmt.Println()
}

// ---------------------------------------------------------------------------
// setup
// ---------------------------------------------------------------------------

func saveLiveAsNewSlot(m *manifest, live *liveState, fl flags) (int, bool) {
	next, free := m.nextFreeSlot()
	if !free {
		die(fmt.Sprintf("All %d account slots are used.", maxSlots), "Remove one with: claude-account remove <n>")
	}
	fmt.Printf("Claude Code is currently logged in as %s, which is not a saved account.\n", live.Email)
	if !fl.force && !yesNo(fmt.Sprintf("Save it as Account %d?", next), true) {
		fmt.Println("Not saved.")
		return 0, false
	}
	saveSlot(m, next, live.CredentialsText, live.OAuthJSON, "")
	m.setActive(next)
	saveManifest(m)
	ok(fmt.Sprintf("Account %d saved (%s) and marked active", next, live.Email))
	return next, true
}

func cmdSetup(fl flags) {
	assertClaudeSupported()
	title("Claude Account Manager - Setup")
	fmt.Printf("Claude Code %s detected at %s\n", claudeVersion, claudeExe)
	fmt.Printf("Config directory: %s\n", configDir)
	fmt.Printf("Saved accounts are protected by: %s\n\n", secrets().Describe())

	m := readManifest()
	live := getLiveState()

	if len(m.slots()) == 0 {
		fmt.Println(paint(cBold, "Step 1: Account 1"))
		switch {
		case live.HasCredentials && live.Identity != "":
			fmt.Printf("  You are currently logged in to Claude Code as %s.\n", live.Email)
			if yesNo("  Save this login as Account 1?", true) {
				saveSlot(m, 1, live.CredentialsText, live.OAuthJSON, "")
				m.setActive(1)
				saveManifest(m)
				ok(fmt.Sprintf("Account 1 saved (%s)", live.Email))
			} else {
				fmt.Println()
				fmt.Println("  A browser window will open. Sign in with the account you want as Account 1.")
				fmt.Println("  Your current Claude Code login will be replaced by it.")
				readLine("  Press Enter to continue: ")
				cap, err := isolatedLogin(fl.email)
				if err != nil {
					die(err.Error(), "Run: claude-account setup   to try again.")
				}
				saveSlot(m, 1, cap.Credentials, cap.OAuthJSON, "")
				if _, err := setLiveFromSlot(1); err != nil {
					die("Saved Account 1 but could not activate it: "+err.Error(), "")
				}
				m.setActive(1)
				saveManifest(m)
				ok(fmt.Sprintf("Account 1 saved and activated (%s)", parseProfile(cap.OAuthJSON).Email))
			}
		default:
			fmt.Println("  No Claude Code login found. Starting the normal Claude Code login flow...")
			fmt.Println("  (a browser window will open; sign in with your FIRST account)")
			fmt.Println()
			if r := runClaude([]string{"auth", "login"}, nil, true); r.exit != 0 {
				die("Claude Code login did not complete.", "Run: claude-account setup   to try again.")
			}
			live = getLiveState()
			if !live.HasCredentials {
				die("Login finished but no credentials were written by Claude Code.", "Expected: "+liveCredentialDescription())
			}
			saveSlot(m, 1, live.CredentialsText, live.OAuthJSON, "")
			m.setActive(1)
			saveManifest(m)
			ok(fmt.Sprintf("Account 1 saved (%s)", live.Email))
		}
		fmt.Println()
	} else {
		fmt.Println("Already configured:")
		for _, s := range m.slots() {
			fmt.Printf("  [%d] %s  %s\n", s, m.name(s), m.entry(s).Email)
		}
		fmt.Println()
		if live.HasCredentials && live.Identity != "" {
			if _, synced := syncLiveToStore(m, live, true); !synced {
				saveLiveAsNewSlot(m, live, fl)
				fmt.Println()
			}
		}
	}

	first := true
	for {
		next, free := m.nextFreeSlot()
		if !free {
			warn(fmt.Sprintf("All %d account slots are used.", maxSlots))
			break
		}
		def := first && len(m.slots()) < 2
		if !yesNo("Do you want to add another Claude account?", def) {
			break
		}
		first = false
		fmt.Println()
		fmt.Println(paint(cBold, fmt.Sprintf("Step: Account %d", next)))
		fmt.Println("  A browser window will open for the Claude login flow.")
		fmt.Println("  Sign in with the OTHER account. If the browser is already signed in to claude.ai")
		fmt.Println("  with an account you have saved, use a private/incognito window or sign out there first.")
		fmt.Println("  If the browser shows a code instead of returning, paste it at the prompt.")
		fmt.Println("  Your current Claude Code login is NOT touched by this step.")
		readLine("  Press Enter to open the login: ")
		fmt.Println()
		cap, err := isolatedLogin(fl.email)
		if err != nil {
			fail(err.Error())
			fmt.Println()
			continue
		}
		prof := parseProfile(cap.OAuthJSON)
		if dup, found := m.findByIdentity(identityOf(prof)); found {
			saveSlot(m, dup, cap.Credentials, cap.OAuthJSON, "")
			warn(fmt.Sprintf("That is the same account as %s (%s). Its saved login was refreshed instead of adding a new account.", m.name(dup), prof.Email))
		} else {
			saveSlot(m, next, cap.Credentials, cap.OAuthJSON, "")
			ok(fmt.Sprintf("Account %d saved (%s)", next, prof.Email))
		}
		fmt.Println()
	}

	fmt.Println()
	ok("Setup complete.")
	fmt.Println()
	showStatus(true)
	fmt.Println()
	fmt.Println("Next: just run `claude`. To change account: claude-account switch")
}

// ---------------------------------------------------------------------------
// switch
// ---------------------------------------------------------------------------

func cmdSwitch(args []string, fl flags) {
	assertClaudeSupported()
	m := readManifest()
	slots := m.slots()
	if len(slots) == 0 {
		die("No accounts are configured.", "Run:\n\n  claude-account setup\n\nto save your accounts.")
	}
	live := getLiveState()
	cur, hasCur := m.findByIdentity(live.Identity)
	curLabel := "(not logged in)"
	if hasCur {
		curLabel = m.name(cur)
	} else if live.HasCredentials {
		curLabel = "(unsaved login: " + live.Email + ")"
	}

	var target int
	if len(args) >= 1 {
		t, okArg := m.resolveSlotArg(args[0])
		if !okArg {
			die(fmt.Sprintf("Unknown account '%s'.", args[0]), "Run: claude-account list")
		}
		target = t
	} else {
		title("Claude Account Manager")
		fmt.Printf("Current account: %s\n\n", curLabel)
		for _, s := range slots {
			mark := ""
			if hasCur && s == cur {
				mark = " (active)"
			}
			fmt.Printf("[%d] %s  %s%s\n", s, m.name(s), m.entry(s).Email, mark)
		}
		fmt.Println()
		answer := strings.TrimSpace(readLine("Select account: "))
		if answer == "" {
			fmt.Println("Cancelled.")
			return
		}
		t, okArg := m.resolveSlotArg(answer)
		if !okArg {
			die(fmt.Sprintf("Unknown account '%s'.", answer), "")
		}
		target = t
	}

	if !m.has(target) {
		die(fmt.Sprintf("Account %d is not configured.", target), "Run:\n\n  claude-account setup\n\nto add another account.")
	}
	name := m.name(target)

	if hasCur && cur == target {
		syncLiveToStore(m, live, true)
		m.setActive(target)
		saveManifest(m)
		fmt.Println()
		ok(fmt.Sprintf("%s is already the active account (%s).", name, live.Email))
		return
	}

	assertNotRunning(fl.force)

	// 1. Save the outgoing login (Claude Code may have refreshed its tokens since we stored it).
	syncLiveToStore(m, live, false)

	// 2. Check the incoming login before touching anything.
	blob, err := readSlot(target)
	if err != nil {
		die(err.Error(), "")
	}
	if !blob.CredInfo.RefreshExpires.IsZero() && blob.CredInfo.RefreshExpires.Before(time.Now()) {
		die(fmt.Sprintf("%s's saved login has expired (refresh token %s).", name, formatWhen(blob.CredInfo.RefreshExpires)),
			fmt.Sprintf("Re-authenticate it with:\n\n  claude-account login %d", target))
	}

	// 3. Swap.
	if _, err := setLiveFromSlot(target); err != nil {
		die("Switching failed: "+err.Error(), "Backups of the previous files are in:\n  "+backupDir())
	}
	m.setActive(target)
	saveManifest(m)

	// 4. Verify with Claude Code itself.
	st := getAuthStatus("")
	expected := ""
	if blob.Profile != nil {
		expected = blob.Profile.Email
	}
	fmt.Println()
	if st != nil && st.LoggedIn && (expected == "" || strings.EqualFold(st.Email, expected)) {
		ok(fmt.Sprintf("Switched to %s (%s)", name, st.Email))
		if !blob.CredInfo.AccessExpires.IsZero() && blob.CredInfo.AccessExpires.Before(time.Now()) {
			dim("  (access token is expired; Claude Code refreshes it automatically on next start)")
		}
		return
	}
	seen := "no status output"
	if st != nil {
		seen = fmt.Sprintf("loggedIn=%v email=%s", st.LoggedIn, st.Email)
	}
	die(fmt.Sprintf("Files were switched to %s but Claude Code does not report it as logged in (%s).", name, seen),
		fmt.Sprintf("Try:\n\n  claude-account login %d\n\nBackups of the previous files are in:\n  %s", target, backupDir()))
}

// ---------------------------------------------------------------------------
// status / list
// ---------------------------------------------------------------------------

func cmdStatus() {
	assertClaudeSupported()
	title("Claude Account Manager")
	showStatus(false)
}

func showStatus(brief bool) {
	m := readManifest()
	slots := m.slots()
	live := getLiveState()
	cur, hasCur := m.findByIdentity(live.Identity)

	switch {
	case hasCur:
		fmt.Printf("Active account: %s (%s)\n", m.name(cur), live.Email)
		if !brief {
			syncLiveToStore(m, live, true)
		}
	case live.HasCredentials:
		fmt.Println(paint(cYellow, fmt.Sprintf("Active account: unsaved login (%s) - run 'claude-account save' to add it", live.Email)))
	default:
		fmt.Println(paint(cYellow, "Active account: none (Claude Code is logged out)"))
	}
	fmt.Println()

	if len(slots) == 0 {
		fmt.Println(paint(cYellow, "No accounts configured. Run: claude-account setup"))
	}
	for _, s := range slots {
		e := m.entry(s)
		state, detail := "configured", ""
		if blob, err := readSlot(s); err != nil {
			state, detail = "UNREADABLE", err.Error()
		} else if rx := blob.CredInfo.RefreshExpires; !rx.IsZero() {
			switch {
			case rx.Before(time.Now()):
				state = "EXPIRED"
				detail = fmt.Sprintf("login expired %s - run: claude-account login %d", rx.Format("2006-01-02"), s)
			case rx.Before(time.Now().Add(72 * time.Hour)):
				detail = fmt.Sprintf("refresh token %s - use it soon or run: claude-account login %d", formatWhen(rx), s)
			default:
				detail = "refresh token " + formatWhen(rx)
			}
		}
		active := ""
		if hasCur && s == cur {
			active = "  <- active"
		}
		sub := ""
		if e.SubscriptionType != "" {
			sub = ", " + e.SubscriptionType
		}
		fmt.Printf("%s: %s  (%s%s)%s\n", m.name(s), state, e.Email, sub, active)
		if detail != "" && !brief {
			dim("    " + detail)
		}
	}
	fmt.Println()
	if claudeExe != "" {
		fmt.Printf("Claude Code: detected (%s, %s)\n", claudeVersion, claudeExe)
	}
	if !brief {
		dim("Live credentials: " + liveCredentialDescription())
		dim("Saved accounts:   " + secrets().Describe())
	}
}

func cmdList() {
	m := readManifest()
	live := getLiveState()
	cur, hasCur := m.findByIdentity(live.Identity)
	slots := m.slots()
	if len(slots) == 0 {
		fmt.Println("No accounts configured. Run: claude-account setup")
		return
	}
	for _, s := range slots {
		mark := " "
		if hasCur && s == cur {
			mark = "*"
		}
		fmt.Printf("%s [%d] %-16s %s\n", mark, s, m.name(s), m.entry(s).Email)
	}
}

// ---------------------------------------------------------------------------
// login / remove / rename / save / test
// ---------------------------------------------------------------------------

func cmdLogin(args []string, fl flags) {
	assertClaudeSupported()
	m := readManifest()
	if len(args) < 1 {
		die("Usage: claude-account login <n> [--email address]", "")
	}
	slot, okArg := m.resolveSlotArg(args[0])
	if !okArg || slot < 1 || slot > maxSlots {
		die(fmt.Sprintf("Unknown account '%s'.", args[0]), "")
	}
	entry := m.entry(slot)
	name := m.name(slot)
	live := getLiveState()
	isActive := live.Identity != "" && entry != nil && entry.Identity == live.Identity

	title("Re-authenticate " + name)
	if entry != nil && entry.Email != "" {
		fmt.Printf("  Sign in as %s in the browser window that opens.\n", entry.Email)
	} else {
		fmt.Printf("  Sign in with the account you want to save as %s.\n", name)
	}
	fmt.Println("  If the browser is signed in to a different Claude account, use a private window.")
	readLine("  Press Enter to open the login: ")
	email := fl.email
	if email == "" && entry != nil {
		email = entry.Email
	}
	cap, err := isolatedLogin(email)
	if err != nil {
		die(err.Error(), "")
	}
	prof := parseProfile(cap.OAuthJSON)
	id := identityOf(prof)
	if entry != nil && entry.Identity != "" && id != entry.Identity && !fl.force {
		die(fmt.Sprintf("You signed in as %s, but %s is %s.", prof.Email, name, entry.Email),
			fmt.Sprintf("Sign in with the right account, or use --force to replace %s with %s.", name, prof.Email))
	}
	if other, found := m.findByIdentity(id); found && other != slot {
		die(fmt.Sprintf("%s is already saved as %s.", prof.Email, m.name(other)), "")
	}
	saveSlot(m, slot, cap.Credentials, cap.OAuthJSON, "")
	ok(fmt.Sprintf("%s re-authenticated (%s)", name, prof.Email))
	if isActive {
		assertNotRunning(fl.force)
		if _, err := setLiveFromSlot(slot); err != nil {
			die("Saved, but updating the live login failed: "+err.Error(), "")
		}
		m.setActive(slot)
		saveManifest(m)
		ok(fmt.Sprintf("Live Claude Code login updated to the new %s credentials", name))
	}
}

func cmdRemove(args []string, fl flags) {
	m := readManifest()
	if len(args) < 1 {
		die("Usage: claude-account remove <n>", "")
	}
	slot, okArg := m.resolveSlotArg(args[0])
	if !okArg || !m.has(slot) {
		die(fmt.Sprintf("Account '%s' is not configured.", args[0]), "Run: claude-account list")
	}
	name := m.name(slot)
	e := m.entry(slot)
	if !fl.force && !yesNo(fmt.Sprintf("Forget %s (%s)?", name, e.Email), false) {
		fmt.Println("Cancelled.")
		return
	}
	removeSlot(m, slot)
	ok(name + " removed from the account manager.")
	if live := getLiveState(); live.Identity != "" && e.Identity == live.Identity {
		dim("  Claude Code itself is still logged in with it. Use `claude auth logout` if you also want that gone.")
	}
}

func cmdRename(args []string) {
	m := readManifest()
	if len(args) < 2 {
		die("Usage: claude-account rename <n> <new name>", "")
	}
	slot, okArg := m.resolveSlotArg(args[0])
	if !okArg || !m.has(slot) {
		die(fmt.Sprintf("Account '%s' is not configured.", args[0]), "")
	}
	newName := strings.TrimSpace(strings.Join(args[1:], " "))
	if newName == "" {
		die("The new name cannot be empty.", "")
	}
	if _, err := strconv.Atoi(newName); err == nil {
		die("The name cannot be just a number (it would clash with slot numbers).", "")
	}
	old := m.name(slot)
	m.entry(slot).Name = newName
	saveManifest(m)
	ok(fmt.Sprintf("Renamed '%s' to '%s'", old, newName))
}

func cmdSave(fl flags) {
	assertClaudeSupported()
	m := readManifest()
	live := getLiveState()
	if !live.HasCredentials || live.Identity == "" {
		die("Claude Code is not logged in; nothing to save.", "Run: claude auth login   or   claude-account switch")
	}
	if slot, synced := syncLiveToStore(m, live, true); synced {
		ok(fmt.Sprintf("Saved the live login into %s (%s)", m.name(slot), live.Email))
		return
	}
	if _, saved := saveLiveAsNewSlot(m, live, fl); !saved {
		os.Exit(1)
	}
}

func cmdTest() {
	assertClaudeSupported()
	m := readManifest()
	live := getLiveState()
	cur, hasCur := m.findByIdentity(live.Identity)
	label := "the current login"
	if hasCur {
		label = m.name(cur)
	}
	if !live.HasCredentials {
		die("Claude Code is not logged in.", "Run: claude-account switch   or   claude-account setup")
	}
	fmt.Printf("Sending a one-line test prompt as %s (%s)...\n", label, live.Email)
	r := runClaude([]string{"-p", "Reply with the single word OK and nothing else.", "--max-turns", "1"}, nil, false)
	out := strings.TrimSpace(r.out)
	if r.exit == 0 && out != "" {
		ok(fmt.Sprintf("%s works. Reply: %s", label, out))
		return
	}
	hint := "If the message above mentions authentication or an expired token, re-authenticate with:\n\n  claude-account login " + strconv.Itoa(cur) +
		"\n\nIf it mentions a usage limit, switch to another account:\n\n  claude-account switch"
	die(fmt.Sprintf("%s did not work (exit code %d).", label, r.exit), hint)
}
