#!/bin/sh
# Control-flow test with fake host tools: no VM, Docker daemon or accounts used.
set -eu
root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
fixture="$(mktemp -d "${TMPDIR:-/tmp}/codex-macos-install.XXXXXX")"
mkdir -p "$fixture/scripts" "$fixture/bin" "$fixture/mockbin"
cp "$root/scripts/install-macos.sh" "$fixture/scripts/install-macos.sh"
printf 'fixture network policy\n' > "$fixture/scripts/network-policy.sh"
export CODEX_INSTALL_TEST_LOG="$fixture/commands.log"
export CODEX_INSTALL_TEST_POLICY="$fixture/applied-policy.txt"
cat > "$fixture/mockbin/uname" <<'SH'
#!/bin/sh
echo Darwin
SH
cat > "$fixture/mockbin/python3" <<'SH'
#!/bin/sh
printf 'python %s\n' "$*" >> "$CODEX_INSTALL_TEST_LOG"
SH
cat > "$fixture/mockbin/docker" <<'SH'
#!/bin/sh
printf 'docker context=%s args=%s\n' "${DOCKER_CONTEXT:-unset}" "$*" >> "$CODEX_INSTALL_TEST_LOG"
if [ "$*" = "info --format {{.OSType}}" ]; then echo linux; fi
SH
cat > "$fixture/mockbin/colima" <<'SH'
#!/bin/sh
printf 'colima %s\n' "$*" >> "$CODEX_INSTALL_TEST_LOG"
if [ "$1" = status ]; then exit 1; fi
if [ "$1" = ssh ]; then cat > "$CODEX_INSTALL_TEST_POLICY"; fi
SH
for name in sudo iptables sysctl; do
 cat > "$fixture/mockbin/$name" <<'SH'
#!/bin/sh
echo 'ERROR: attempted host firewall or sudo operation' >&2
exit 99
SH
done
cat > "$fixture/bin/orchestrator" <<'SH'
#!/bin/sh
echo 'Codex Orchestrator test fixture'
SH
cat > "$fixture/scripts/check-isolation.sh" <<'SH'
#!/bin/sh
echo isolation-check >> "$CODEX_INSTALL_TEST_LOG"
SH
cat > "$fixture/start.sh" <<'SH'
#!/bin/sh
printf 'start %s\n' "$*" >> "$CODEX_INSTALL_TEST_LOG"
SH
chmod +x "$fixture/mockbin/"* "$fixture/bin/orchestrator"
PATH="$fixture/mockbin:$PATH" sh "$fixture/scripts/install-macos.sh"
grep -F 'colima start codex-orchestrator --runtime docker --vm-type vz --cpus 4 --memory 6 --disk 60 --mount none --activate=false' "$CODEX_INSTALL_TEST_LOG" >/dev/null
grep -F 'docker context=colima-codex-orchestrator args=build ' "$CODEX_INSTALL_TEST_LOG" >/dev/null
grep -F 'docker context=colima-codex-orchestrator args=compose up -d --build' "$CODEX_INSTALL_TEST_LOG" >/dev/null
grep -F 'colima ssh codex-orchestrator -- sudo -n sh -s' "$CODEX_INSTALL_TEST_LOG" >/dev/null
cmp "$fixture/scripts/network-policy.sh" "$CODEX_INSTALL_TEST_POLICY"
grep -F 'isolation-check' "$CODEX_INSTALL_TEST_LOG" >/dev/null
grep -F 'start --check' "$CODEX_INSTALL_TEST_LOG" >/dev/null
echo 'macOS installer routing passed with mocked tools; no host firewall or real VM touched.'
