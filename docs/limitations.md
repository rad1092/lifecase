# Scope, capabilities, and interpretation

Lifecase tests controlled fixture behavior. A passing run applies to the tested adapter, fixture, runtime, and OS combination. It is not a proof that every arbitrary program or process tree will shut down correctly.

## Capability scope

The table describes the native fixtures and optional Go verifier. The direct CliWrap suite selects contracts from these inputs; it does not test native graceful console signaling. See [the .NET guide](../dotnet/README.md) for its exact assertions.

| Capability | Linux / macOS | Windows |
| --- | --- | --- |
| Direct native launch, including Unicode and spaces in the executable path | In scope | In scope |
| Separate parent-exit and stdout/stderr-EOF observations | In scope | In scope |
| Concurrent bounded output and blocked stdin | In scope | In scope |
| One descendant retaining inherited stdout/stderr | In scope | In scope |
| Cooperative shutdown through authenticated fixture control files | In scope | In scope |
| Force termination of the owned parent process | In scope | In scope |
| Cooperative native SIGTERM | In scope | Unsupported in protocol v1 |
| Windows CTRL+C / CTRL+BREAK / CTRL+CLOSE delivery | Not applicable | Unsupported in protocol v1 |
| Job Object, process group, or session containment contracts | Outside v1 | Outside v1 |

"In scope" describes an implemented test target, not evidence that an untested machine passed. Check the JSON/JUnit results and CI run for the exact commit. An adapter may expose a narrower capability set; inspect `unsupported` results before accepting a release.

The file-based cooperative stop proves the fixture's application-level shutdown path. It does not model a graphical application's close button or Windows console events. POSIX signal numbers and forced-exit representations are not a portable result format; rely on the named assertions and shutdown mode.

## What scenario names mean

- `inherit` creates one descendant that keeps the pipe handles/descriptors open after the parent exits. It checks observation and controlled cleanup; it does not exercise deep trees, daemonization, reparenting races, or nested Job Objects.
- `spawn-cancel` delays fixture readiness, allowing cancellation during startup. It cannot suspend or interrupt the operating system's process creation syscall.
- `startup-timeout` tests the ready handshake deadline. It is distinct from an executable-not-found error.
- `crash` exits abruptly with code 23. It does not trigger memory corruption, an access violation, a signal crash, a dump, or core collection.
- `flood` writes a fixed 262,144 bytes to each of stdout and stderr concurrently. This is a pipe-backpressure contract, not a throughput benchmark.
- `stdin-blocked` refuses to consume input. A pending asynchronous write must not prevent termination handling.
- The Unicode/spaces case copies the executable into a synthetic temporary path. It does not validate every shell's quoting or every filesystem normalization behavior.

## Bounds and trust

The fixture accepts a finite lease from 1,000 to 10,000 ms, with 5,000 ms used by the protocol's normal invocation. Each process has its own watchdog. There is at most one descendant per scenario. Output is fixed and retained prefixes are bounded; complete byte counts can exceed the retained prefix size.

The harness uses a fresh private temporary directory and a random token to bind fixture evidence and control files to a run. Control files are not a sandbox boundary against another program running as the same user. An external adapter is a trusted executable and can ignore the protocol or fabricate observations. Lifecase validates the contract and compares independent fixture milestones, but cannot make hostile adapter code safe.

Normal cleanup and watchdog expiry are different outcomes. A watchdog bounds how long a broken fixture can live; it must not be interpreted as successful graceful shutdown. Do not use a cached PID, name matching, or `pkill` to "repair" a failed test. The kit targets only the processes it launched and the authenticated fixture control plane.

## Timing and reports

Ready/go barriers reduce avoidable startup races. Scheduling, loaded CI hosts, antivirus, filesystem latency, and runtime startup still affect elapsed time. Reports preserve measured ordering and elapsed values; they are not hard realtime guarantees.

Deterministic output means stable scenario order, assertion identities, and status vocabulary. PIDs and timing observations change. JSON files from two correct runs need not be byte-identical. Unsupported capabilities become JUnit skipped entries; skipping does not demonstrate that a feature works.

Lifecase is a correctness gate with bounded evidence, not a benchmark. Run it repeatedly on the platforms you ship, preserve reports, and review any runtime/compiler upgrade against those results.
