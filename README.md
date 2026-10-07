# Codex Orchestrator

**English** | [Русский](docs/ru/README.md)

A general-purpose, isolated environment for handling Trello tasks with Codex CLI.
Use local projects with local branches and reviews, or GitHub repositories with pull
requests. Choose the project, base branch, account, models, and integrations during
setup. The account that owns this orchestrator does not determine the project account.

Start with `scripts/Configure-Project.ps1` and the [project setup guide](PROJECT-SETUP.md).
Choose Docker or the [restricted Windows installation](NATIVE-INSTALL.md).
Configure integrations and verify isolation before starting task processing.

## Task reference materials

Before each executor or reviewer run, the trusted service downloads files attached
to the Trello card and HTTPS links in its description and supplied answer. Trello
download authentication stays on the host; agents receive only local files and a
manifest. Open local pictures with the image-viewing tool. Failed downloads are
listed in the manifest so the agent can ask about the specific missing resource.
Public HTTPS links are supported; other private services need separately supplied
files. Downloads reject private-network destinations and credential forwarding.
Limits are 32 files, 16 MiB per file, and 64 MiB of attachments per task.

The same read-only context includes `branches.git`, an offline repository containing
the selected project's branches and their history, plus `branches.txt` with commit
IDs. Inspect other branches with Git `show`, `log`, or `diff`, then apply task-relevant
changes in the task workspace. The reference repository has no remote or credentials,
is protected from writes, and is excluded from published changes. Publication remains
limited to the task's own `codex/<card-id>` branch. The branch bundle limit is 512 MiB.

In Docker these materials are mounted read-only at `/review-input/context`. On
Windows they are read-only under the workspace's `.orchestrator-review/context`.

## Workflow

1. People manually add cards to the dedicated board's **To Do** list.
2. Once an hour, the manager selects up to two independent tasks.
   A card title can be the complete specification; an empty description is not a blocker.
3. The controller moves cards to **In Progress**. At most two executors run at once.
4. Each executor works in a new `codex/<card-id>` branch.
5. The trusted Git broker commits the changes and publishes only the task branch.
   GitHub mode creates a PR targeting the configured base branch; local mode saves a
   local branch and creates a local review record. The card stays **In Progress**.
6. A reviewer checks the exact commit SHA using a read-only working copy. Findings
   become PR comments or local review files. **Reviewers cannot submit GitHub Approve
   or merge:** the controller exposes neither operation, and agents have no GitHub credentials.
7. Findings return to the original executor's session, branch, and review.
   Automatic fixes continue for up to three rounds by default. A human reply is
   required to authorize another round.
8. Telegram sends completion notifications only after the internal reviewer finds
   no issues. Clarification questions and requests for missing resources are sent immediately.
   Only a successful review of the current commit moves the card to **Done**.
   A new unreviewed revision returns it to **In Progress**. People archive cards manually.

Default models are `gpt-6.1-sol / low` for executors, `gpt-6-sol / medium` as the
fallback, and `gpt-6-luna / low` for managers and reviewers. These defaults are
configurable. Fallback is used only when the primary model is explicitly unavailable,
never to bypass subscription limits. A rate limit delays the next attempt by an hour.

Open PRs targeting the configured base branch are also discovered and reviewed.
External PRs receive comments; automatic fixes apply only to system-created tasks.
A human update to a system-owned branch stops automatic publication and requests a
decision. Closed PRs receive no further runs.

## Docker isolation

Agent working copies stay inside Docker; the host project directory is not mounted.
Each task has named volumes for its working copy, private publisher repository, review
materials, and each role's Codex state. With WSL, volumes reside on the Docker/WSL
virtual disk. Moving storage to another physical disk requires moving the Docker data
root or WSL disk separately.

Agents run as UID 1000 with capabilities dropped, `no-new-privileges`, a read-only
root filesystem, and RAM/CPU/process limits. They receive no Docker socket, host bind
mounts, integration tokens, or remote Git configuration. Reviewer and manager working
copies are mounted read-only. Containers join only the Docker internal network.

The sole outbound route is a CONNECT proxy allowing exactly `chatgpt.com:443`,
`api.openai.com:443`, and `auth.openai.com:443`. It rejects private addresses, other
ports, redirects, and other domains. Firewall rules block Linux host services and
neighboring agents. External DNS is disabled; the proxy address is explicit.
The controller checks the rules before each run. Reapply `scripts/network-policy.sh`
after restarting Docker. Prepare dependencies in the image; an agent asks a human
when dependencies, data, or access are missing.

Codex CLI uses `danger-full-access` **only inside these containers**, where Docker,
read-only mounts, and network policy provide the boundary. Host-native execution does
not use that setting. Agents can access their selected ChatGPT credentials to reach
the model. Apps, plugins, hooks, browser tools, and built-in delegation are disabled.
Integration credentials stay with the trusted controller and Git broker.
Containers share the Linux kernel and do not replace a separate VM against kernel exploits.

The trusted Git broker never executes project code, hooks, or agent-supplied commands.
It ignores the agent's `.git`, copies ordinary files into its own repository, and
makes fast-forward pushes to `codex/...` only. Base-branch writes, force pushes,
Approve, and merge are not exposed. Existing tracked relative symlinks are preserved
only when their complete target chain stays inside the working copy and does not
reference Git metadata. New or modified links, submodules, and `.github/workflows`
changes require separate human preparation. The broker receives the GitHub token
over stdin, not through command arguments, persistent volumes, or the agent environment.

