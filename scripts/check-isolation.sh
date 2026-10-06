#!/bin/sh
# Run inside Linux after Setup. Does not use accounts, model tokens or project code.
set -eu
image=codex-worker:0.160.0
network=codex-agents
test "$(docker network inspect "$network" --format '{{.Internal}}')" = true
common='--rm --network codex-agents --dns 127.0.0.1 --add-host proxy:172.30.250.2 --read-only --user 1000:1000 --cap-drop=ALL --security-opt=no-new-privileges:true --pids-limit=64 --memory=256m'
# shellcheck disable=SC2086
docker run $common --entrypoint node "$image" -e '
const net=require("net"),fs=require("fs");
if(fs.existsSync("/var/run/docker.sock"))process.exit(1);
try{fs.writeFileSync("/root/escape","x");process.exit(1)}catch(e){}
const s=net.connect({host:"1.1.1.1",port:443});
s.on("connect",()=>process.exit(1));s.on("error",()=>process.exit(0));setTimeout(()=>process.exit(0),3000);
'
# Forbidden destination through the only reachable proxy must return 403.
# shellcheck disable=SC2086
docker run $common --entrypoint node "$image" -e '
const http=require("http");
const r=http.request({hostname:"proxy",port:8080,method:"CONNECT",path:"github.com:443",headers:{host:"github.com:443"}});
r.on("connect",(res,s)=>{s.destroy();process.exit(res.statusCode===403?0:1)});
r.on("response",res=>process.exit(res.statusCode===403?0:1));r.on("error",()=>process.exit(1));r.setTimeout(5000,()=>process.exit(1));r.end();
'
echo 'Basic Docker isolation checks passed.'
# Host services must be unreachable even when they listen on the Docker bridge.
# shellcheck disable=SC2086
docker run $common --entrypoint node "$image" -e '
const net=require("net");const s=net.connect({host:"172.30.250.1",port:22});
s.on("connect",()=>process.exit(1));s.on("error",()=>process.exit(0));setTimeout(()=>process.exit(0),3000);
'
# External DNS must not provide another exfiltration channel.
# shellcheck disable=SC2086
docker run $common --entrypoint node "$image" -e '
require("dns").resolve4("example.com",e=>process.exit(e?0:1));setTimeout(()=>process.exit(0),5000);
'
fixture_volume="codex-isolation-fixture-$$"
docker volume create "$fixture_volume" >/dev/null
trap 'docker volume rm "$fixture_volume" >/dev/null' EXIT
docker run --rm --network none --read-only --cap-drop=ALL --mount "type=volume,src=$fixture_volume,dst=/workspace" --entrypoint sh "$image" -c 'printf test > /workspace/fixture; chmod 666 /workspace/fixture'
# Even a world-writable project file must be unmodifiable by a reviewer.
# shellcheck disable=SC2086
docker run $common --mount "type=volume,src=$fixture_volume,dst=/workspace,readonly" --entrypoint node "$image" -e '
const fs=require("fs");if(fs.readFileSync("/workspace/fixture","utf8")!=="test")process.exit(1);
try{fs.writeFileSync("/workspace/fixture","changed");process.exit(1)}catch(e){if(e.code!=="EROFS")process.exit(1)}
'
echo 'Read-only reviewer mount verified.'
