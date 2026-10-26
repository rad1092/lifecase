"""What Python's subprocess reports when the process you started exits but a
process it started still holds stdout and stderr.

Run: LIFECASE_FIXTURE=/absolute/path/to/lifecase-fixture python test_subprocess.py
Needs only the standard library (Python 3.9+).
"""

import contextlib
import os
import secrets
import signal
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from pathlib import Path

FIXTURE = os.environ.get("LIFECASE_FIXTURE", "")
POSIX = os.name == "posix"


def wait_for(check, timeout, what):
    deadline = time.monotonic() + timeout
    while not check():
        if time.monotonic() > deadline:
            raise AssertionError(f"{what} did not happen within {timeout} s")
        time.sleep(0.01)


@contextlib.contextmanager
def inherit_run(lease_ms=5000):
    """Yield (args, go, stop, killed) for one inherit run; always stops the descendant.

    Call killed() after ending the descendant yourself, so cleanup does not wait
    for an exit marker it will never write.
    """
    # TemporaryDirectory is created with mode 0700, as the fixture requires.
    with tempfile.TemporaryDirectory(prefix="lifecase-python-") as temporary:
        directory = Path(temporary)
        token = secrets.token_hex(16)
        args = [FIXTURE, "--scenario", "inherit", "--dir", str(directory),
                "--token", token, "--lease-ms", str(lease_ms)]

        def go():
            wait_for(lambda: (directory / "parent-ready.json").exists(), 5, "fixture readiness")
            (directory / "go").write_text(token)

        def stop():
            (directory / "stop").write_text(token)

        ended = []
        try:
            yield args, go, stop, lambda: ended.append(True)
        finally:
            stop()
            if not ended and (directory / "child-ready.json").exists():
                with contextlib.suppress(AssertionError):
                    wait_for(lambda: (directory / "child-exit.json").exists(), lease_ms / 1000 + 1, "descendant exit")


@unittest.skipUnless(os.path.isabs(FIXTURE), "set LIFECASE_FIXTURE to the absolute fixture path")
class InheritedPipes(unittest.TestCase):
    def test_poll_reports_exit_while_communicate_still_waits(self):
        with inherit_run() as (args, go, stop, _killed):
            process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            try:
                go()
                wait_for(lambda: process.poll() is not None, 2, "parent exit")
                self.assertEqual(process.returncode, 0)
                # The process is gone, but its descendant still holds the pipes.
                with self.assertRaises(subprocess.TimeoutExpired):
                    process.communicate(timeout=0.5)
                stop()
                process.communicate(timeout=5)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait(timeout=2)

    def test_run_with_timeout_fails_a_process_that_exited_with_zero(self):
        # On Windows, run() reads the pipes again after the timeout, so it also
        # waits for the descendant's 2 s lease before raising.
        with inherit_run(lease_ms=2000) as (args, go, _stop, _killed):
            threading.Thread(target=go, daemon=True).start()
            started = time.monotonic()
            with self.assertRaises(subprocess.TimeoutExpired):
                subprocess.run(args, stdin=subprocess.DEVNULL, capture_output=True, timeout=1)
            if POSIX:
                self.assertLess(time.monotonic() - started, 2)

    @unittest.skipUnless(POSIX, "sessions and process groups are POSIX-only")
    def test_bounded_policy_ends_the_session_after_a_grace_period(self):
        with inherit_run() as (args, go, _stop, killed):
            process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, start_new_session=True)
            go()
            wait_for(lambda: process.poll() is not None, 2, "parent exit")
            try:
                process.communicate(timeout=0.2)
            except subprocess.TimeoutExpired:
                # The parent was the group leader; its descendant is still in the group.
                os.killpg(process.pid, signal.SIGKILL)
                killed()
                process.communicate(timeout=2)


if __name__ == "__main__":
    unittest.main(verbosity=2, argv=sys.argv[:1])