## Required access

- **GitHub mode:** GitHub CLI authentication for the selected project account, with
  repository read/write access and permission to create PRs and comments. Setup
  verifies the account and access. The controller explicitly selects
  `gh auth token --user <github_user>`, regardless of the active CLI account.
  Local mode does not use GitHub.
- **Trello:** an API key, a read/write token for the dedicated board, its board ID,
  and distinct list IDs for To Do, In Progress, and Done.
- **Telegram:** a dedicated bot token and your private chat ID after sending `/start`.
  The service uses `getUpdates`; do not share the bot with another poller or webhook.
- **Codex:** the selected ChatGPT login in `secrets/auth.json`. API-key authentication
  is not accepted. Prepare a separate `CODEX_HOME` with `codex login --device-auth`;
  do not copy global configuration, hooks, MCP servers, or skills.
  Refresh expired authentication manually.

Save integration secrets in `secrets/integrations.json` using `secrets.example.json`
and restrict access with filesystem permissions. Secrets, local configurations,
state, `.env`, and binaries are excluded from Git. Telegram replies are clarification
text, not a way to supply secrets or relax isolation.

## Windows with WSL2 and Docker

Docker Desktop is not supported on Windows Server. This installation uses Docker
Engine inside Ubuntu WSL2 and runs the controller on Windows through
`wsl.exe --exec docker`. GitHub credentials remain in the Windows credential store.

1. Enable nested virtualization at the VM host if Windows itself runs in a VM.
2. Run `wsl --install --distribution Ubuntu-24.04 --no-launch`.
3. Start Ubuntu and run `scripts/install-docker-ubuntu.sh` as root. It uses Docker's
   official APT repository and does not change Windows virtualization settings.
4. Run `scripts/Configure-Project.ps1` and `scripts/Setup.ps1 -Config config.json`.
   Setup builds images, the internal network, and the proxy.
5. Configure `config.json`, `secrets/integrations.json`, and `secrets/auth.json`.
   Set the absolute `gh.exe` path if the current process's PATH has not refreshed.
6. Run `scripts/check-isolation.sh` in Linux and verify the reviewer's read-only mount.
7. Run `scripts/Start-Orchestrator.ps1 -Config config.json -Check`, then run it without `-Check`.
8. Verify one test card, PR, Telegram Reply, automatic fix cycle, and completion
   notification after a successful internal review.
9. Only after that end-to-end check, configure Task Scheduler under the account that
   owns the selected credentials. These scripts do not install automatic startup.

On a regular Linux host, set `docker_distro` to `""`, configure `gh` and the chosen
account, build with `go build -o bin/orchestrator ./cmd/orchestrator`, and build/start
the Docker images and Compose services. Supply integration environment variables to
the controller. The controller code is portable; the interactive PowerShell wizard
is the Windows setup path.

## Operation

Use **Reply on the question message**. Its message ID maps to the requesting agent's
thread ID, so simultaneous questions stay separate. `/status` lists task states.
Send `/check` to request an immediate board check without waiting for the hourly scan.
It preserves the hourly schedule and executor limits. If the manager is busy or waiting
for clarification, the request stays queued until that manager run finishes.
Other messages without Reply do not start work or change permissions. New runs pause
while Telegram is unavailable.

Agents resolve routine implementation choices autonomously using the task and local
code. They ask only about genuine blockers and combine related questions. Replies
are saved with their questions and supplied on subsequent turns in the same session.
An identical answered agent question is retried once with that context instead of
being immediately sent again. Setup failures are labeled **System blocker**, so they
are not mistaken for questions from an executor that has not started.

State is saved atomically in `state_dir/state.json`. Interrupted jobs pause after a
crash and require a human reply; abandoned containers are stopped.
`state_dir/controller.lock` prevents a second controller for that state directory.
Remove a stale lock only after confirming the previous process has exited.
Named volumes are retained for recovery; automatic cleanup is not enabled.
The operator sets retention and cleanup rules.

A crash between sending a Telegram notification and recording it may cause a
duplicate, because `sendMessage` has no idempotency key. Completion notifications
apply only to the current head SHA. PR creation recovers using the exact task branch.

English is the default for documentation, system prompts, and operator messages.
Agents use English for summaries, questions, and findings unless a task explicitly
requests another language. Task descriptions and project content may use any language.

## Development checks

Run `go test ./...` and `go vet ./...`. Checks cover Reply routing, executor concurrency,
reviewer capability restrictions, stale SHAs, rate limits, recovery, local publication
and review, project/account separation, agent Git isolation, and proxy rules.
They do not replace isolation probes or end-to-end tests with real integrations.

Reference documentation: [Codex non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode),
[Codex authentication](https://learn.chatgpt.com/docs/auth),
[Docker Engine on Ubuntu](https://docs.docker.com/engine/install/ubuntu/), and
[Docker Desktop Windows requirements](https://docs.docker.com/desktop/setup/install/windows-install/).

## Installation without Docker or a VM

See [NATIVE-INSTALL.md](NATIVE-INSTALL.md) for the restricted Windows backend,
its isolation checks, and its limitations.
