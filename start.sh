#!/bin/sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
test -f config.json && test -f secrets/integrations.json || { echo 'Run sh install.sh first.' >&2; exit 1; }
exec python3 - "$@" <<'PY'
import json, os, sys
with open("secrets/integrations.json", encoding="utf-8-sig") as source:
    secrets = json.load(source)
env = dict(os.environ)
for name, key in [("TRELLO_KEY", "trello_key"), ("TRELLO_TOKEN", "trello_token"), ("TELEGRAM_TOKEN", "telegram_token")]:
    env[name] = secrets[key]
args = ["bin/orchestrator", "-config", "config.json"]
if sys.argv[1:] == ["--check"]:
    args.append("-check")
elif sys.argv[1:]:
    sys.exit("Usage: sh start.sh [--check]")
os.execve(args[0], args, env)
PY
