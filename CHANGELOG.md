# Changelog

## Unreleased

- Introduce protocol v1 for controlled native fixtures and external JSON Lines runner adapters.
- Add distinct contracts for parent exit, actual stdout/stderr EOF, descendant cleanup, and shutdown behavior.
- Add bounded native scenarios for inherited pipes, concurrent output, blocked stdin, cooperative and forced shutdown, abrupt exit, delayed readiness, and POSIX signals.
- Add a Go harness and CLI with JSON/JUnit reporting and executable-path coverage.
- Add .NET fixture integration contracts against the official CliWrap 3.10.5 NuGet package, including a comparison with cancellation of `Process.WaitForExitAsync`.
- Document safety bounds, platform capabilities, historical issue evidence, and a Korean quickstart.

Protocol changes that alter required events or their meaning require a new protocol version. New implementation fixes must preserve the documented semantics of version 1.
