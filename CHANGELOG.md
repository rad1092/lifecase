# Changelog

## Unreleased

- First integration examples for Node `child_process` and Python `subprocess`, standard library only: `exit` versus `close`, `execFile` with and without `timeout`, `poll()` versus `communicate()`, `run(timeout=...)`, and a bounded process-group policy. `examples/node/hang-demo.mjs` shows a CI step that cannot finish. CI runs both on all three platforms, and release archives include them.
- README leads with the CI-hang reproduction, says who should use or skip Lifecase, and marks it as a lab project. The CliWrap contracts remain as the .NET example.

## 0.1.3 — 2026-10-12

- Reserve all output files before launching a fixture, using destination filesystem rules to reject symlink-parent and case aliases. A collision removes empty reservations without leaving partial evidence. Existing files are still never overwritten.
- Roll back only newly created empty parent directories when reservation fails, including ancestor/descendant output paths. Existing directories and any new contents remain untouched.
- Keep output handles for the run. Ordinary failures remove unused reservations; a hard kill can leave an empty file, and two report writes are not an atomic transaction.

## 0.1.2 — 2026-10-12

- Refuse existing report paths before starting scenarios and create reports exclusively. JSON/JUnit output can no longer truncate an existing report, a fixture executable, or a symlink target.
- Reject identical JSON/JUnit paths before executing a fixture. Use fresh report filenames for repeat runs; report publication is not an atomic two-file transaction, and write errors remain exit 2.
- Include the optional Go verifier executable in each native fixture archive, and execute that extracted verifier in the release gate. No Go installation is needed to try the packaged verifier.
- Keep protocol v1 and native fixture behavior unchanged. Add report-preservation regression tests and first-run result guidance.

## 0.1.1 — 2026-10-08

- Fix a watchdog lifetime defect: an exception unwound the run-local guard before writing a diagnostic, so a full inherited stderr pipe could block indefinitely in 0.1.0.
- Keep one watchdog active from program entry through argument conversion, diagnostics and final output flush, then exit while the guard remains active. Invalid invocations and help/version use a 5000 ms bootstrap deadline; validated leases retain the original entry start time.
- Add actual saturated-pipe regressions for runtime errors, parse errors, help/version flush, Windows invalid Unicode arguments, and normal output preservation. Every negative test has an independent timeout and owns its cleanup handle.
- Preserve the 0.1.0 tag and artifacts; 0.1.1 is a separate patch release with protocol 1 unchanged.

## 0.1.0 — 2026-10-08

- Introduce protocol v1 for controlled native fixtures and external JSON Lines runner adapters.
- Add distinct contracts for parent exit, actual stdout/stderr EOF, descendant cleanup, and shutdown behavior.
- Add bounded native scenarios for inherited pipes, concurrent output, blocked stdin, cooperative and forced shutdown, abrupt exit, delayed readiness, and POSIX signals.
- Add a Go harness and CLI with JSON/JUnit reporting and executable-path coverage.
- Add .NET fixture integration contracts against the official CliWrap 3.10.5 NuGet package, including a comparison with cancellation of `Process.WaitForExitAsync`.
- Document safety bounds, platform capabilities, historical issue evidence, and a Korean quickstart.

Protocol changes that alter required events or their meaning require a new protocol version. New implementation fixes must preserve the documented semantics of version 1.
