#!/bin/bash
# Linux end-to-end test: real Claude Code binary, fake (structurally valid) credentials.
set -u
export PATH="$HOME/.local/bin:$PATH"
apt-get update -qq >/dev/null 2>&1 && apt-get install -y -qq curl ca-certificates procps >/dev/null 2>&1
curl -fsSL https://claude.ai/install.sh | bash >/tmp/install.log 2>&1
echo "claude: $(claude --version 2>&1)"
cp /mnt/repo/dist/linux-amd64/claude-account /usr/local/bin/claude-account && chmod +x /usr/local/bin/claude-account
mk() { # $1 dir $2 email $3 uuid $4 tokentag
  mkdir -p "$1"
  now=$(date +%s); exp=$(( (now+8*3600)*1000 )); rexp=$(( (now+16*86400)*1000 ))
  printf '{"claudeAiOauth":{"accessToken":"sk-ant-oat01-%s","refreshToken":"sk-ant-ort01-%s","expiresAt":%s,"refreshTokenExpiresAt":%s,"scopes":["user:inference"],"subscriptionType":"max"}}' "$4" "$4" "$exp" "$rexp" > "$1/.credentials.json"
  chmod 600 "$1/.credentials.json"
  printf '{"numStartups":3,"projects":{"/root/work":{"allowedTools":["Bash"]}},"oauthAccount":{"accountUuid":"%s","emailAddress":"%s","organizationName":"Org"}}' "$3" "$2" > "$1/.claude.json"
}
mk /root/cfgA alice@example.com aaaaaaaa-0000-0000-0000-000000000001 ALICE
mk /root/cfgB bob@example.com   bbbbbbbb-0000-0000-0000-000000000002 BOB
export CLAUDE_ACCOUNT_HOME=/root/store
export CLAUDE_CONFIG_DIR=/root/cfgA
echo "##### version"; claude-account version
echo "##### auth status sees fake login?"; claude auth status --json | grep -E 'loggedIn|email'
echo "##### setup (y,n)"; printf 'y\nn\n' | claude-account setup; echo "exit=$?"
echo "##### save bob from cfgB"; CLAUDE_CONFIG_DIR=/root/cfgB claude-account fetch --yes; echo "exit=$?"
echo "##### status"; claude-account status; echo "exit=$?"
echo "##### switch 2"; claude-account switch 2 --force; echo "exit=$?"
echo "live creds: $(grep -o 'sk-ant-ort01-[A-Z]*' /root/cfgA/.credentials.json)  perms: $(stat -c %a /root/cfgA/.credentials.json)"
echo "claude.json projects intact: $(grep -c '"/root/work"' /root/cfgA/.claude.json)  email: $(grep -o "bob@example.com" /root/cfgA/.claude.json | head -1)"
claude auth status --json | grep -E 'loggedIn|email'
echo "##### menu -> 1"; printf '1\n' | claude-account switch --force; echo "exit=$?"
echo "live creds: $(grep -o 'sk-ant-ort01-[A-Z]*' /root/cfgA/.credentials.json)"
echo "##### running-process guard (claude running in background, answer n)"; (claude -p "wait" --max-turns 1 >/dev/null 2>&1 &); sleep 2
printf 'n\n' | claude-account switch 2; echo "exit=$?"
echo "##### rename/list/remove"; claude-account rename 2 Work; claude-account list; claude-account delete work --yes; claude-account list
echo "##### store layout"; find /root/store -type f -exec ls -la {} \;
echo "##### no claude on PATH"; PATH=/usr/bin:/bin /usr/local/bin/claude-account status; echo "exit=$?"
