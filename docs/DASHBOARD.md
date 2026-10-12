# Operator dashboard

Open **http://127.0.0.1:8787** in a browser on the remote machine.
The controller serves the dashboard by default. `-web ""` disables it; `-web 127.0.0.1:PORT` changes its port.

- **Tasks:** status, blocker, reviewer findings, current commit, PR link, retry schedule, answers, and manual retry.
- **Agents:** running role, elapsed time, current session, last session activity, and last tool/event kind. Native telemetry never displays arguments, tool output, or raw model conversations. Docker agents currently expose lifecycle events, not native session telemetry.
- **Environments:** add independent GitHub or local projects, select a GitHub account, sign in another account, and switch the active environment when agents are idle.
- **Configuration:** models, reasoning effort, executor concurrency (maximum two), retry/fix limits, run deadline, manager interval, and branch continuation. Changes are validated and applied only while workers are idle. A pending change survives controller restarts.
- **Events:** the latest 200 starts, results, blockers, controller issues, and operator actions. Events persist across restarts.
- **Check board:** request an immediate To Do check.
- **Pause scheduling:** finish active work but stop starting new agent and publisher runs. Resume starts scheduling again.

An agent question requires an answer; an empty retry cannot bypass it. Workflow permission can be supplied by answering its specific question in the panel. SQL guards, branch restrictions, and review requirements still apply. Repository profiles and GitHub sign-in are managed in Environments. Integration credentials and isolation configuration remain in the setup scripts/files.

The dashboard binds only to loopback and uses a random session cookie, request token, origin checks, and a restrictive content policy. It is intended for the machine's trusted local operator. Use your RDP browser or a private SSH tunnel to reach it; do not expose the port through a public proxy without adding authenticated access.

## Notifications and Windows watchdog

Task questions and blockers go to Telegram immediately when detected. Waiting tasks receive hourly reminders. Agent errors and controller/API errors are reported with per-issue notification throttling. When Telegram is unavailable, the event history retains that fact; Telegram cannot deliver messages during its own outage.

Install the Windows watchdog:

```powershell
.\scripts\Install-Watchdog.ps1 -Config config.native.json
```

It checks the process once a minute, reports a stale state after five minutes, and restarts a stopped controller. It does not kill a live process or repeat an uncertain publication. The task runs as the installing Windows user while that user is logged in (including disconnected RDP sessions). For unattended operation after logout/reboot, configure a dedicated account and suitable Task Scheduler logon credentials separately; do not switch GitHub operations to SYSTEM.

Disable it before intentionally stopping the controller:

```powershell
Disable-ScheduledTask -TaskName CodexOrchestratorWatchdog
```

Notification throttling and restart logs are stored in the private state directory. Native agent cleanup and bounded interruption recovery run before tasks resume.
## Agent log viewer

Open **Agents → View logs** for an active agent, or expand **Task agent logs** to select an executor/reviewer session from a completed task. The viewer refreshes with the page and shows recent tool calls, outputs, errors, and public progress/final messages. It reads at most the latest 1 MiB and displays at most 300 entries, with each entry limited to 8,000 characters. Private reasoning, input prompts, and credential-related entries are excluded. Known integration/Codex credentials and common token patterns are redacted; treat remaining project output as private operational data. Session ID and workspace must both match the selected task. Detailed session logs currently support the native Windows backend; Docker uses controller events.

## Cards outside managed lists

Moving a tracked card outside the configured To Do, In Progress, and Done lists (for example, to Merged), or archiving it, permanently completes that task for automation. The controller checks card location before PR discovery and scheduling. PR updates, merges, completion-message replies, dashboard retries, controller restarts, and late agent results cannot reopen it. Publication rechecks the card location before proceeding. Work already in flight may finish, but its result will not schedule another review or publication once completion is detected; an already-started Git push cannot be undone atomically with a Trello move. Create a new card for new work. A failed Trello read pauses new scheduling rather than guessing the card location.

## Multiple environments and GitHub accounts

