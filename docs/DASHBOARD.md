# Operator dashboard

Open **http://127.0.0.1:8787** in a browser on the remote machine.
The controller serves the dashboard by default. `-web ""` disables it; `-web 127.0.0.1:PORT` changes its port.

- **Tasks:** status, blocker, reviewer findings, current commit, PR link, retry schedule, answers, and manual retry.
- **Agents:** running role, elapsed time, current session, last session activity, and last tool/event kind. Native telemetry never displays arguments, tool output, or raw model conversations. Docker agents currently expose lifecycle events, not native session telemetry.
- **Configuration:** models, reasoning effort, executor concurrency (maximum two), retry/fix limits, run deadline, manager interval, and branch continuation. Changes are validated and applied only while workers are idle. A pending change survives controller restarts.
- **Events:** the latest 200 starts, results, blockers, controller issues, and operator actions. Events persist across restarts.
- **Check board:** request an immediate To Do check.
- **Pause scheduling:** finish active work but stop starting new agent and publisher runs. Resume starts scheduling again.

An agent question requires an answer; an empty retry cannot bypass it. Workflow permission can be supplied by answering its specific question in the panel. SQL guards, branch restrictions, and review requirements still apply. Repository setup, integration credentials, and isolation configuration remain in the setup scripts/files and cannot be edited through the browser.

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
