#!/bin/sh
# Trusted host setup. Only this dedicated Docker subnet is affected.
set -eu
test "$(id -u)" = 0
subnet=172.30.250.0/24
proxy=172.30.250.2
sysctl -w net.bridge.bridge-nf-call-iptables=1 >/dev/null
iptables -nL DOCKER-USER >/dev/null
iptables -N CODEX-AGENTS 2>/dev/null || true
iptables -F CODEX-AGENTS
iptables -A CODEX-AGENTS -s "$subnet" -d "$proxy" -p tcp --dport 8080 -j ACCEPT
iptables -A CODEX-AGENTS -s "$proxy" -d "$subnet" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
iptables -A CODEX-AGENTS -s "$subnet" -j DROP
iptables -A CODEX-AGENTS -j RETURN
iptables -C DOCKER-USER -j CODEX-AGENTS 2>/dev/null || iptables -I DOCKER-USER 1 -j CODEX-AGENTS
# Docker internal networks alone do not prohibit access to services on the host.
iptables -C INPUT -s "$subnet" -j DROP 2>/dev/null || iptables -I INPUT 1 -s "$subnet" -j DROP
echo 'Agent subnet can reach only the model proxy; host and peer traffic blocked.'
