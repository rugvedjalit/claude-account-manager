package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Small wording helpers
// ---------------------------------------------------------------------------

// planName turns "max" into "Max plan".
func planName(sub string) string {
	if sub == "" {
		return ""
	}
	return strings.ToUpper(sub[:1]) + sub[1:] + " plan"
}

// loginLife describes how long a saved login keeps working, in plain words.
func loginLife(slot int, rx time.Time) (state, detail string) {
	if rx.IsZero() {
		return "ready", ""
	}
	left := time.Until(rx)
	switch {
	case left < 0:
		return "LOGIN RAN OUT", fmt.Sprintf("ran out on %s - sign in again with: claude-account login %d", rx.Format("2006-01-02"), slot)
	case left < 72*time.Hour:
		return "ready", fmt.Sprintf("login runs out in %s - use this account soon, or run: claude-account login %d", formatLeft(left), slot)
	default:
		return "ready", fmt.Sprintf("login still good for %s", formatLeft(left))
	}
}

// ---------------------------------------------------------------------------
// help
// ---------------------------------------------------------------------------

func helpSection(t string) { fmt.Println(); fmt.Println(paint(cYellow, t)) }

func helpRow(cmd, desc, note string) {
	line := "  " + paint(cCyan, fmt.Sprintf("%-32s", cmd)) + desc
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
		fmt.Print("Using now:  ")
		switch {
		case has:
			fmt.Print(paint(cGreen, fmt.Sprintf("%s (%s)", m.name(cur), live.Email)))
		case live.HasCredentials:
			fmt.Print(paint(cYellow, live.Email+" - not saved yet, run: claude-account fetch"))
		default:
			fmt.Print(paint(cYellow, "not signed in"))
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
	helpRow("claude", "Open Claude Code with the account in use", "(nothing changes)")
	helpRow("claude-account switch", "Change to another account (shows a list)", "(close Claude Code first)")
	helpRow("claude-account switch 2", "Change straight to account 2 (a name works too)", "")
	helpRow("claude --resume", "Carry on the same chat with the new account", "")

	helpSection("ADD ACCOUNTS")
	helpRow("claude-account setup", "First time? Start here. Saves your account and adds more", "")
	helpRow("claude-account add", "Add a new account (opens the browser to sign in)", "")
	helpRow("claude-account fetch", "Fetch the account Claude is signed in to now and save it", "(after /login)")

	helpSection("MANAGE ACCOUNTS")
	helpRow("claude-account list", "Show all saved accounts", "")
	helpRow("claude-account rename 2 Work", "Give account 2 an easy name, then: claude-account switch Work", "")
	helpRow("claude-account delete 2", "Delete account 2 from the saved list", "")

	helpSection("CHECK / FIX")
	helpRow("claude-account status", "See which account is in use and how long each login lasts", "")
	helpRow("claude-account login 2", "Sign in to account 2 again", "(when its login has run out)")
	helpRow("claude-account test", "Check the account in use works (sends one tiny message)", "")

	helpSection("EXTRA OPTIONS")
	helpRow("--yes   (or -y)", "Don't ask \"are you sure?\"", "")
	helpRow("--email you@example.com", "Fill in the email on the sign-in page for you", "(setup, add, login)")

	helpSection("EXAMPLE: account 1 has hit its usage limit")
	fmt.Println("  1. Close Claude Code")
	fmt.Println("  2. " + paint(cCyan, "claude-account switch") + "   and pick 2")
	fmt.Println("  3. " + paint(cCyan, "claude --resume") + "         same chat, now on account 2")
	fmt.Println()
}

// ---------------------------------------------------------------------------
// setup / add / fetch
// ---------------------------------------------------------------------------

func saveLiveAsNewSlot(m *manifest, live *liveState, fl flags) (int, bool) {
	next, free := m.nextFreeSlot()
	if !free {
		die(fmt.Sprintf("You already have the most accounts allowed (%d).", maxSlots), "Delete one first with: claude-account delete <number>")
	}
	fmt.Printf("Claude is signed in to %s, which isn't saved yet.\n", live.Email)
	if !fl.force && !yesNo(fmt.Sprintf("Save it as Account %d?", next), true) {
		fmt.Println("Not saved.")
		return 0, false
	}
	saveSlot(m, next, live.CredentialsText, live.OAuthJSON, "")
	m.setActive(next)
	saveManifest(m)
	ok(fmt.Sprintf("Fetched %s and saved it as Account %d. It's the account in use now.", live.Email, next))
	return next, true
}

// addOneAccount opens the normal Claude sign-in for a new account without signing
// out of the account in use, then saves it in the next free number.
func addOneAccount(m *manifest, fl flags) bool {
	next, free := m.nextFreeSlot()
	if !free {
		warn(fmt.Sprintf("You already have the most accounts allowed (%d). Delete one with: claude-account delete <number>", maxSlots))
		return false
	}
	fmt.Println()
	fmt.Println(paint(cBold, fmt.Sprintf("Adding Account %d", next)))
	fmt.Println("  Your browser will open the normal Claude sign-in page.")
	fmt.Println("  Sign in with the NEW account. If the browser signs you in to an account you")
	fmt.Println("  already saved, use a private/incognito window or sign out on claude.ai first.")
	fmt.Println("  If the browser shows a code instead of coming back here, paste it below.")
	fmt.Println("  The account you're using now stays signed in.")
	readLine("  Press Enter to open the sign-in page: ")
	fmt.Println()
	cap, err := isolatedLogin(fl.email)
	if err != nil {
		fail(err.Error())
		fmt.Println()
		return false
	}
	prof := parseProfile(cap.OAuthJSON)
	if dup, found := m.findByIdentity(identityOf(prof)); found {
		saveSlot(m, dup, cap.Credentials, cap.OAuthJSON, "")
		warn(fmt.Sprintf("You already saved this account as %s (%s). Its login was updated instead of adding it twice.", m.name(dup), prof.Email))
	} else {
		saveSlot(m, next, cap.Credentials, cap.OAuthJSON, "")
		ok(fmt.Sprintf("Account %d saved (%s)", next, prof.Email))
	}
	fmt.Println()
	return true
}

func cmdSetup(fl flags) {
	assertClaudeSupported()
	title("Claude Account Manager - Setup")
	fmt.Printf("Claude Code %s found at %s\n", claudeVersion, claudeExe)
	fmt.Printf("Claude's settings folder: %s\n", configDir)
	fmt.Printf("Saved accounts are kept in: %s\n\n", secrets().Describe())

	m := readManifest()
	live := getLiveState()

	if len(m.slots()) == 0 {
		fmt.Println(paint(cBold, "Step 1: Account 1"))
		switch {
		case live.HasCredentials && live.Identity != "":
			fmt.Printf("  Claude is signed in to %s right now.\n", live.Email)
			if yesNo("  Save this account as Account 1?", true) {
				saveSlot(m, 1, live.CredentialsText, live.OAuthJSON, "")
				m.setActive(1)
				saveManifest(m)
				ok(fmt.Sprintf("Account 1 saved (%s)", live.Email))
			} else {
				fmt.Println()
				fmt.Println("  Your browser will open. Sign in with the account you want as Account 1.")
				fmt.Println("  Claude will then use that account instead of the current one.")
				readLine("  Press Enter to continue: ")
				cap, err := isolatedLogin(fl.email)
				if err != nil {
					die(err.Error(), "Try again with: claude-account setup")
				}
				saveSlot(m, 1, cap.Credentials, cap.OAuthJSON, "")
				if _, err := setLiveFromSlot(1); err != nil {
					die("Account 1 was saved, but Claude couldn't be changed over to it: "+err.Error(), "")
				}
				m.setActive(1)
				saveManifest(m)
				ok(fmt.Sprintf("Account 1 saved and in use (%s)", parseProfile(cap.OAuthJSON).Email))
			}
		default:
			fmt.Println("  Claude isn't signed in yet. Opening the normal Claude sign-in...")
			fmt.Println("  (your browser will open; sign in with your FIRST account)")
			fmt.Println()
			if r := runClaude([]string{"auth", "login"}, nil, true); r.exit != 0 {
				die("Sign-in was not finished.", "Try again with: claude-account setup")
			}
			live = getLiveState()
			if !live.HasCredentials {
				die("Sign-in finished, but Claude didn't save a login.", "Expected it here: "+liveCredentialDescription())
			}
			saveSlot(m, 1, live.CredentialsText, live.OAuthJSON, "")
			m.setActive(1)
			saveManifest(m)
			ok(fmt.Sprintf("Account 1 saved (%s)", live.Email))
		}
		fmt.Println()
	} else {
		fmt.Println("Accounts you already saved:")
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
		if _, free := m.nextFreeSlot(); !free {
			warn(fmt.Sprintf("You already have the most accounts allowed (%d).", maxSlots))
			break
		}
		def := first && len(m.slots()) < 2
		if !yesNo("Do you want to add another Claude account?", def) {
			break
		}
		first = false
		addOneAccount(m, fl)
	}

	fmt.Println()
	ok("All set.")
	fmt.Println()
	showStatus(true)
	fmt.Println()
	fmt.Println("Next: just run `claude`. To change account: claude-account switch")
}

func cmdAdd(fl flags) {
	assertClaudeSupported()
	m := readManifest()
	if len(m.slots()) == 0 {
		// Nothing saved yet: the full setup saves the current account first.
		cmdSetup(fl)
		return
	}
	title("Claude Account Manager - Add an account")
	if !addOneAccount(m, fl) {
		os.Exit(1)
	}
	showStatus(true)
}

func cmdFetch(fl flags) {
	assertClaudeSupported()
	m := readManifest()
	live := getLiveState()
	if !live.HasCredentials || live.Identity == "" {
		die("Claude isn't signed in to any account, so there is nothing to fetch.",
			"Sign in first with: claude auth login\nThen run: claude-account fetch")
	}
	if slot, synced := syncLiveToStore(m, live, true); synced {
		ok(fmt.Sprintf("%s is already saved (%s). Its login was updated.", m.name(slot), live.Email))
		return
	}
	if _, saved := saveLiveAsNewSlot(m, live, fl); !saved {
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// switch
// ---------------------------------------------------------------------------

func cmdSwitch(args []string, fl flags) {
	assertClaudeSupported()
	m := readManifest()
	slots := m.slots()
	if len(slots) == 0 {
		die("You haven't saved any accounts yet.", "Start with:\n\n  claude-account setup")
	}
	live := getLiveState()
	cur, hasCur := m.findByIdentity(live.Identity)
	curLabel := "(not signed in)"
	if hasCur {
		curLabel = fmt.Sprintf("%s (%s)", m.name(cur), live.Email)
	} else if live.HasCredentials {
		curLabel = live.Email + " (not saved yet)"
	}

	var target int
	if len(args) >= 1 {
		t, okArg := m.resolveSlotArg(args[0])
		if !okArg {
			die(fmt.Sprintf("There is no saved account called '%s'.", args[0]), "See your accounts with: claude-account list")
		}
		target = t
	} else {
		title("Claude Account Manager")
		fmt.Printf("Using now: %s\n\n", curLabel)
		for _, s := range slots {
			mark := ""
			if hasCur && s == cur {
				mark = paint(cGreen, "  (using now)")
			}
			fmt.Printf("[%d] %s  %s%s\n", s, m.name(s), m.entry(s).Email, mark)
		}
		fmt.Println()
		answer := strings.TrimSpace(readLine("Type the number of the account to use (or press Enter to cancel): "))
		if answer == "" {
			fmt.Println("Cancelled. Nothing changed.")
			return
		}
		t, okArg := m.resolveSlotArg(answer)
		if !okArg {
			die(fmt.Sprintf("There is no saved account called '%s'.", answer), "")
		}
		target = t
	}

	if !m.has(target) {
		die(fmt.Sprintf("Account %d isn't saved.", target), "Add it with:\n\n  claude-account add")
	}
	name := m.name(target)

	if hasCur && cur == target {
		syncLiveToStore(m, live, true)
		m.setActive(target)
		saveManifest(m)
		fmt.Println()
		ok(fmt.Sprintf("%s is already the account in use (%s).", name, live.Email))
		return
	}

	assertNotRunning(fl.force)

	// 1. Keep the account we are leaving up to date (Claude renews its login while you work).
	syncLiveToStore(m, live, false)

	// 2. Check the account we are changing to before touching anything.
	blob, err := readSlot(target)
	if err != nil {
		die(err.Error(), "")
	}
	if rx := blob.CredInfo.RefreshExpires; !rx.IsZero() && rx.Before(time.Now()) {
		die(fmt.Sprintf("The saved login for %s has run out (on %s).", name, rx.Format("2006-01-02")),
			fmt.Sprintf("Sign in to it again with:\n\n  claude-account login %d", target))
	}

	// 3. Change over.
	if _, err := setLiveFromSlot(target); err != nil {
		die("Couldn't change account: "+err.Error(), "Nothing was lost. Copies of the old login files are in:\n  "+backupDir())
	}
	m.setActive(target)
	saveManifest(m)

	// 4. Ask Claude Code itself which account it now sees.
	st := getAuthStatus("")
	expected := ""
	if blob.Profile != nil {
		expected = blob.Profile.Email
	}
	fmt.Println()
	if st != nil && st.LoggedIn && (expected == "" || strings.EqualFold(st.Email, expected)) {
		ok(fmt.Sprintf("Switched to %s (%s)", name, st.Email))
		if !blob.CredInfo.AccessExpires.IsZero() && blob.CredInfo.AccessExpires.Before(time.Now()) {
			dim("  (Claude will renew this login by itself when it starts)")
		}
		return
	}
	seen := "Claude gave no answer"
	if st != nil {
		if st.LoggedIn {
			seen = "Claude says it is signed in as " + st.Email
		} else {
			seen = "Claude says it is not signed in"
		}
	}
	die(fmt.Sprintf("Changed to %s, but something is wrong: %s.", name, seen),
		fmt.Sprintf("Sign in to it again with:\n\n  claude-account login %d\n\nCopies of the old login files are in:\n  %s", target, backupDir()))
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
		fmt.Println("Using now: " + paint(cGreen, fmt.Sprintf("%s (%s)", m.name(cur), live.Email)))
		if !brief {
			syncLiveToStore(m, live, true)
		}
	case live.HasCredentials:
		fmt.Println(paint(cYellow, fmt.Sprintf("Using now: %s - not saved yet, run: claude-account fetch", live.Email)))
	default:
		fmt.Println(paint(cYellow, "Using now: nothing (Claude isn't signed in)"))
	}
	fmt.Println()

	if len(slots) == 0 {
		fmt.Println(paint(cYellow, "No saved accounts yet. Start with: claude-account setup"))
	} else {
		fmt.Println("Saved accounts:")
	}
	for _, s := range slots {
		e := m.entry(s)
		state, detail := "ready", ""
		if blob, err := readSlot(s); err != nil {
			state, detail = "CAN'T OPEN", err.Error()
		} else {
			state, detail = loginLife(s, blob.CredInfo.RefreshExpires)
		}
		plan := ""
		if p := planName(e.SubscriptionType); p != "" {
			plan = ", " + p
		}
		active := ""
		if hasCur && s == cur {
			active = paint(cGreen, "  <- using now")
		}
		stateText := state
		if state != "ready" {
			stateText = paint(cRed, state)
		}
		fmt.Printf("  [%d] %s: %s  (%s%s)%s\n", s, m.name(s), stateText, e.Email, plan, active)
		if detail != "" && !brief {
			dim("      " + detail)
		}
	}
	fmt.Println()
	if claudeExe != "" {
		fmt.Printf("Claude Code: found (version %s, %s)\n", claudeVersion, claudeExe)
	}
	if !brief {
		dim("Claude's login is in:      " + liveCredentialDescription())
		dim("Saved accounts are kept in: " + secrets().Describe())
	}
}

func cmdList() {
	m := readManifest()
	live := getLiveState()
	cur, hasCur := m.findByIdentity(live.Identity)
	slots := m.slots()
	if len(slots) == 0 {
		fmt.Println("No saved accounts yet. Start with: claude-account setup")
		return
	}
	fmt.Println("Saved accounts (* = using now):")
	for _, s := range slots {
		mark := " "
		if hasCur && s == cur {
			mark = "*"
		}
		fmt.Printf("%s [%d] %-16s %s\n", mark, s, m.name(s), m.entry(s).Email)
	}
	if !hasCur && live.HasCredentials {
		fmt.Println()
		fmt.Printf("Claude is signed in to %s, which isn't saved yet. Run: claude-account fetch\n", live.Email)
	}
}

// ---------------------------------------------------------------------------
// login / delete / rename / test
// ---------------------------------------------------------------------------

func cmdLogin(args []string, fl flags) {
	assertClaudeSupported()
	m := readManifest()
	if len(args) < 1 {
		die("Which account? For example: claude-account login 2", "")
	}
	slot, okArg := m.resolveSlotArg(args[0])
	if !okArg || slot < 1 || slot > maxSlots {
		die(fmt.Sprintf("There is no saved account called '%s'.", args[0]), "See your accounts with: claude-account list")
	}
	entry := m.entry(slot)
	name := m.name(slot)
	live := getLiveState()
	isActive := live.Identity != "" && entry != nil && entry.Identity == live.Identity

	title("Sign in again: " + name)
	if entry != nil && entry.Email != "" {
		fmt.Printf("  Sign in as %s in the browser window that opens.\n", entry.Email)
	} else {
		fmt.Printf("  Sign in with the account you want to save as %s.\n", name)
	}
	fmt.Println("  If the browser signs you in to a different Claude account, use a private window.")
	readLine("  Press Enter to open the sign-in page: ")
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
			fmt.Sprintf("Sign in with the right account. Or, to replace %s with %s, run the same command with --yes.", name, prof.Email))
	}
	if other, found := m.findByIdentity(id); found && other != slot {
		die(fmt.Sprintf("%s is already saved as %s.", prof.Email, m.name(other)), "")
	}
	saveSlot(m, slot, cap.Credentials, cap.OAuthJSON, "")
	ok(fmt.Sprintf("%s is signed in again (%s)", name, prof.Email))
	if isActive {
		assertNotRunning(fl.force)
		if _, err := setLiveFromSlot(slot); err != nil {
			die("Saved, but Claude couldn't be changed over to the new login: "+err.Error(), "")
		}
		m.setActive(slot)
		saveManifest(m)
		ok(fmt.Sprintf("Claude is now using the new login for %s", name))
	}
}

