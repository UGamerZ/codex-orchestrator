#!/bin/sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
umask 077
echo 'Codex Orchestrator - Linux Docker installation'
for tool in python3 git docker; do
  command -v "$tool" >/dev/null 2>&1 || { echo "Missing $tool. Install the prerequisites listed in INSTALL.md, then rerun." >&2; exit 1; }
done
python3 -c 'import sys; assert sys.version_info >= (3,9), "Python 3.9 or later is required"'
docker info >/dev/null 2>&1 || { echo 'Docker Engine must be running and accessible to your current user.' >&2; exit 1; }
docker compose version >/dev/null
if [ ! -f bin/orchestrator ]; then
  command -v go >/dev/null 2>&1 || { echo 'Use the Linux release archive, which includes the compiled program, or install Go.' >&2; exit 1; }
  mkdir -p bin
  go build -trimpath -o bin/orchestrator ./cmd/orchestrator
fi
chmod 700 bin/orchestrator
bin/orchestrator --version
python3 scripts/configure-linux.py
echo 'Building isolated worker and broker images. The first build can take several minutes.'
docker build -t codex-worker:0.160.0 -f docker/Dockerfile.worker .
docker compose up -d --build
echo 'Installing firewall rules for the dedicated codex-agents Docker network.'
if [ "$(id -u)" = 0 ]; then sh scripts/network-policy.sh; else sudo sh scripts/network-policy.sh; fi
sh scripts/check-isolation.sh
sh start.sh --check
echo 'Ready. Run: sh start.sh'
echo 'Dashboard: http://127.0.0.1:8787 (use an SSH tunnel for remote access).'
