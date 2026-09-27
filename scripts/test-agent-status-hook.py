"""Focused tests for the provider hook event to status mapping."""

import importlib.util
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


if __name__ == "__main__":
    unittest.main()