func cmdDelete(args []string, fl flags) {
	m := readManifest()
	if len(args) < 1 {
		die("Which account? For example: claude-account delete 2", "")
	}
	slot, okArg := m.resolveSlotArg(args[0])
	if !okArg || !m.has(slot) {
		die(fmt.Sprintf("There is no saved account called '%s'.", args[0]), "See your accounts with: claude-account list")
	}
	name := m.name(slot)
	e := m.entry(slot)
	if !fl.force && !yesNo(fmt.Sprintf("Delete %s (%s) from the saved list?", name, e.Email), false) {
		fmt.Println("Cancelled. Nothing changed.")
		return
	}
	removeSlot(m, slot)
	ok(name + " deleted from the saved list.")
	if live := getLiveState(); live.Identity != "" && e.Identity == live.Identity {
		dim("  Claude is still signed in to it right now. Run `claude auth logout` if you want to sign out too.")
	}
}

func cmdRename(args []string) {
	m := readManifest()
	if len(args) < 2 {
		die("Give the account number and the new name. For example: claude-account rename 2 Work", "")
	}
	slot, okArg := m.resolveSlotArg(args[0])
	if !okArg || !m.has(slot) {
		die(fmt.Sprintf("There is no saved account called '%s'.", args[0]), "See your accounts with: claude-account list")
	}
	newName := strings.TrimSpace(strings.Join(args[1:], " "))
	if newName == "" {
		die("The new name can't be empty.", "")
	}
	if _, err := strconv.Atoi(newName); err == nil {
		die("The name can't be only a number, because numbers are used to pick accounts.", "Try something like: Work, Personal, Team")
	}
	old := m.name(slot)
	m.entry(slot).Name = newName
	saveManifest(m)
	ok(fmt.Sprintf("Renamed '%s' to '%s'. You can now run: claude-account switch %s", old, newName, newName))
}

func cmdTest() {
	assertClaudeSupported()
	m := readManifest()
	live := getLiveState()
	cur, hasCur := m.findByIdentity(live.Identity)
	label := "the account in use"
	if hasCur {
		label = m.name(cur)
	}
	if !live.HasCredentials {
		die("Claude isn't signed in to any account.", "Pick one with: claude-account switch")
	}
	fmt.Printf("Sending one tiny test message as %s (%s)...\n", label, live.Email)
	r := runClaude([]string{"-p", "Reply with the single word OK and nothing else.", "--max-turns", "1"}, nil, false)
	out := strings.TrimSpace(r.out)
	if r.exit == 0 && out != "" {
		ok(fmt.Sprintf("%s works. Claude replied: %s", label, out))
		return
	}
	hint := "If the message above talks about signing in or an expired login, sign in again with:\n\n  claude-account login " + strconv.Itoa(cur) +
		"\n\nIf it talks about a usage limit, change to another account:\n\n  claude-account switch"
	die(fmt.Sprintf("%s didn't work (Claude exited with code %d).", label, r.exit), hint)
}
