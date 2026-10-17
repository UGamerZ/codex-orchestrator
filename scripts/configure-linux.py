#!/usr/bin/env python3
"""Interactive Docker configuration; secrets never enter command arguments."""
import getpass
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent.parent


def ask(prompt, default=""):
    value = input(f"{prompt}" + (f" [{default}]" if default else "") + ": ").strip()
    return value or default


def checked(args, env=None):
    result = subprocess.run(args, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode:
        raise RuntimeError(f"{args[0]} failed. Check the selected account, repository and prerequisites.")
    return result.stdout.strip()


def api(url):
    try:
        with urllib.request.urlopen(url, timeout=30) as response:
            return json.load(response)
    except Exception:
        raise RuntimeError("Integration request failed. Check credentials and internet access.") from None


def choose(items, prompt):
    if not items:
        raise RuntimeError("No available items: " + prompt)
    for index, item in enumerate(items, 1):
        print(f"{index}. {item['name']}")
    while True:
        value = ask(prompt)
        if value.isdigit() and 1 <= int(value) <= len(items):
            return items[int(value) - 1]
        print("Enter one of the displayed numbers.")


def save_private(path, data):
    if path.is_symlink() or path.parent.is_symlink():
        raise RuntimeError("Configuration paths must not be symbolic links.")
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent, delete=False) as output:
        temporary = Path(output.name)
        json.dump(data, output, indent=2)
        output.write("\n")
    try:
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def main():
    os.umask(0o077)
    config_path = ROOT / "config.json"
    if config_path.exists() and ask("Configuration exists. Keep it?", "Y").lower() != "n":
        existing = json.loads(config_path.read_text(encoding="utf-8-sig"))
        if sys.platform == "darwin" and (existing.get("docker_vm") != "colima" or existing.get("docker_distro")):
            raise RuntimeError("This macOS installer requires docker_vm=colima and no WSL distribution. Keep a backup and reconfigure this installation.")
        print("Existing configuration and credentials retained.")
        return
    config = json.loads((ROOT / "config.example.json").read_text(encoding="utf-8-sig"))
    config.update(backend="docker", docker_distro="", gh_path=shutil.which("gh") or "gh", state_dir=str(ROOT / "state"))
    if sys.platform == "darwin":
        config["docker_vm"] = "colima"
    config["base_branch"] = ask("Base branch", "main")
    checked(["git", "check-ref-format", "--branch", config["base_branch"]])
    mode = ask("Project: 1 existing GitHub repository, 2 local Git project", "1")
    if mode not in ("1", "2"):
        raise RuntimeError("Choose project type 1 or 2.")
    config["project_mode"] = "github" if mode == "1" else "local"
    if mode == "1":
        if not shutil.which("gh"):
            raise RuntimeError("Install GitHub CLI from https://cli.github.com/ first, then rerun setup.")
        login = ask("GitHub account with write access")
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9-]*", login):
            raise RuntimeError("Invalid GitHub account name.")
        try:
            token = checked(["gh", "auth", "token", "--hostname", "github.com", "--user", login])
        except RuntimeError:
            print("Sign in with the selected project account using GitHub's device login.")
            subprocess.run(["gh", "auth", "login", "--hostname", "github.com", "--git-protocol", "https", "--web"], check=True)
            token = checked(["gh", "auth", "token", "--hostname", "github.com", "--user", login])
        env = {k: v for k, v in os.environ.items() if k not in ("GH_TOKEN", "GITHUB_TOKEN", "GH_HOST")}
        env.update(GH_TOKEN=token, GH_HOST="github.com")
        if checked(["gh", "api", "user", "--jq", ".login"], env).lower() != login.lower():
            raise RuntimeError("The authenticated account does not match the selected account.")
        repository = ask("GitHub repository OWNER/NAME").removeprefix("https://github.com/").removesuffix(".git").rstrip("/")
        if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
            raise RuntimeError("Invalid repository name.")
        repo = json.loads(checked(["gh", "api", "repos/" + repository], env))
        if repo.get("archived") or not repo.get("permissions", {}).get("push"):
            raise RuntimeError("Write access to an active repository is required.")
        checked(["gh", "api", "repos/" + repository + "/branches/" + urllib.parse.quote(config["base_branch"], safe="")], env)
        config.update(repository=repository, github_user=login)
    else:
        project = Path(ask("Local project directory", str(ROOT / "projects" / "new-project"))).expanduser().resolve()
        project.mkdir(parents=True, exist_ok=True)
        if not any(project.iterdir()):
            checked(["git", "init", "--initial-branch", config["base_branch"], str(project)])
            (project / "README.md").write_text("# New project\n", encoding="utf-8")
            checked(["git", "-C", str(project), "add", "--", "README.md"])
            checked(["git", "-C", str(project), "-c", "user.name=Project Owner", "-c", "user.email=owner@localhost", "commit", "-m", "Initialize project"])
        checked(["git", "-C", str(project), "rev-parse", "--verify", "refs/heads/" + config["base_branch"]])
        store = ROOT / "local-project.git"
        if store.exists():
            raise RuntimeError("local-project.git already exists; keep the existing configuration or use a fresh installation directory.")
        checked(["git", "clone", "--bare", "--no-hardlinks", str(project), str(store)])
        config.update(repository="", github_user="", local_repository=str(store))
        print("Local task branches will be published to:", store)
    print("Trello API credentials: https://trello.com/power-ups/admin")
    key = getpass.getpass("Trello API key (hidden): ").strip()
    token = getpass.getpass("Trello token (hidden): ").strip()
    if not key or not token:
        raise RuntimeError("Trello key and token are required.")
    query = urllib.parse.urlencode({"key": key, "token": token})
    board = choose(api("https://api.trello.com/1/members/me/boards?filter=open&fields=name&" + query), "Select board")
    lists = api("https://api.trello.com/1/boards/" + board["id"] + "/lists?filter=open&fields=name&" + query)
    todo = choose(lists, "Select To Do")
    progress = choose([x for x in lists if x["id"] != todo["id"]], "Select In Progress")
    done = choose([x for x in lists if x["id"] not in (todo["id"], progress["id"])], "Select Done")
    telegram = getpass.getpass("Telegram bot token from @BotFather (hidden): ").strip()
    bot_url = "https://api.telegram.org/bot" + telegram
    bot = api(bot_url + "/getMe")
    if not bot.get("ok"):
        raise RuntimeError("Telegram did not accept the bot token.")
    print("Open https://t.me/" + bot["result"]["username"] + " and send /start.")
    input("Press Enter after sending /start: ")
    updates = api(bot_url + "/getUpdates")
    chats = {}
    for item in updates.get("result", []):
        chat = item.get("message", {}).get("chat", {})
        if chat.get("type") == "private":
            chats[chat["id"]] = {"id": chat["id"], "name": chat.get("first_name", str(chat["id"]))}
    chat_id = choose(list(chats.values()), "Select your private chat")["id"] if chats else int(ask("Your Telegram chat ID"))
    auth_file = Path(ask("Codex ChatGPT auth.json", str(Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex"))) / "auth.json"))).expanduser().resolve()
    auth = json.loads(auth_file.read_text(encoding="utf-8-sig"))
    if auth.get("OPENAI_API_KEY") or not auth.get("tokens", {}).get("access_token") or not auth.get("tokens", {}).get("account_id"):
        raise RuntimeError("Sign in to Codex CLI with ChatGPT first; API-key authentication is not supported.")
    config.update(trello_board=board["id"], trello_todo=todo["id"], trello_progress=progress["id"], trello_done=done["id"], telegram_chat=chat_id, codex_auth_file=str(auth_file))
    save_private(ROOT / "secrets" / "integrations.json", {"trello_key": key, "trello_token": token, "telegram_token": telegram})
    save_private(config_path, config)
    print("Configuration saved privately. No agents have been started.")


if __name__ == "__main__":
    try:
        main()
    except (Exception, KeyboardInterrupt) as error:
        print("Installation stopped:", str(error) if not isinstance(error, KeyboardInterrupt) else "cancelled", file=sys.stderr)
        sys.exit(1)
