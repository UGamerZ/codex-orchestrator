# Install Codex Orchestrator

[Русская инструкция](docs/ru/INSTALL.md)

Download an archive from [GitHub Releases](https://github.com/UGamerZ/codex-orchestrator/releases/latest), extract it to a **new, private directory**, and use the installer inside. The release archives contain the compiled controller; **Go and a Git clone of this project are not required**. Keep the extracted folder: it becomes your installation, including configuration and task history.

| Download | Host | Start setup |
| --- | --- | --- |
| `*-windows-amd64.zip` | 64-bit Intel/AMD Windows | Double-click `Install.cmd` |
| `*-linux-amd64.tar.gz` | Intel/AMD Linux | `sh install.sh` |
| `*-linux-arm64.tar.gz` | ARM64 Linux | `sh install.sh` |

Choose the named release archive, rather than GitHub's automatically generated **Source code** download, for a prebuilt controller. Packages are unsigned; SHA-256 checksums are supplied in `SHA256SUMS.txt`.

## Before setup

You need Git, a Codex **ChatGPT** login, Trello API credentials, and your own Telegram bot from **@BotFather**. GitHub mode also needs GitHub CLI and an account with write access to your project. Local mode can keep reviewed branches locally. The Windows wizard also uses GitHub CLI when offering GitHub publication.

- Install Git: <https://git-scm.com/downloads>
- Install GitHub CLI: <https://cli.github.com/>
- Install and sign in to Codex CLI: <https://developers.openai.com/codex/cli/>
- Create a Trello API key and a token with read/write access: <https://trello.com/power-ups/admin>
- Create a board with **To Do**, **In Progress**, and **Done** lists. The wizard lists your boards and lists; you do not need to look up their IDs.
- Open a private chat with your Telegram bot and send `/start` when the wizard asks.

Credentials are entered with hidden prompts and saved locally. They are never part of the release archive. Use a dedicated bot: another process or Telegram webhook must not consume its updates.

## Windows

1. Extract the Windows ZIP somewhere you can write, for example `C:\Users\YOUR_NAME\Apps\CodexOrchestrator`. Do not extract over an existing installation or put it in a shared/public folder.
2. Double-click **Install.cmd**. Choose **Windows sandbox** or **Docker inside Ubuntu WSL**. Missing dependencies produce an actionable message; reopen the installer after installing them.
3. Choose your project, base branch and project GitHub account, or create a local project. Each project can use its own GitHub account. An existing configuration is kept.
4. Enter Trello and Telegram credentials, select the board and lists by number, and point to your maintained Codex `auth.json`. The wizard checks credentials and discovers the private Telegram chat.
5. Wait for backend preparation and isolation/integration checks. **Setup does not start agents.**
6. When ready, double-click **Start.cmd**. Leave that window running. Open **http://127.0.0.1:8787** for the dashboard. Stop the controller with `Ctrl+C` in its window.

Native Windows setup requests Administrator access through Windows UAC. Run it as the Windows account that owns your Codex login. It installs machine-level sandbox network restrictions and stores task runtimes under the configured private `C:\ProgramData\CodexOrchestrator\instances` directory. Each task has separate project and agent state, with up to two executors; the Windows sandbox uses a shared OS identity. See [native limitations](NATIVE-INSTALL.md).

For Docker on Windows, first prepare Ubuntu WSL with a running **Linux Docker Engine**, as described in [README](README.md#docker-installation-notes). The wizard asks for the distribution name. This path does not configure Docker Desktop automatically.

For startup recovery after you have tested a real task, optionally run `scripts/Install-Watchdog.ps1 -Config config.native.json` from an Administrator PowerShell. For a Docker setup, use `-Config config.json`. Automatic startup is not enabled by the installation wizard.

## Linux with Docker

Use a Linux host with Docker Engine, Docker Compose v2, Git, GitHub CLI, **Python 3.9+**, and access to `iptables`. Your user must be able to use Docker, and must have `sudo` access for firewall setup. Do not run the whole wizard under a different user: GitHub and Codex login files belong to the account running the controller.

```sh
tar -xzf codex-orchestrator-vVERSION-linux-amd64.tar.gz
cd codex-orchestrator-vVERSION-linux-amd64
sh install.sh
```

Replace `vVERSION` with the downloaded version, and use `arm64` for an ARM host. The wizard supports an existing GitHub repository, a new local project, or committed history from an existing local Git project. In GitHub mode it authenticates the selected project account and checks repository/base-branch access. The Linux local mode keeps branches locally; GitHub repository creation is available through the dashboard after starting.

The first installation builds Docker worker/broker images and downloads their dependencies. It then installs rules for the dedicated agent network, checks isolation, and validates integrations. It never processes cards during setup.

Start manually when ready:

```sh
sh start.sh
```

Keep the process running, or supervise that command with your own service manager. Stop it with `Ctrl+C`. Reapply `scripts/network-policy.sh` with administrator privileges after restarting Docker, then run `sh start.sh --check` before processing tasks. The wizard does not install a Linux service or modify system startup.

The dashboard listens on loopback. For a remote host, forward it rather than exposing it publicly:

```sh
ssh -L 8787:127.0.0.1:8787 USER@SERVER
```

Then open http://127.0.0.1:8787 on your computer.

## Verify a download

Download `SHA256SUMS.txt` from the same release. On Windows, run `Get-FileHash .\DOWNLOADED.zip -Algorithm SHA256` and compare the value with its line in that file. On Linux, from the directory containing the archives and checksums, run `sha256sum --ignore-missing -c SHA256SUMS.txt`.

## Retry setup or update

If setup stops, read the error, fix the missing prerequisite or credential, and rerun it. Existing configuration can be retained. To change integrations, run `scripts/Configure-Integrations.ps1` on Windows and answer `n` when asked to keep existing credentials.

Release archives are **fresh-install packages**, not unattended upgrades. For an existing deployment, stop its controller and watchdog, back up its configuration/secrets/state, review the release notes, and plan the update. Never run two controllers against the same state, bot or task queue. Installing a new package does not import previous task history automatically.

The native backend and Docker backend preserve the existing publication restrictions: agents cannot approve GitHub PRs, merge them, force-push, or publish to the base branch. Live public web research is available; direct command networking remains restricted.
