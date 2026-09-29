"""Focused tests for the provider hook event to status mapping."""

import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("agent-status-hook.py")
SPEC = importlib.util.spec_from_file_location("agent_status_hook", SCRIPT)
HOOK = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(HOOK)


class StatusMappingTests(unittest.TestCase):
    def test_codex_events(self):
        cases = {
            "SessionStart": "blocked",
            "SessionEnd": "ended",
            "UserPromptSubmit": "working",
            "PreToolUse": "working",
            "PostToolUse": "working",
            "PermissionRequest": "blocked",
            "Stop": "ended",
            "Interrupt": "ended",
        }
        for event, status in cases.items():
            with self.subTest(event=event):
                self.assertEqual(HOOK.status_for("codex", {"hook_event_name": event}), status)
        self.assertIsNone(HOOK.status_for("codex", {"hook_event_name": "SessionStart", "source": "compact"}))
        self.assertIsNone(HOOK.status_for("codex", {"hook_event_name": "Unknown"}))

    def test_claude_events_and_notification_types(self):
        cases = {
            "SessionStart": "blocked",
            "SessionEnd": "ended",
            "UserPromptSubmit": "working",
            "UserPromptExpansion": "working",
            "PreToolUse": "working",
            "PostToolUse": "working",
            "PostToolBatch": "working",
            "PermissionRequest": "blocked",
            "Stop": "ended",
            "StopFailure": "ended",
        }
        for event, status in cases.items():
            with self.subTest(event=event):
                self.assertEqual(HOOK.status_for("claude", {"hook_event_name": event}), status)
        for kind, status in (("permission_prompt", "blocked"), ("agent_needs_input", "blocked"), ("idle_prompt", "ended")):
            with self.subTest(notification=kind):
                self.assertEqual(HOOK.status_for("claude", {"hook_event_name": "Notification", "notification_type": kind}), status)
        self.assertIsNone(HOOK.status_for("claude", {"hook_event_name": "Notification", "notification_type": "auth_success"}))

    @patch.object(HOOK.subprocess, "run")
    def test_report_uses_only_fixed_status_and_valid_pane_target(self, run):
        HOOK.report("codex", {"hook_event_name": "UserPromptSubmit"}, "%42")
        run.assert_called_once_with(
            ["tmux", "set-option", "-p", "-q", "-t", "%42", "@hostbud_agent_status", "working"],
            check=False,
            stdin=HOOK.subprocess.DEVNULL,
            stdout=HOOK.subprocess.DEVNULL,
            stderr=HOOK.subprocess.DEVNULL,
            timeout=2,
        )
        run.reset_mock()
        HOOK.report("codex", {"hook_event_name": "UserPromptSubmit"}, "; kill-server")
        run.assert_not_called()


