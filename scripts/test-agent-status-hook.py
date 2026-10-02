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
            "Stop": "blocked",
            "Interrupt": "blocked",
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
            "Stop": "blocked",
            "StopFailure": "blocked",
        }
        for event, status in cases.items():
            with self.subTest(event=event):
                self.assertEqual(HOOK.status_for("claude", {"hook_event_name": event}), status)
        for kind in ("permission_prompt", "idle_prompt", "agent_needs_input"):
            with self.subTest(notification=kind):
                self.assertEqual(HOOK.status_for("claude", {"hook_event_name": "Notification", "notification_type": kind}), "blocked")
        self.assertIsNone(HOOK.status_for("claude", {"hook_event_name": "Notification", "notification_type": "auth_success"}))

    @patch.object(HOOK.subprocess, "run")
    def test_report_uses_only_fixed_status_and_valid_pane_target(self, run):
        HOOK.report("codex", {"hook_event_name": "UserPromptSubmit"}, "%42")
        run.assert_any_call(
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


class CodexUsageTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.path = self.root / "sessions" / "rollout.jsonl"
        self.path.parent.mkdir()
        env = patch.dict(os.environ, {"CODEX_HOME": str(self.root)})
        env.start()
        self.addCleanup(env.stop)

    def record(self, current=12000, total=345678, window=200000):
        return json.dumps({"type": "event_msg", "payload": {"type": "token_count", "info": {
            "last_token_usage": {"total_tokens": current, "cached_input_tokens": 100},
            "total_token_usage": {"total_tokens": total, "reasoning_output_tokens": 50},
            "model_context_window": window,
        }}}) + "\n"

    def read(self, text):
        self.path.write_text(text)
        return HOOK.codex_usage({"transcript_path": str(self.path)})

    def test_latest_usage_ignores_partial_records_and_does_not_double_count(self):
        self.assertEqual(self.read(self.record() + self.record(14000, 360000) + '{"type":'), "14000,360000,200000")
        self.assertEqual(self.read(self.record(0, 0, None)), "0,0,0")
        self.assertEqual(self.read(self.record(2000, 360000)), "2000,360000,200000")  # compacted context
        self.assertEqual(self.read(self.record() + '{"type":"event_msg","payload":{"type":"token_count","info":null}}\n'), "12000,345678,200000")

    def test_only_structured_bounded_safe_counts_are_accepted(self):
        for invalid in (-1, True, 1.5, "100", 2**53):
            self.assertEqual(self.read(self.record(invalid)), "")
        self.assertEqual(self.read('null\n[]\n' + json.dumps({"type": "response_item", "text": self.record()}) + "\n"), "")
        self.assertEqual(self.read(self.record() + "x" * 1048576), "")
        self.assertEqual(self.read("x" * 1048576 + "\n" + self.record()), "12000,345678,200000")

    def test_missing_external_symlink_and_fifo_paths_are_ignored(self):
        self.assertEqual(HOOK.codex_usage({}), "")
        outside = self.root / "outside.jsonl"
        outside.write_text(self.record())
        self.assertEqual(HOOK.codex_usage({"transcript_path": str(outside)}), "")
        self.path.symlink_to(outside)
        self.assertEqual(HOOK.codex_usage({"transcript_path": str(self.path)}), "")
        self.path.unlink()
        os.mkfifo(self.path)
        self.assertEqual(HOOK.codex_usage({"transcript_path": str(self.path)}), "")

    @patch.object(HOOK.subprocess, "run")
    def test_hooks_publish_counts_and_clear_unknown_new_sessions(self, run):
        self.read(self.record())
        HOOK.report("codex", {"hook_event_name": "Stop", "transcript_path": str(self.path)}, "%1")
        self.assertEqual(run.call_args_list[0].args[0][-2:], ["@hostbud_codex_usage", "12000,345678,200000"])
        run.reset_mock()
        HOOK.report("codex", {"hook_event_name": "SessionStart"}, "%1")
        self.assertEqual(run.call_args_list[0].args[0][-2:], ["@hostbud_codex_usage", ""])
        run.reset_mock()
        HOOK.report("codex", {"hook_event_name": "SessionStart", "source": "compact", "transcript_path": str(self.path)}, "%1")
        self.assertEqual(len(run.call_args_list), 1)
        self.assertEqual(run.call_args.args[0][-2:], ["@hostbud_codex_usage", "12000,345678,200000"])


class ClaudeUsageTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.path = self.root / "projects" / "-home-dev" / "s.jsonl"
        self.path.parent.mkdir(parents=True)
        self.cache = str(self.root / "state" / "claude-usage.json")
        env = patch.dict(os.environ, {"CLAUDE_CONFIG_DIR": str(self.root)})
        env.start()
        self.addCleanup(env.stop)

    def record(self, mid="m1", inp=10, create=1000, read=20000, out=500, **extra):
        usage = {"input_tokens": inp, "cache_creation_input_tokens": create, "cache_read_input_tokens": read, "output_tokens": out}
        return json.dumps({"type": "assistant", "message": {"id": mid, "usage": usage, "content": "secret"}, **extra}) + "\n"

    def usage(self):
        return HOOK.claude_usage({"transcript_path": str(self.path)}, self.cache)

    def append(self, text):
        with open(self.path, "a") as f:
            f.write(text)

    def test_counts_each_message_once_and_reads_incrementally(self):
        self.append(self.record() + self.record())  # one message, two content blocks
        self.assertEqual(self.usage(), "21510,21510,0")
        self.append(self.record("m2", 5, 0, 21500, 100) + '{"type":"assistant"')
        self.assertEqual(self.usage(), "21605,43115,0")
        self.assertEqual(self.usage(), "21605,43115,0")  # nothing new: same totals
        self.append(',"message":{"id":"m3","usage":{"output_tokens":1}}}\n')  # finished partial record
        self.assertEqual(self.usage(), "1,43116,0")
        cached = json.loads(Path(self.cache).read_text())
        self.assertEqual(cached[str(self.path)]["offset"], self.path.stat().st_size)
        self.assertNotIn("secret", Path(self.cache).read_text())

    def test_ignores_sidechains_invalid_counts_and_other_records(self):
        self.append(self.record("s", 1, 1, 1, 1, isSidechain=True) + self.record("x", True) + self.record("y", -1)
                    + json.dumps({"type": "user", "message": {"usage": {"input_tokens": 9}}}) + "\nnot json usage\n")
        self.assertEqual(self.usage(), "")
        self.append(self.record())
        self.assertEqual(self.usage(), "21510,21510,0")

    def test_rewritten_transcript_restarts_and_paths_are_restricted(self):
        self.append(self.record() + self.record("m2"))
        self.assertEqual(self.usage(), "21510,43020,0")
        self.path.unlink()
        self.append(self.record("m9", 1, 0, 0, 1))
        self.assertEqual(self.usage(), "2,2,0")
        self.assertEqual(HOOK.claude_usage({}, self.cache), "")
        outside = self.root / "outside.jsonl"
        outside.write_text(self.record())
        self.assertEqual(HOOK.claude_usage({"transcript_path": str(outside)}, self.cache), "")
        link = self.path.parent / "link.jsonl"
        link.symlink_to(outside)
        self.assertEqual(HOOK.claude_usage({"transcript_path": str(link)}, self.cache), "")
        fifo = self.path.parent / "fifo.jsonl"
        os.mkfifo(fifo)
        self.assertEqual(HOOK.claude_usage({"transcript_path": str(fifo)}, self.cache), "")

    @patch.object(HOOK.subprocess, "run")
    def test_hooks_publish_claude_counts_and_skip_unrelated_events(self, run):
        self.append(self.record())
        with patch.object(HOOK, "usage_cache_path", return_value=self.cache):
            HOOK.report("claude", {"hook_event_name": "Stop", "transcript_path": str(self.path)}, "%1")
            self.assertEqual(run.call_args_list[0].args[0][-2:], ["@hostbud_claude_usage", "21510,21510,0"])
            self.assertEqual(run.call_args_list[1].args[0][-2:], ["@hostbud_agent_status", "blocked"])
            run.reset_mock()
            HOOK.report("claude", {"hook_event_name": "SessionStart", "transcript_path": str(self.root / "projects" / "-home-dev" / "new.jsonl")}, "%1")
            self.assertEqual(run.call_args_list[0].args[0][-2:], ["@hostbud_claude_usage", ""])
            run.reset_mock()
            HOOK.report("claude", {"hook_event_name": "Notification", "notification_type": "other"}, "%1")
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

    def find(self, clients, panes, event, status="working"):
        with patch.object(HOOK, "provider_clients", return_value=clients), patch.object(HOOK, "pane_ttys", return_value=panes):
            return HOOK.find_pane("codex", event, status, self.claims)

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
        self.assertEqual(self.find(restarted, panes, {"cwd": repo, "session_id": "b"}, "ended"), "%2")
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
        find.assert_called_once_with("codex", {"hook_event_name": "Stop", "cwd": "/home/dev/repo"}, "blocked")
        self.assertEqual(run.call_args.args[0][-3:], ["%7", "@hostbud_agent_status", "blocked"])
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
        self.assertEqual(run.call_args.args[0][-3:], ["%3", "@hostbud_agent_status", "blocked"])


if __name__ == "__main__":
    unittest.main()
