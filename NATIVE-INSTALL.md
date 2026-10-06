# Windows installation without Docker or a VM

**English (default)** | [Русский](docs/ru/NATIVE-INSTALL.md)

This backend uses Codex's elevated Windows sandbox and the dedicated
`CodexSandboxOffline` account. The Docker backend remains available. Project copies
reside on the Windows disk under `native_root\jobs`, as selected in configuration.

From an Administrator PowerShell in the repository directory:

```powershell
.\scripts\Setup-Native.ps1
# Configure config.native.json and secrets/integrations.json; prepare secrets/auth.json.
.\scripts\Start-Orchestrator.ps1 -Config config.native.json -Check
.\scripts\Start-Orchestrator.ps1 -Config config.native.json
```

Install Git, Go, and Codex CLI; GitHub mode also requires GitHub CLI. Set the absolute
Codex and GitHub CLI paths in configuration. Authentication uses a ChatGPT login;
API keys are not accepted. The selected project account supplies GitHub access.
Local mode does not use GitHub. See [PROJECT-SETUP.md](PROJECT-SETUP.md) to choose a
project source. Integrations require a Trello key and token, board and three list IDs,
a Telegram bot token, and a private chat ID. Secrets and local configurations stay out of Git.

An executor can change only its working copy and scratch files. A reviewer reads
the working copy and review materials. GitHub credentials and publication remain
with the trusted controller. Approve and merge are not exposed in either backend.
Global user settings, plugins, and additional agents are not loaded into tasks.

Setup installs persistent IPv4/IPv6 WFP filters for the SID of `CodexSandboxOffline`,
blocking Internet, LAN, and localhost connections. The rules affect every process
under that account, including other offline Codex sessions. The controller, model
client, and ordinary Windows accounts retain their own network access. Changes to
the sandbox account require administrator review of the policy. The system does
not replace filters whose contents differ from the expected rules.

Each task process tree is placed in a Windows Job Object with a 2 GB memory limit,
256-process limit, and 25% of the machine's CPU. Closing the controller terminates
the tree. The process starts suspended and resumes after these limits are attached.

`Check-NativeIsolation.ps1` checks the actual offline account, executor writes,
reviewer read-only access, denial of a sentinel outside the task, Internet blocking,
and localhost blocking. Startup is refused unless the checks pass. The receipt is
bound to the Codex CLI executable hash; repeat the checks after updating it.

This backend provides weaker isolation than a separate VM or container. Windows
and installed tools remain readable. Permission profiles deny known unrelated
top-level directories, while NTFS protects private data. This does not isolate the
kernel or guarantee that new directories created during a task are blocked. Do not
store secrets in publicly readable tool directories. Probes passed on the development
Windows Server, but that does not establish official Codex support for every Windows
Server configuration.

Installation does not start card processing. After configuring access, run an
end-to-end check with one card, a PR or local review, a Telegram Reply, and an automatic
fix. Enable automatic startup only afterward. Prefer Docker on a host with available
virtualization when stricter isolation is required.
