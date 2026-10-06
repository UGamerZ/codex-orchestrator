# Project setup

**English (default)** | [Русский](docs/ru/PROJECT-SETUP.md)

The system is not tied to its source-code owner, a project, or a GitHub account.
Each installation selects its project source, base branch, account, models, and
integrations through local configuration. The wizard does not start agents.

```powershell
.\scripts\Configure-Project.ps1
```

Choose Docker or restricted Windows execution, then an existing GitHub repository
or a new local project. Setup and Start also launch the wizard when configuration
is missing. Replacing an existing configuration requires typing `REPLACE`.
Use separate configuration files, state directories, working directories, boards,
and Telegram bots for separate projects.

## Existing GitHub repository

```powershell
.\scripts\Configure-Project.ps1 -Mode github -Backend docker `
  -Config config.team.json -Repository OWNER/REPOSITORY `
  -GitHubUser LOGIN -BaseBranch develop
```

If GitHub CLI has no credentials for the selected account, the wizard starts browser
device authorization. Sign in as that account. Setup verifies the actual login,
repository write access, and the existence of the base branch. The active CLI account
and token environment variables do not determine the working identity.
GitHub CLI stores the token; it is not written to project configuration.
The controller repeats these access checks before agents start.

The wizard does not clone an existing repository. The trusted broker fetches the
project when tasks run. Commits go only to `codex/...` branches, and PRs target the
selected base branch. **Done** means a PR has been created. Reviewers publish comments,
not GitHub Approve or merge.

## New local project

```powershell
.\scripts\Configure-Project.ps1 -Mode local -Backend native-windows `
  -Config config.local.json -LocalPath C:\Projects\new-project `
  -BaseBranch main -LocalOnly
```

Choose a new empty directory. The wizard creates a minimal project, its initial
commit, and a separate bare Git store at `.orchestrator.git`. Existing files are never
automatically staged. Without `-LocalOnly`, the wizard offers to create a private
GitHub repository instead. Select that explicitly with `-Publish`:

```powershell
.\scripts\Configure-Project.ps1 -Mode local -Backend docker -Publish `
  -Config config.new.json -LocalPath C:\Projects\another-project `
  -Repository OWNER/NEW-REPOSITORY -GitHubUser LOGIN -BaseBranch main
```

Fully local mode uses no GitHub CLI or GitHub authentication. Source commits reach
the broker as Git bundles, without mounting the host project directory into a
container. Results return to the bare store as local branches. Both isolation
backends support this workflow. Reviews are saved under `state_dir/reviews`: JSON
task descriptions and Markdown findings for the specific commit SHA.

In local mode, **Done** means the task branch has been saved and queued for local
review. Telegram sends completion notifications only after review finds no issues.

Inspect and accept a result manually from the local project directory:

```powershell
git --git-dir=.orchestrator.git branch --list
git fetch .orchestrator.git codex/TASK-ID
git diff main...FETCH_HEAD
git merge FETCH_HEAD
```

Replace `main` with your configured base branch where appropriate. The system never
changes that branch or merges results. After accepting changes manually, synchronize
the store's base branch so new tasks receive the updated source:

```powershell
git --git-dir=.orchestrator.git fetch . main:main
```

A human update to a task branch stops automatic fixes. State from one project cannot
be reused for another project, account, or base branch. Docker containers and volumes
have separate names for each project instance.

## Complete installation

Configure Trello, Telegram, and Codex authentication. Run Setup for the selected
isolation backend, then Start with the same configuration. For native Windows:

```powershell
.\scripts\Setup-Native.ps1 -Config config.local.json
.\scripts\Start-Orchestrator.ps1 -Config config.local.json -Check
.\scripts\Start-Orchestrator.ps1 -Config config.local.json
```

For Docker on Windows with WSL2, use `Setup.ps1 -Config YOUR-CONFIG.json` and follow
the [Docker installation instructions](README.md#windows-with-wsl2-and-docker).
For native execution, review [the native isolation limitations](NATIVE-INSTALL.md).

Models and reasoning effort are configured per installation. Template values are
defaults, not a fixed list of permitted models. The maximum of two executors and the
Approve/merge prohibition remain safety constraints.

After starting the controller, send `/check` to the Telegram bot for an immediate
board check, or `/status` to inspect task states. Manual checks preserve the hourly
schedule and do not start another manager while one is already active.

English is the default language for documentation, system prompts, and operator
messages. Agents use English for output unless a task explicitly requests another
language. Task descriptions and project content do not need to be in English.
