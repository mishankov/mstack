import contextlib
import io
import runpy
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "skills/babysit-pr/scripts/pr-wait"
Watcher = runpy.run_path(str(SCRIPT))["Watcher"]


class FixtureWatcher(Watcher):
    def __init__(self, *, bucket="pass", state="CLEAN", unresolved=False):
        super().__init__("example/repo", 1, "ready", 5)
        self.required = {"ci"}
        self.bucket = bucket
        self.merge_state = state
        self.extra_checks = {}
        self.threads = []
        if unresolved:
            self.threads.append({
                "isResolved": False, "path": "README.md", "line": 1,
                "comments": {"nodes": [{"id": "c1", "body": "Fix this", "url": "thread-url"}]},
            })

    def load(self):
        return None

    def save(self):
        pass

    def fetch_pr(self):
        return {
            "state": "OPEN", "headRefOid": "123456789", "baseRefName": "main",
            "mergeStateStatus": self.merge_state, "viewer": "viewer",
            "comments": {"nodes": []}, "reviews": {"nodes": []},
            "reviewThreads": {"nodes": self.threads},
        }

    def fetch_checks(self):
        checks = {"ci": {"bucket": self.bucket, "link": "check-url"}} if self.bucket else {}
        return checks | self.extra_checks


class TestReady(unittest.TestCase):
    def test_exits_without_bot_feedback(self):
        cases = [
            ({}, 0, "ready on"),
            ({"unresolved": True}, 1, "unresolved threads"),
            ({"state": "BEHIND"}, 1, "BEHIND"),
            ({"state": "DIRTY"}, 1, "DIRTY"),
            ({"bucket": "fail"}, 1, "required check failed"),
        ]
        for options, code, message in cases:
            with self.subTest(options=options):
                watcher = FixtureWatcher(**options)
                out = io.StringIO()
                with contextlib.redirect_stdout(out), self.assertRaises(SystemExit) as result:
                    watcher.poll()
                    watcher.poll()
                self.assertEqual(result.exception.code, code)
                self.assertIn(message, out.getvalue())

    def test_missing_required_check_keeps_waiting(self):
        watcher = FixtureWatcher(bucket=None)
        watcher.poll()
        watcher.poll()

    def test_optional_review_checks_do_not_hold_fallback_ci(self):
        for name in ("Greptile Review", "Macroscope Review"):
            with self.subTest(name=name):
                watcher = FixtureWatcher()
                watcher.required = set()
                watcher.extra_checks[name] = {"bucket": "pending", "link": "review-url"}
                with contextlib.redirect_stdout(io.StringIO()), self.assertRaises(SystemExit) as result:
                    watcher.poll()
                    watcher.poll()
                self.assertEqual(result.exception.code, 0)

                watcher = FixtureWatcher()
                watcher.required.add(name)
                watcher.extra_checks[name] = {"bucket": "pending", "link": "review-url"}
                watcher.poll()
                watcher.poll()

    def test_ci_job_with_bot_name_still_blocks_readiness(self):
        for name in ("test-greptile-integration", "test-macroscope-integration"):
            with self.subTest(name=name):
                watcher = FixtureWatcher()
                watcher.required = set()
                watcher.extra_checks[name] = {"bucket": "fail", "link": "integration-url"}
                out = io.StringIO()
                with contextlib.redirect_stdout(out), self.assertRaises(SystemExit) as result:
                    watcher.poll()
                    watcher.poll()
                self.assertEqual(result.exception.code, 1)
                self.assertIn(f"check failed: {name}", out.getvalue())
