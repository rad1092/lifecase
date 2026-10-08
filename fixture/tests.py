"""Real-process native fixture conformance tests; no third-party dependencies.

Only Popen handles created here may be killed. Descendants receive token-owned
stop files and have independent watchdogs. Never use process-name matching.
"""

import concurrent.futures
import contextlib
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import unittest


FIXTURE = Path(sys.argv.pop(1)).resolve()


def process_not_live(pid):
    """Read-only inventory for an exact fixture-reported child, never signal it."""
    if os.name == "nt":
        import ctypes
        from ctypes import wintypes
        kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        kernel.OpenProcess.argtypes = (wintypes.DWORD, wintypes.BOOL, wintypes.DWORD)
        kernel.OpenProcess.restype = wintypes.HANDLE
        kernel.WaitForSingleObject.argtypes = (wintypes.HANDLE, wintypes.DWORD)
        kernel.WaitForSingleObject.restype = wintypes.DWORD
        kernel.CloseHandle.argtypes = (wintypes.HANDLE,)
        kernel.CloseHandle.restype = wintypes.BOOL
        handle = kernel.OpenProcess(0x00100000, False, pid)  # SYNCHRONIZE only.
        if not handle:
            return ctypes.get_last_error() == 87  # ERROR_INVALID_PARAMETER: gone.
        try:
            return kernel.WaitForSingleObject(handle, 0) == 0
        finally:
            kernel.CloseHandle(handle)
    try:
        os.kill(pid, 0)  # Signal 0 only queries existence; it cannot terminate.
    except ProcessLookupError:
        return True
    if sys.platform.startswith("linux"):
        try:
            state = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()[0]
            # An exited zombie waiting for the system's orphan reaper is not live.
            return state == "Z"
        except FileNotFoundError:
            return True
    return False


def wait_until(check, timeout=3):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = check()
        if value:
            return value
        time.sleep(0.005)
    raise AssertionError("bounded wait expired")


def event(directory, name):
    path = directory / (name + ".json")
    return json.loads(path.read_text()) if path.exists() else None


@contextlib.contextmanager
def full_blocking_pipe():
    """A pipe with a live reader but no available write space (Python 3.12+ on Windows)."""
    reader, writer = os.pipe()
    try:
        os.set_blocking(writer, False)
        filled = 0
        for chunk in (b"X" * 4096, b"X"):
            while True:
                try:
                    written = os.write(writer, chunk)
                except BlockingIOError:
                    break
                if not written:
                    break
                filled += written
                if filled > 4 * 1024 * 1024:
                    raise AssertionError("pipe fill exceeded synthetic test bound")
        assert filled > 0
        # The child must receive a blocking handle; nonblocking stderr would hide the bug.
        os.set_blocking(writer, True)
        yield writer
    finally:
        os.close(writer)
        os.close(reader)


def blocked_output_exit(arguments, target, timeout):
    with full_blocking_pipe() as writer:
        streams = {"stdout": subprocess.DEVNULL, "stderr": subprocess.DEVNULL}
        streams[target] = writer
        process = subprocess.Popen([str(FIXTURE), *arguments], stdin=subprocess.DEVNULL, **streams)
        try:
            return process.wait(timeout=timeout)
        finally:
            if process.poll() is None:
                process.kill()  # Exact child owned by this test, including assertion failures.
                process.wait(timeout=2)


@contextlib.contextmanager
def launched(scenario, lease=5000, executable=FIXTURE, directory_prefix="lifecase-native-"):
    with tempfile.TemporaryDirectory(prefix=directory_prefix) as temporary:
        directory = Path(temporary)
        token = secrets.token_hex(16)
        process = subprocess.Popen(
            [str(executable), "--scenario", scenario, "--dir", str(directory),
             "--token", token, "--lease-ms", str(lease)],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        )
        try:
            yield process, directory, token
        finally:
            (directory / "stop").write_text(token)
            if process.poll() is None:
                try:
                    process.wait(timeout=0.5)
                except subprocess.TimeoutExpired:
                    process.kill()  # Exact owned Popen object only.
                    process.wait(timeout=2)
            try:
                if (directory / "child-ready.json").exists():
                    # Check the exact owned child's OS state as well as its marker.
                    # Cleanup permits its lease when testing watchdog fallback.
                    child = event(directory, "child-ready")
                    assert child["token"] == token and child["role"] == "child"
                    wait_until(lambda: process_not_live(child["pid"]), timeout=lease / 1000 + 0.5)
            finally:
                for stream in (process.stdin, process.stdout, process.stderr):
                    if stream is not None:
                        stream.close()


def ready(process, directory, token):
    value = wait_until(lambda: event(directory, "parent-ready"))
    assert value == {"v": 1, "token": token, "pid": process.pid, "role": "parent", "event": "ready"}, value
    return value


