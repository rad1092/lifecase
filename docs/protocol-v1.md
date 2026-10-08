# Lifecase protocol 1

Lifecase is an executable contract kit for real OS subprocess runners. It is not a production process supervisor. The native fixture, Go harness and external adapters share protocol version 1.

## Fixture

`lifecase-fixture --scenario NAME --dir ABSOLUTE_PRIVATE_DIR --token HEX32 --lease-ms 5000`

Scenarios: `exit`, `flood`, `stdin-blocked`, `cooperative`, `uncooperative`, `crash`, `inherit`, `startup-timeout`, `spawn-cancel`, `signal`.

Harness owns a fresh directory (0700 where supported), creates `go` to release the ready barrier and `stop` to request fixture cleanup. Token is random 16-byte lowercase hex. Control files contain token text; fixtures ignore mismatches. Fixture atomically writes `parent-ready.json`, and for inherit `child-ready.json` and `child-exit.json`. JSON is `{ "v":1,"token":"...","pid":123,"role":"parent|child","event":"ready|exit" }`. Files are rename-written. Parent-ready follows child-ready for inherit. Each process has an independent hard watchdog (lease 5000, allowed 1000..10000 ms) calling immediate exit(124). Since fixture 0.1.1, a 5000 ms bootstrap watchdog begins at program entry before argument conversion; a validated lease adjusts that original deadline without resetting elapsed time. Invalid arguments, help/version output, exception diagnostics, and output flush remain under the bootstrap/configured watchdog. Process termination occurs while the watchdog remains active. Failure to establish or operate the guard exits immediately with code 70 and no fallback diagnostic.

On go: exit writes `ok\n` and exits 0; flood concurrently writes exactly 262144 `O` bytes to stdout and 262144 `E` bytes to stderr; stdin-blocked never reads stdin; cooperative waits for stop then exits 0; uncooperative ignores stop; crash exits immediately with code 23 (portable abrupt exit, no core dump); inherit parent exits 0 while one child holds stdout/stderr until stop or watchdog; startup-timeout and spawn-cancel delay readiness 2000ms; signal waits for SIGTERM on POSIX and then exits 0. Signal handling uses safe flag polling, no handler I/O. Windows native console signal contract is explicitly unsupported in v1. Stop is honored before go for cleanup except uncooperative. At most one descendant; no detached process outside watchdog.

## Runner adapter JSON Lines

External adapter executable receives one request line: `{"v":1,"op":"start","fixture":"absolute executable","args":["--scenario",...],"capture_limit":4096}`. It must launch directly without a shell, keep stdin open, concurrently drain both output streams, bound retained prefixes to capture_limit, and report complete byte counts. Standard output is exclusively protocol JSON lines, diagnostic stderr is bounded by the harness. One session per adapter invocation.

The Go verifier accepts capture_limit from 0 to 8192 bytes and bounds each JSON line to 65536 bytes. Required `exit_code` and `bytes` fields must be present even when zero.

Events: `{"v":1,"kind":"started","pid":123}`; `{"v":1,"kind":"parent_exit","exit_code":0}`; `{"v":1,"kind":"stdout_eof","bytes":3,"prefix":"ok\n"}`; corresponding stderr_eof; errors use `{"v":1,"kind":"error","message":"..."}`. Events may be concurrent. Parent exit MUST be emitted independently of pipe drain; stdout_eof/stderr_eof mean actual zero-length read, never a forced close. All four normal events are required. Adapter exits after parent-exit and both EOFs. It consumes further commands `{"v":1,"op":"kill"}` (force only the exact process it launched), `{"v":1,"op":"signal"}` (POSIX SIGTERM only), `{"v":1,"op":"write_stdin","bytes":262144}` (bounded asynchronous write), `{"v":1,"op":"close_stdin"}`. Kill is idempotent after exit. Unsupported signal emits kind `unsupported`. On input EOF adapter kills only its owned parent, closes resources and has a bounded exit. Harness writes stop independently to safely release the descendant even if adapter fails. Watchdogs are fallback, not successful cleanup evidence.

Adapters are trusted executable test subjects, not sandboxed programs. Events are checked for v, known kinds, bounded size, duplicate milestones and token-owned fixture readiness. A lying adapter is outside scope; the kit detects common incorrect lifecycle implementations with independent control-plane evidence and negative adapter tests.

## Results

Stable scenario order, assertions and status strings (`pass`, `fail`, `unsupported`) are deterministic. Durations/PIDs are observations and intentionally variable. Report includes parent exit, actual stdout/stderr EOF, descendant cleanup, and shutdown mode as distinct observations. Failures cause a nonzero CLI exit. Unsupported capabilities appear in JSON and JUnit skipped entries.