Open **Environments** to create a profile with a name, project source, base branch, and Trello board/list IDs. For GitHub, enter `owner/repository` and the login of an account with access. Use **Sign in another GitHub account**, open the device authorization link, and enter the displayed one-time code. Complete authorization in the browser while signed in to the intended account. The CLI stores credentials on the controller machine; the dashboard shows account names and authorization status only. Project operations always select the profile's account explicitly, regardless of GitHub CLI's globally active account.

For a local project, select an existing Git repository directory. Only committed history is imported into a private bare repository; the source directory and uncommitted changes remain untouched. Tasks create local branches and local review results. This mode does not create GitHub PRs.

Creation does not activate a profile. Click **Switch environment** to validate repository/account access, base branch, Trello lists, and integrations before activation. Native Windows profiles also pass isolation probes before first activation. A failed check leaves the current environment active. Switching and GitHub sign-in require idle workers; pause scheduling first if necessary, then wait for running agents/publication to finish. Scheduling is held during device sign-in. Inactive environments retain their queued/waiting tasks but do not run agents or poll their board/PRs. Resume scheduling after switching if the selected profile is paused.

Environments can share the same Trello board and all three lists. Only the selected environment processes its configured board; switching preserves independent task state for each profile. Trello and Telegram credentials use the installation's existing host setup; the Telegram chat is shared. Replies to questions and completion notifications are retained in their originating inactive profile and resume when it is selected. Task statuses, configuration edits, logs, branches, and model sessions remain separate. Telegram's update offset carries forward to avoid replaying old messages when switching.

`environments.json` contains the private profile registry next to the active configuration file. New profiles store state and local repositories under `environment-state/<profile-id>`; native workspaces and sessions are under `<original-native-root>/environments/<profile-id>`. The current project keeps its existing directories and tasks. The active configuration file is updated on switching, so the normal launch script and watchdog reopen the selected environment after restart. Keep the registry and all profile state directories in backups, exclude them from Git, and continue using the same configuration path. Do not run a second controller for another profile: `environment-controller.lock` protects the whole installation, alongside each state's `controller.lock`. Remove stale locks only after verifying the owning process has stopped. The watchdog handles stopped-controller locks, and does not terminate live controllers or workers.
Use **View settings** on any saved environment, including inactive profiles, to inspect its repository or local path, GitHub account, Trello lists, models, execution backend, and limits. Viewing settings does not switch environments or start agents. The expanded profile remains open during automatic refresh; use **Hide settings** to close it. Credentials are excluded.

## Edit saved environments

Click **Edit environment** on a profile to open its prefilled form. You can change the name, project source, repository/local path, base branch, GitHub account, Trello board/lists, models, reasoning efforts, concurrency, fix limit, run timeout, manager interval, and task-branch continuation. **Save environment** validates and saves the changes; **Cancel edit** discards the draft. Automatic refresh retains unsaved form input. Integration credentials, backend isolation, and private working-directory locations remain controlled by installation setup.

Name/model/limit edits preserve the existing tasks and sessions. Changing the repository, local source, base branch, account, or Trello board/lists creates a fresh working setup under the same profile ID. The previous setup is retained as a separate selectable profile named `Original name (previous …)`, with its original configuration, tasks, files, and sessions. No project files or task history are deleted. Existing local profiles show their private bare Git store as the local path; leave it unchanged to keep that project, or select another existing local Git directory to import its committed history into the fresh setup.

The active environment can be edited only after agents/publication are idle and pending configuration or GitHub sign-in has finished. Pause scheduling first if necessary. Changes to its project or Trello routing pass access and isolation checks before activation; a failed check keeps the original active setup and configuration. Inactive profiles are saved without switching and are checked when selected. Previously saved setups remain inactive until you switch to them. A queued edit or its failure appears in Events.
PR discovery restores Trello card links from the system task branch or the PR description before scheduling review, including full card IDs and verified short links. Cards already outside managed lists are recorded as completed even when the current environment has no original task record. PRs owned by an inactive saved environment do not create duplicate reviews in the selected environment. Late worker results recheck card location before reporting task errors. A failed card lookup holds scheduling instead of treating the PR as an unlinked task. Reviewer role restrictions remain enforced.
