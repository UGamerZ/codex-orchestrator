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
Native review inputs are refreshed by the trusted broker under the read-only working
copy's `.orchestrator-review` directory. They are excluded from Git publication.
Isolation probes verify both reading these inputs and refusing reviewer writes.

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
executor read-only access to task attachments and reference branches,
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

For a long-running installation, set codex_auth_file to the absolute auth.json
path of the host's maintained Codex ChatGPT login (normally
C:\Users\<service-user>\.codex\auth.json, or the login under your configured
CODEX_HOME). A one-time copy in secrets/auth.json can expire even while the
host login remains valid. The controller reads this explicit source before each
agent run and retains a more recently refreshed runtime login for the same
account. It never copies runtime credentials back into the host login.
Credentials remain inaccessible to agent tools. A 401 authorization failure is
reported with its filtered cause; renew the configured login and retry the task.

## Separate task runtimes

Every native task/role uses its own private Codex home at
native_root/jobs/<task-id>/private/<role>-runtime/codex. New tasks do not inherit
another task's conversation history, SQLite databases, memories, caches, or scratch
files. Their project files come from the configured base-branch snapshot. Fixes
and retries continue only that task's own workspace and role session.

Only the pre-provisioned Windows sandbox account metadata is seeded into a new
runtime. Global user configuration, skills, session databases, and capability
state are not copied. Existing tasks may migrate the exact known rollout file
after both its session ID and workspace have been verified. Old logs remain
available to the dashboard without becoming another task's runtime state.

Isolation receipt version 4 verifies task-local runtimes, reviewer read-only
access, simultaneous use of separate workspaces, and blocked internet and
loopback connections. Checks work with PowerShell ConstrainedLanguage enabled.

Native Windows retains the configured concurrency of up to two executors.
Each task has its own working copy and runtime state, but native sandboxes use
a shared Windows account. Separate directories do not provide an OS security
boundary between mutually untrusted agents. Use Docker or VMs when that boundary
is required. The controller does not switch global task ACLs between runs.
