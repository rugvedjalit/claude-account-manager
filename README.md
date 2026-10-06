# Claude Account Manager

Use two or more Claude accounts with the **Claude Code CLI**. Log each account in once, switch between them with one command, and keep using the plain `claude` command and `claude --resume` exactly as before.

Works on **Windows, Linux and macOS**. One small binary per platform, no runtime dependencies (written in Go, standard library only).

```
ONE TIME:        claude-account setup        (login C1, login C2, done)
EVERY DAY:       claude                      (uses the selected account)
WHEN C1 IS OUT:  exit Claude Code
                 claude-account switch  ->  select C2
                 claude --resume        ->  same session, keep working
```

Verified against Claude Code **2.1.287** on Windows 11 (real accounts) and Ubuntu 24.04 (real Claude Code binary, test credentials). macOS support follows the scheme found in the Claude Code binary but has not been run on a Mac; see Limitations.

> **Not affiliated with Anthropic.** This is an independent community tool. It relies on how Claude Code stores its login today, which Anthropic does not document as a stable interface. Read [Limitations](#g-limitations-and-what-is-not-official) before using it.

## Quick install

**Windows** (PowerShell, no admin):

```powershell
irm https://raw.githubusercontent.com/rugvedjalit/claude-account-manager/main/install.ps1 | iex
```

**Linux / macOS**:

```bash
curl -fsSL https://raw.githubusercontent.com/rugvedjalit/claude-account-manager/main/install.sh | bash
```

Then open a new terminal and run `claude-account setup`. Prefer to read a script before running it? Download it, inspect it, and run it from disk instead; see [Installation](#c-installation).

---

## A. How it works

### What Claude Code stores where

| Data | Windows / Linux | macOS | Per account? |
|---|---|---|---|
| OAuth access + refresh token | `~/.claude/.credentials.json` (`claudeAiOauth` object) | Keychain item, service `Claude Code-credentials`, account `claude-code-user`; file fallback when the Keychain is unavailable | **Yes** |
| Account profile (email, org, account UUID) | top-level `oauthAccount` key in `~/.claude.json` | same | **Yes** |
| Project trust, allowed tools, MCP config, caches | other keys in `~/.claude.json` | same | No |
| Session transcripts | `~/.claude/projects/<encoded cwd>/<session-id>.jsonl` | same | No |
| History, settings, plugins, skills | `~/.claude/...` | same | No |

Only the first two rows differ between accounts. Transcripts carry the working directory and session id, not the login, which is why `claude --resume` works after switching.

With `CLAUDE_CONFIG_DIR` set, Claude Code moves `.credentials.json`, `.claude.json` and `projects/` into that directory (and on macOS suffixes the Keychain service name with the first 8 hex characters of the SHA-256 of that directory path).

### The tool

```
<store>/config.json            names, emails, which slot is active; no secrets
<store>/accounts/ or keyring   each account's credentials + oauthAccount JSON, encrypted
<store>/backups/               last 10 copies of the live files, taken before each switch

claude-account switch 2
  1. read the live login; if it is a saved account, save it back   (keeps refreshed tokens)
  2. decrypt slot 2; refuse if its refresh token has expired
  3. write slot 2's credentials to the live location (file or Keychain)
     splice slot 2's oauthAccount into ~/.claude.json   (only that key; rest byte-identical)
  4. ask `claude auth status --json` and confirm the email
```

- **No wrapper around `claude`.** The real binary reads the live login at startup, so after a switch it simply is the other account.
- **The second account is logged in without logging out the first.** `setup` runs `claude auth login` with `CLAUDE_CONFIG_DIR` pointed at a throw-away folder, captures the result, encrypts it, and scrubs the folder (and on macOS deletes the temporary Keychain item). `claude auth logout` is never called.
- **Refreshed tokens are not lost.** Every `switch`, `status` and `fetch` first copies the live login back into its slot, matched by account UUID.
- **`~/.claude.json` is never re-serialized.** A small JSON scanner replaces exactly the `oauthAccount` value. The result is validated before an atomic write, and the previous file is backed up.

Store location: `%LOCALAPPDATA%\claude-account` (Windows), `~/Library/Application Support/claude-account` (macOS), `$XDG_CONFIG_HOME/claude-account` or `~/.config/claude-account` (Linux). Override with `CLAUDE_ACCOUNT_HOME`.

## B. Files

```
claude-account-manager/
├── go/                    source (Go, standard library only)
├── install.ps1            Windows installer / uninstaller
├── install.sh             Linux + macOS installer / uninstaller
├── build.ps1, build.sh    cross-compile everything into dist/ (not committed)
├── test/linux-e2e.sh      Docker-based Linux end-to-end test
├── legacy-powershell/     the original Windows-only PowerShell version (superseded)
├── LICENSE                MIT
└── README.md
```

Prebuilt binaries for windows/linux/darwin on amd64/arm64 are attached to each [GitHub release](https://github.com/rugvedjalit/claude-account-manager/releases).

## C. Installation

The installers use a local build in `dist/` if one exists, otherwise they download the right binary for your OS and CPU from the latest GitHub release.

**Windows** (PowerShell, no admin):

```powershell
git clone https://github.com/rugvedjalit/claude-account-manager.git
cd claude-account-manager
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

Installs `claude-account.exe` to `%LOCALAPPDATA%\claude-account\bin` and adds it to the user PATH. Open a new terminal afterwards.

**Linux / macOS**:

```bash
git clone https://github.com/rugvedjalit/claude-account-manager.git
cd claude-account-manager
bash install.sh
```

Installs to `~/.local/bin/claude-account`, the same folder Claude Code's native installer uses. If that folder is not on your PATH the script prints the line to add.

**Manual**: download the binary for your platform from the [releases page](https://github.com/rugvedjalit/claude-account-manager/releases), rename it to `claude-account` (or `claude-account.exe`), make it executable, and put it anywhere on your PATH.

**Build from source** (any OS with Go 1.22+): `./build.sh` or `.\build.ps1` produces all six binaries in `dist/`, and the installers then use those.

Uninstall: `install.ps1 -Uninstall` or `bash install.sh --uninstall`. Add `-PurgeAccounts` / `--purge-accounts` to also delete saved accounts.

## D. First-time setup

Close Claude Code first, then run:

```
claude-account setup
```

```
Claude Account Manager - Setup
------------------------------

Claude Code 2.1.291 found at /home/you/.local/bin/claude
Claude's settings folder: /home/you/.claude
Saved accounts are kept in: your Linux keyring (Secret Service)

Step 1: Account 1
  Claude is signed in to you@work.com right now.
  Save this account as Account 1? [Y/n]: y
✓ Account 1 saved (you@work.com)

Do you want to add another Claude account? [Y/n]: y

Adding Account 2
  Your browser will open the normal Claude sign-in page.
  Sign in with the NEW account. If the browser signs you in to an account you
  already saved, use a private/incognito window or sign out on claude.ai first.
  If the browser shows a code instead of coming back here, paste it below.
  The account you're using now stays signed in.
  Press Enter to open the sign-in page:

  ... Claude's own sign-in: link, browser, "Paste code here if prompted" ...

✓ Account 2 saved (you@personal.com)

Do you want to add another Claude account? [y/N]: n

✓ All set.
```

- **Not signed in to Claude yet?** Step 1 opens the normal Claude sign-in first.
- **Signed in to the same account twice?** The tool notices and just updates it, so you never get duplicates.
- **Want more accounts later?** Run `claude-account add`. You can save up to 9.
- **Already signed in to an account with `/login`?** Run `claude-account fetch` to fetch that account and save it.

## E. Daily usage

```
claude                       # opens Claude Code with the account in use
```

When the account in use hits its usage limit:

```
1. Close Claude Code
2. claude-account switch        (or straight away: claude-account switch 2 / claude-account switch Work)

   Using now: Account 1 (you@work.com)

   [1] Account 1  you@work.com  (using now)
   [2] Account 2  you@personal.com

   Type the number of the account to use (or press Enter to cancel): 2
   ✓ Switched to Account 2 (you@personal.com)

3. claude --resume              # same project folder, pick the chat, keep working
```

### All commands

| Command | What it does |
|---|---|
| `claude-account` | Shows help, with the account in use and your saved accounts at the top |
| `claude-account setup` | First time? Start here. Saves your account and lets you add more |
| `claude-account add` | Add a new account (opens the browser to sign in) |
| `claude-account fetch` | Fetch the account Claude is signed in to now and save it |
| `claude-account switch` | Change to another account (shows a list) |
| `claude-account switch 2` | Change straight to account 2 (a name works too) |
| `claude-account list` | Show all saved accounts (`*` marks the one in use) |
| `claude-account status` | See which account is in use and how long each login lasts |
| `claude-account rename 2 Work` | Give an account an easy name |
| `claude-account delete 2` | Delete an account from the saved list (Claude stays signed in) |
| `claude-account login 2` | Sign in to an account again when its login has run out |
| `claude-account test` | Check the account in use works (sends one tiny message) |

Extra options: `--yes` (or `-y`) skips "are you sure?" questions, and `--email you@example.com` fills in the email on the sign-in page.

Older command names from earlier versions still work: `save`/`sync` (now `fetch`), `remove`/`rm` (now `delete`), `use` (now `switch`), and `--force` (now `--yes`).

## F. Security

**Saved (inactive) accounts**

| OS | Protection |
|---|---|
| Windows | DPAPI (`CryptProtectData`, current-user scope, app-specific entropy). Readable only by your Windows user on this machine. |
| macOS | Login Keychain generic password, service `claude-account-manager`. Falls back to a 0600 file if the Keychain is unavailable. |
| Linux | Secret Service (GNOME Keyring / KWallet) through `secret-tool` when a keyring is running. Otherwise a 0600 file, the same protection Claude Code gives its own live credentials file. `status` tells you which one is in use. |

**The active account** lives wherever Claude Code itself keeps it: a plaintext file on Windows and Linux, the Keychain on macOS. That is Claude Code's design and cannot be changed from outside.

**Also**: `config.json` holds names, emails and account UUIDs, no tokens. `backups/` holds copies of the live files taken before each switch (plaintext on Windows/Linux; delete the folder if unwanted). Passwords are never seen; tokens never appear on a command line, in shell history or in environment variables; the temporary login folder is zero-filled and deleted; removed slots are zero-filled before deletion.

## G. Limitations and what is not official

- **Multiple accounts sharing one session history is not an official feature.** Anthropic's documented way to use two accounts is a separate `CLAUDE_CONFIG_DIR` per account, which gives each account its own session history. That is exactly what this tool avoids, so that `claude --resume` sees the same sessions under both accounts. The credential JSON layout, the `oauthAccount` key, and the Keychain naming scheme are observed behaviour, not documented API. The tool validates the layout before every switch and refuses (backups intact) if it changes.
- **macOS is implemented from the binary's own scheme but untested on a Mac.** First run `claude-account status` and `claude-account setup`, then `claude-account test`; if anything looks off, `claude auth login` always restores a normal login.
- **Refresh tokens expire when unused** (about 16 to 30 days after last use, as observed). `status` shows the remaining validity and warns at 3 days; `claude-account login N` signs that account in again.
- **Exit Claude Code before switching.** A running session can write its old login back when it refreshes. The tool refuses inside a Claude Code shell and warns if a `claude` process is running (`--yes` skips the check). It cannot see Claude Code hosted by Node (npm installs).
- **Plan limits are per account.** Nothing here pools usage.
- **The old PowerShell version** in `legacy-powershell/` still works on Windows and shares the same store format, but is no longer the recommended install.

## License

[MIT](LICENSE). Not affiliated with or endorsed by Anthropic. "Claude" and "Claude Code" are trademarks of Anthropic.
