#!/bin/sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
umask 077
test "$(uname -s)" = Darwin || { echo 'This installer is for macOS.' >&2; exit 1; }
echo 'Codex Orchestrator - macOS Docker setup (dedicated Colima VM)'
missing=''
formulas=''
for tool in python3 git docker colima; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    missing="$missing $tool"
    if [ "$tool" = python3 ]; then formulas="$formulas python"; else formulas="$formulas $tool"; fi
  fi
done
if [ -n "$missing" ]; then
  echo "Missing dependencies:$missing"
  command -v brew >/dev/null 2>&1 || { echo 'Install Homebrew from https://brew.sh, then rerun sh install.sh.' >&2; exit 1; }
  if ! command -v gh >/dev/null 2>&1; then formulas="$formulas gh"; fi
  printf 'Install missing prerequisites with Homebrew (%s)? [y/N] ' "$formulas"
  read -r answer
  # formulas contains only the fixed package names selected above, not user input.
  # shellcheck disable=SC2086
  case "$answer" in y|Y) brew install $formulas;; *) echo 'Install prerequisites, then rerun setup.' >&2; exit 1;; esac
fi
python3 -c 'import sys; assert sys.version_info >= (3,9), "Python 3.9 or later is required"'
if ! docker compose version >/dev/null 2>&1; then
  command -v brew >/dev/null 2>&1 || { echo 'Install Docker Compose v2 or later, then rerun.' >&2; exit 1; }
  plugin="$(brew --prefix)/lib/docker/cli-plugins/docker-compose"
  if [ ! -x "$plugin" ]; then
    printf 'Install Docker Compose using Homebrew? [y/N] '
    read -r answer
    case "$answer" in y|Y) brew install docker-compose;; *) exit 1;; esac
  fi
  plugin_dir="${DOCKER_CONFIG:-$HOME/.docker}/cli-plugins"
  mkdir -p "$plugin_dir"
  if [ ! -e "$plugin_dir/docker-compose" ] && [ ! -L "$plugin_dir/docker-compose" ]; then
    ln -s "$plugin" "$plugin_dir/docker-compose"
  fi
  docker compose version >/dev/null 2>&1 || { echo 'Docker Compose is still unavailable. Check the existing CLI plugin installation.' >&2; exit 1; }
fi
if ! colima status codex-orchestrator >/dev/null 2>&1; then
  echo 'Starting a dedicated Colima VM (4 CPUs, 6 GiB RAM). This can take a few minutes.'
  colima start codex-orchestrator --runtime docker --vm-type vz --cpus 4 --memory 6 --disk 60 --mount none --activate=false
fi
export DOCKER_CONTEXT=colima-codex-orchestrator
docker info >/dev/null
test "$(docker info --format '{{.OSType}}')" = linux || { echo 'The dedicated Colima profile must use Linux Docker.' >&2; exit 1; }
if [ ! -f bin/orchestrator ]; then
  command -v go >/dev/null 2>&1 || { echo 'Download the matching macOS release archive, or install Go to build from source.' >&2; exit 1; }
  mkdir -p bin
  go build -trimpath -o bin/orchestrator ./cmd/orchestrator
fi
chmod 700 bin/orchestrator
if ! bin/orchestrator --version; then
  echo 'Check that the archive matches your Mac (darwin-arm64 for Apple Silicon, darwin-amd64 for Intel).' >&2
  echo 'Packages are unsigned. If macOS blocked the binary, review System Settings > Privacy & Security before retrying.' >&2
  exit 1
fi
python3 scripts/configure-linux.py
docker build -t codex-worker:0.160.0 -f docker/Dockerfile.worker .
docker compose up -d --build
echo 'Applying network restrictions inside the dedicated VM, not to the macOS firewall.'
colima ssh codex-orchestrator -- sudo -n sh -s < scripts/network-policy.sh
sh scripts/check-isolation.sh
sh start.sh --check
echo 'Setup passed. Start with: sh start.sh'
echo 'Dashboard: http://127.0.0.1:8787 . Stop with Ctrl+C.'
echo 'After restarting the VM, reapply its network policy before starting agents (see INSTALL.md).'