def control(directory, name, token):
    temporary = directory / ("." + name)
    temporary.write_text(token)
    temporary.replace(directory / name)


class NativeFixtureTests(unittest.TestCase):
    def test_exit_ready_barrier_and_token(self):
        with launched("exit") as (process, directory, token):
            ready(process, directory, token)
            control(directory, "go", "0" * 32)
            control(directory, "stop", "0" * 32)
            time.sleep(0.08)
            self.assertIsNone(process.poll(), "mismatched control token must be ignored")
            control(directory, "go", token)
            out, err = process.communicate(timeout=2)
            self.assertEqual((process.returncode, out, err), (0, b"ok\n", b""))

    def test_flood_both_pipes_exact(self):
        with launched("flood") as (process, directory, token):
            ready(process, directory, token)
            control(directory, "go", token)
            out, err = process.communicate(timeout=3)
            self.assertEqual(process.returncode, 0)
            self.assertEqual(out, b"O" * 262144)
            self.assertEqual(err, b"E" * 262144)

    def test_blocked_output_cannot_outlive_watchdog(self):
        with launched("flood", lease=1000) as (process, directory, token):
            ready(process, directory, token)
            control(directory, "go", token)
            # Do not drain either pipe until the hard lease has killed writers.
            self.assertEqual(process.wait(timeout=3), 124)
            out, err = process.communicate(timeout=1)
            self.assertLess(len(out), 262144)
            self.assertLess(len(err), 262144)

    def test_error_diagnostic_cannot_outlive_watchdog(self):
        with tempfile.TemporaryDirectory(prefix="lifecase-error-pipe-") as temporary:
            directory = Path(temporary)
            directory.chmod(0o700)
            # Force write_event to throw after the configured lease is active.
            (directory / "parent-ready.json").write_text("collision")
            arguments = ["--scenario", "exit", "--dir", str(directory),
                         "--token", "a" * 32, "--lease-ms", "1000"]
            self.assertEqual(blocked_output_exit(arguments, "stderr", timeout=3), 124)

    def test_parse_error_diagnostic_has_bootstrap_watchdog(self):
        self.assertEqual(blocked_output_exit(["--invalid"], "stderr", timeout=8), 124)

    def test_help_and_version_flush_have_bootstrap_watchdog(self):
        for option in ("--help", "--version"):
            with self.subTest(option=option):
                self.assertEqual(blocked_output_exit([option], "stdout", timeout=8), 124)

    @unittest.skipUnless(os.name == "nt", "Windows UTF-16 argv conversion")
    def test_windows_invalid_unicode_diagnostic_is_bounded(self):
        self.assertEqual(blocked_output_exit(["\ud800"], "stderr", timeout=8), 124)

    def test_help_and_version_output_survive_immediate_exit(self):
        for option, expected in (("--help", "Usage: lifecase-fixture"),
                                 ("--version", "protocol 1")):
            with self.subTest(option=option):
                result = subprocess.run([str(FIXTURE), option], capture_output=True, text=True, timeout=2)
                self.assertEqual(result.returncode, 0)
                self.assertIn(expected, result.stdout)
                self.assertTrue(result.stdout.endswith("\n"))
                self.assertEqual(result.stderr, "")

    def test_stdin_backpressure_and_stop(self):
        with launched("stdin-blocked") as (process, directory, token):
            ready(process, directory, token)
            control(directory, "go", token)
            with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
                def write_stdin():
                    try:
                        process.stdin.write(b"I" * 262144)
                        process.stdin.flush()
                        return True
                    except (BrokenPipeError, OSError):
                        return False
                writer = pool.submit(write_stdin)
                time.sleep(0.08)
                self.assertFalse(writer.done(), "fixture must not consume stdin")
                control(directory, "stop", token)
                self.assertEqual(process.wait(timeout=2), 0)
                self.assertFalse(writer.result(timeout=2))

    def test_cooperative_stop_after_go(self):
        with launched("cooperative") as (process, directory, token):
            ready(process, directory, token)
            control(directory, "go", token)
            time.sleep(0.03)
            self.assertIsNone(process.poll())
            control(directory, "stop", token)
            self.assertEqual(process.wait(timeout=2), 0)

    def test_stop_before_go(self):
        with launched("cooperative") as (process, directory, token):
            ready(process, directory, token)
            control(directory, "stop", token)
            self.assertEqual(process.wait(timeout=2), 0)

    def test_uncooperative_ignores_stop_and_is_bounded(self):
        with launched("uncooperative", lease=1000) as (process, directory, token):
            ready(process, directory, token)
            control(directory, "go", token)
            control(directory, "stop", token)
            time.sleep(0.08)
            self.assertIsNone(process.poll())
            self.assertEqual(process.wait(timeout=3), 124)

    def test_crash_has_portable_abrupt_exit_code(self):
        with launched("crash") as (process, directory, token):
            ready(process, directory, token)
            control(directory, "go", token)
            out, err = process.communicate(timeout=2)
            self.assertEqual((process.returncode, out, err), (23, b"", b""))

    def test_inherited_pipes_outlive_parent(self):
        with launched("inherit") as (process, directory, token):
            ready(process, directory, token)
            child = event(directory, "child-ready")
            self.assertEqual(child["token"], token)
            self.assertEqual(child["role"], "child")
            self.assertNotEqual(child["pid"], process.pid)
            control(directory, "go", token)
            self.assertEqual(process.wait(timeout=2), 0)
            with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
                out = pool.submit(process.stdout.read)
                err = pool.submit(process.stderr.read)
                try:
                    time.sleep(0.08)
                    self.assertFalse(out.done(), "parent exit must precede actual stdout EOF")
                    self.assertFalse(err.done(), "parent exit must precede actual stderr EOF")
                finally:
                    control(directory, "stop", token)
                self.assertEqual(out.result(timeout=2), b"")
                self.assertEqual(err.result(timeout=2), b"")
            exited = wait_until(lambda: event(directory, "child-exit"))
            self.assertEqual(exited, {**child, "event": "exit"})

    def test_inherited_child_has_independent_watchdog(self):
        with launched("inherit", lease=1000) as (process, directory, token):
            ready(process, directory, token)
            process.kill()  # Kill only the directly created parent.
            process.wait(timeout=2)
            out, err = process.communicate(timeout=3)
            self.assertEqual((out, err), (b"", b""))
            self.assertIsNone(event(directory, "child-exit"), "watchdog is not graceful cleanup")

    def test_startup_and_spawn_cancellation_before_readiness(self):
        for scenario in ("startup-timeout", "spawn-cancel"):
            with self.subTest(scenario=scenario), launched(scenario) as (process, directory, token):
                time.sleep(0.08)
                self.assertIsNone(event(directory, "parent-ready"))
                control(directory, "stop", token)
                self.assertEqual(process.wait(timeout=2), 0)
                self.assertIsNone(event(directory, "parent-ready"))

    def test_delayed_startup_has_two_second_barrier(self):
        start = time.monotonic()
        with launched("startup-timeout") as (process, directory, token):
            ready(process, directory, token)
            self.assertGreaterEqual(time.monotonic() - start, 1.9)
            control(directory, "stop", token)
            self.assertEqual(process.wait(timeout=2), 0)

    @unittest.skipIf(os.name == "nt", "Windows console signals unsupported in protocol 1")
    def test_signal_shutdown(self):
        with launched("signal") as (process, directory, token):
            ready(process, directory, token)
            control(directory, "go", token)
            process.send_signal(signal.SIGTERM)
            self.assertEqual(process.wait(timeout=2), 0)

    @unittest.skipUnless(os.name == "nt", "Windows-specific unsupported capability")
    def test_windows_signal_explicitly_unsupported(self):
        with launched("signal") as (process, directory, token):
            out, err = process.communicate(timeout=2)
            self.assertEqual(process.returncode, 78)
            self.assertIn(b"unsupported", err)
            self.assertEqual(out, b"")
            self.assertIsNone(event(directory, "parent-ready"))

    def test_unicode_and_spaces_executable_and_directory(self):
        with tempfile.TemporaryDirectory(prefix="lifecase-경로 with spaces-") as temporary:
            executable = Path(temporary) / ("fixture 실행 file" + FIXTURE.suffix)
            shutil.copy2(FIXTURE, executable)
            # inherit also exercises native CreateProcessW/posix_spawn quoting.
            with launched("inherit", executable=executable, directory_prefix="lifecase-control-경로 with spaces-") as (process, directory, token):
                ready(process, directory, token)
                control(directory, "go", token)
                self.assertEqual(process.wait(timeout=2), 0)
                control(directory, "stop", token)
                out, err = process.communicate(timeout=2)
                self.assertEqual((out, err), (b"", b""))
                self.assertIsNotNone(wait_until(lambda: event(directory, "child-exit")))

    def test_rejects_invalid_inputs_without_spawning(self):
        with tempfile.TemporaryDirectory(prefix="lifecase-invalid-") as temporary:
            base = [str(FIXTURE), "--scenario", "inherit", "--dir", temporary,
                    "--token", "a" * 32]
            cases = [["--lease-ms", "999"], ["--lease-ms", "10001"],
                     ["--lease-ms", "1000junk"], ["--unknown", "x"],
                     ["--scenario", "exit"], ["--token", "B" * 32]]
            for extra in cases:
                result = subprocess.run(base + extra, capture_output=True, timeout=2)
                self.assertEqual(result.returncode, 64)
                self.assertLess(len(result.stderr), 512)
                self.assertEqual(result.stdout, b"")
            self.assertEqual(list(Path(temporary).iterdir()), [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
