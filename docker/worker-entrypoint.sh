#!/bin/sh
set -eu
if [ "$1" = "init-auth" ]; then
  # Do not follow paths left by an untrusted agent during a previous turn.
  test ! -L /home/codex/.codex
  mkdir -p /home/codex/.codex
  rm -f /home/codex/.codex/auth.json
  umask 077
  cat > /home/codex/.codex/auth.json
  chown 1000:1000 /home/codex /home/codex/.codex /home/codex/.codex/auth.json
  exit 0
fi
test "$1" = "run"
model="$2"
effort="$3"
session="${4:-}"
result="/home/codex/result.json"
rm -f "$result"
# Strip default integrations too: a fresh CODEX_HOME alone does not disable
# built-in apps/plugins, hooks, browsers or automatic model delegation.
set -- --ignore-user-config --ignore-rules --skip-git-repo-check --json \
  --disable apps --disable plugins --disable remote_plugin --disable hooks \
  --disable multi_agent --disable multi_agent_v2 --disable computer_use \
  --disable browser_use --disable browser_use_external --disable in_app_browser \
  --disable daemon_auto_start --disable workspace_dependencies \
  --disable skill_mcp_dependency_install --disable skill_search \
  --disable image_generation --disable tool_suggest --disable auth_elicitation \
  -c 'sandbox_mode="danger-full-access"' -c 'approval_policy="never"' \
  -c 'web_search="live"' -c "model_reasoning_effort=\"$effort\"" \
  --model "$model" --output-schema /opt/result.schema.json --output-last-message "$result"
if [ -n "$session" ]; then
  codex exec resume "$@" "$session" -
else
  codex exec "$@" -
fi
printf '\nORCHESTRATOR_RESULT='
cat "$result"