class DaemonPaneLookupTests(unittest.TestCase):
    """Codex runs hooks in its app-server daemon, which has no TMUX_PANE."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.claims = os.path.join(self.tmp.name, "state", "agent-panes.json")

    def fake_proc(self, procs):
        proc = os.path.join(self.tmp.name, "proc")
        for pid, comm, tty, cwd in procs:
            os.makedirs(os.path.join(proc, str(pid)))
            with open(os.path.join(proc, str(pid), "stat"), "w") as f:
                f.write(f"{pid} ({comm}) S 1 1 1 {tty} -1 0 0\n")
            os.symlink(cwd, os.path.join(proc, str(pid), "cwd"))
        os.makedirs(os.path.join(proc, "self"), exist_ok=True)
        return proc

    def test_provider_clients_keeps_named_processes_with_a_tty(self):
        proc = self.fake_proc([
            (10, "codex", 34827, "/home/dev/repo"),
            (11, "codex", 0, "/home/dev/repo"),  # the daemon: no terminal
            (12, "node", 34827, "/home/dev/repo"),
            (13, "co dex)", 34828, "/home/dev/repo"),
        ])
        self.assertEqual(HOOK.provider_clients("codex", proc), [("10", 34827, os.path.realpath("/home/dev/repo"))])

    def find(self, clients, panes, event):
        with patch.object(HOOK, "provider_clients", return_value=clients), patch.object(HOOK, "pane_ttys", return_value=panes):
            return HOOK.find_pane("codex", event, self.claims)

    def test_single_client_in_cwd_is_used_without_a_session_id(self):
        clients = [("10", 1, "/home/dev/repo"), ("20", 2, "/home/dev/other")]
        panes = {1: "%1", 2: "%2"}
        self.assertEqual(self.find(clients, panes, {"cwd": "/home/dev/repo"}), "%1")
        self.assertEqual(self.find(clients, panes, {"cwd": "/home/dev/none"}), "")
        self.assertEqual(self.find(clients, panes, {"cwd": "relative"}), "")

    def test_sessions_claim_clients_in_start_order(self):
        repo = "/home/dev/repo"
        panes = {1: "%1", 2: "%2"}
        one = [("10", 1, repo)]
        both = one + [("20", 2, repo)]
        self.assertEqual(self.find(one, panes, {"cwd": repo, "session_id": "a"}), "%1")
        # A second client opens in the same repo: the new session takes the free one.
        self.assertEqual(self.find(both, panes, {"cwd": repo, "session_id": "b"}), "%2")
        self.assertEqual(self.find(both, panes, {"cwd": repo, "session_id": "a"}), "%1")
        self.assertEqual(self.find(both, panes, {"cwd": repo, "session_id": "b"}), "%2")
        # Both are claimed, so a third session in that repo is ambiguous.
        self.assertEqual(self.find(both, panes, {"cwd": repo, "session_id": "c"}), "")
        with open(self.claims) as f:
            self.assertEqual(json.load(f), {"a": "%1/10", "b": "%2/20"})

    def test_restarted_client_and_ended_session_release_claims(self):
        repo = "/home/dev/repo"
        panes = {1: "%1", 2: "%2"}
        both = [("10", 1, repo), ("20", 2, repo)]
        self.find([("10", 1, repo)], panes, {"cwd": repo, "session_id": "a"})
        self.find(both, panes, {"cwd": repo, "session_id": "b"})
        # %1's client exits and a new one starts there: the old claim no longer holds.
        restarted = [("11", 1, repo), ("20", 2, repo)]
        self.assertEqual(self.find(restarted, panes, {"cwd": repo, "session_id": "c"}), "%1")
        # A finished turn keeps the claim; only the session's end releases it.
        self.assertEqual(self.find(restarted, panes, {"cwd": repo, "session_id": "b", "hook_event_name": "Stop"}), "%2")
        with open(self.claims) as f:
            self.assertEqual(json.load(f), {"b": "%2/20", "c": "%1/11"})
        self.assertEqual(self.find(restarted, panes, {"cwd": repo, "session_id": "b", "hook_event_name": "SessionEnd"}), "%2")
        with open(self.claims) as f:
            self.assertEqual(json.load(f), {"c": "%1/11"})

    def test_single_client_takes_over_a_new_session(self):
        repo = "/home/dev/repo"
        one = [("10", 1, repo)]
        self.find(one, {1: "%1"}, {"cwd": repo, "session_id": "a"})
        self.assertEqual(self.find(one, {1: "%1"}, {"cwd": repo, "session_id": "b"}), "%1")

    @patch.dict(os.environ, {}, clear=True)
    @patch.object(HOOK.subprocess, "run")
    def test_report_without_tmux_pane_uses_lookup(self, run):
        with patch.object(HOOK, "find_pane", return_value="%7") as find:
            HOOK.report("codex", {"hook_event_name": "Stop", "cwd": "/home/dev/repo"})
        find.assert_called_once_with("codex", {"hook_event_name": "Stop", "cwd": "/home/dev/repo"})
        self.assertEqual(run.call_args.args[0][-3:], ["%7", "@hostbud_agent_status", "ended"])
        run.reset_mock()
        with patch.object(HOOK, "find_pane", side_effect=RuntimeError("boom")):
            HOOK.report("codex", {"hook_event_name": "Stop"})
        run.assert_not_called()

    @patch.dict(os.environ, {"TMUX_PANE": "%3"}, clear=True)
    @patch.object(HOOK.subprocess, "run")
    def test_report_prefers_tmux_pane(self, run):
        with patch.object(HOOK, "find_pane") as find:
            HOOK.report("claude", {"hook_event_name": "Stop"})
        find.assert_not_called()
        self.assertEqual(run.call_args.args[0][-3:], ["%3", "@hostbud_agent_status", "ended"])


if __name__ == "__main__":
    unittest.main()
