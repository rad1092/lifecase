# Changelog

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
