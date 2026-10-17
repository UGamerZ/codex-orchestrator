"""Run with python3 scripts/test_configure_linux.py; no external network or accounts."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("configure_linux", Path(__file__).with_name("configure-linux.py"))
wizard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(wizard)


class InstallerTests(unittest.TestCase):
    def test_full_github_setup_with_mocked_integrations(self):
        template = (wizard.ROOT / "config.example.json").read_text(encoding="utf-8-sig")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "config.example.json").write_text(template, encoding="utf-8")
            auth = root / "mock-auth.json"
            auth.write_text(json.dumps({"tokens": {"access_token": "dummy-access", "account_id": "dummy-account"}}))

            def command(args, env=None):
                if args[:3] == ["gh", "auth", "token"]:
                    return "dummy-gh-token"
                if args[:3] == ["gh", "api", "user"]:
                    self.assertEqual(env["GH_TOKEN"], "dummy-gh-token")
                    return "project-user"
                if args == ["gh", "api", "repos/owner/repo"]:
                    return '{"permissions":{"push":true},"archived":false}'
                return "{}"

            def request(url):
                if "members/me/boards" in url:
                    return [{"id": "a" * 24, "name": "board"}]
                if "/lists?" in url:
                    return [{"id": char * 24, "name": char} for char in "bcd"]
                if url.endswith("/getMe"):
                    return {"ok": True, "result": {"username": "dummy_bot"}}
                if url.endswith("/getUpdates"):
                    return {"result": [{"message": {"chat": {"id": 12345, "type": "private", "first_name": "Test"}}}]}
                raise AssertionError("Unexpected request")

            answers = ["main", "1", "project-user", "owner/repo", "1", "1", "1", "1", "", "1", str(auth)]
            with patch.object(wizard, "ROOT", root), patch.object(wizard.shutil, "which", return_value="/mock/gh"), patch.object(wizard, "checked", side_effect=command), patch.object(wizard, "api", side_effect=request), patch("builtins.input", side_effect=answers), patch.object(wizard.getpass, "getpass", side_effect=["dummy-key", "dummy-token", "dummy-bot"]):
                wizard.main()
            config = json.loads((root / "config.json").read_text())
            self.assertEqual(config["github_user"], "project-user")
            self.assertEqual(config["docker_distro"], "")
            if wizard.sys.platform == "darwin":
                self.assertEqual(config["docker_vm"], "colima")
            self.assertEqual(config["trello_done"], "d" * 24)
            self.assertEqual(config["telegram_chat"], 12345)
            self.assertEqual(config["codex_auth_file"], str(auth.resolve()))
            self.assertEqual(json.loads((root / "secrets" / "integrations.json").read_text())["telegram_token"], "dummy-bot")

    def test_macos_setup_selects_colima(self):
        with patch.object(wizard.sys, "platform", "darwin"):
            self.test_full_github_setup_with_mocked_integrations()

    def test_macos_refuses_to_reuse_incompatible_config(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "config.json").write_text('{"backend":"docker","docker_distro":"Ubuntu"}')
            with patch.object(wizard, "ROOT", root), patch.object(wizard.sys, "platform", "darwin"), patch("builtins.input", return_value="Y"):
                with self.assertRaisesRegex(RuntimeError, "docker_vm=colima"):
                    wizard.main()

    def test_private_json_and_symlink_refusal(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "secrets" / "integrations.json"
            wizard.save_private(path, {"token": "dummy-token"})
            self.assertEqual(json.loads(path.read_text()), {"token": "dummy-token"})
            if hasattr(__import__("os"), "getuid"):
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            try:
                (path.parent / "linked.json").symlink_to(path)
            except OSError:
                return
            with self.assertRaisesRegex(RuntimeError, "symbolic links"):
                wizard.save_private(path.parent / "linked.json", {})

    def test_bad_selection_retries(self):
        with patch("builtins.input", side_effect=["0", "nonsense", "2"]):
            selected = wizard.choose([{"name": "one"}, {"name": "two"}], "Select")
        self.assertEqual(selected["name"], "two")

    def test_errors_do_not_expose_credential_urls(self):
        with patch.object(wizard.urllib.request, "urlopen", side_effect=OSError("https://example.com/?token=dummy-private")):
            with self.assertRaises(RuntimeError) as failure:
                wizard.api("https://example.com/?token=dummy-private")
        self.assertNotIn("dummy-private", str(failure.exception))

    def test_process_failure_does_not_expose_credentials(self):
        with patch.object(wizard.subprocess, "run", return_value=wizard.subprocess.CompletedProcess(["gh"], 1, "", "dummy-secret")):
            with self.assertRaises(RuntimeError) as failure:
                wizard.checked(["gh", "api", "user"])
        self.assertNotIn("dummy-secret", str(failure.exception))


if __name__ == "__main__":
    unittest.main()
