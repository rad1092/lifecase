# Lifecase

**Reproduce the CI step that prints its last line and never finishes.**

> **Lab project.** Lifecase is a small, tested experiment rather than a maintained product. The fixture and examples work and run in CI on Linux, macOS and Windows. Breadth beyond them waits for a real user with a concrete case.

A process your build starts can exit while something it started (a dev server, a file watcher, a compiler daemon) keeps stdout and stderr open. Then:

- Node's `child_process` fires `exit` but not `close`.
- `execFile` callbacks and Python's `subprocess.run(capture_output=True)` wait for the pipes.
- The job sits there until a timeout, or reports success with the output cut off.

Lifecase is a native fixture that does exactly this on demand. Its parent exits at once while one descendant holds the pipes until you say stop. With it you can write the test that proves your runner handles the case, and keep it in CI.

[한국어 시작 가이드](docs/quickstart.ko.md) · [Protocol v1](docs/protocol-v1.md) · [Limits and capabilities](docs/limitations.md) · [Research and fit](docs/research.md)

## Is this for you?

**Use it** if you maintain a build runner, test executor, dev tool or job worker that starts processes and decides when they are "done", and you want a repeatable inherited-pipe, blocked-stdin or forced-shutdown case in its CI.

**Skip it** in these cases:
- A fake process is enough for your tests (for example [pytest-subprocess](https://pytest-subprocess.readthedocs.io/en/latest/)).
- Your process library's own tests already cover your policy.
- You need production process supervision.

## Try it with Node or Python

The examples use only the standard library: no npm or pip install. You need the native fixture. Take `lifecase-fixture-TARGET.zip` from [Releases](https://github.com/rad1092/lifecase/releases) (verify its `.sha256` sidecar first), or build it with CMake 3.16+ and a C++17 compiler:

```sh
git clone https://github.com/rad1092/lifecase && cd lifecase
cmake -S . -B build && cmake --build build --config Release
export LIFECASE_FIXTURE="$PWD/build/lifecase-fixture"

node examples/node/hang-demo.mjs
(cd examples/node && node --test)
python3 examples/python/test_subprocess.py
```

The demo is a CI step in miniature. The tool exits at once, but Node cannot end until the descendant lets go of the pipes (here, its 5 second watchdog):

```text
   27 ms  tool exited with 0; the step looks finished
 5016 ms  pipes closed; only now can Node exit
 5016 ms  node process ends
```

What the example tests pin down, so a runtime or library upgrade that changes them fails your build:

| Example | What it shows |
| --- | --- |
| Node `exit` vs `close` | `exit` fires at once; `close` waits until the descendant releases the pipes. |
| Node `execFile` | The callback waits for the descendant, not for the process you started. |
| Node `execFile` with `timeout` | When the timeout fires, Node closes the pipes and reports **success** (`error === null`) for a process that exited long before. |
| Python `poll()` vs `communicate()` | `poll()` reports exit code 0 while `communicate()` still waits. |
| Python `run(timeout=...)` | Raises `TimeoutExpired` for a process that exited with 0. On Windows it also waits for the descendant before raising. |
| Bounded policy (POSIX) | Drain briefly after exit, then end the process group or session. `close` arrives within the grace period. |

Copy the policy your tool actually wants into its own test suite, pin the fixture version, and run it on each platform you ship. On Windows, set `LIFECASE_FIXTURE` to `lifecase-fixture.exe`. The process-group tests are skipped there.

Each release archive also includes the `lifecase` verifier (`lifecase.exe` on Windows), which runs the full scenario list and writes JSON/JUnit evidence without Go or .NET:

```sh
./lifecase run --fixture ./lifecase-fixture --scenario exit --scenario flood --json reports/first.json --junit reports/first.xml
```

Release targets are `linux-x64`, `macos-arm64` and `windows-x64`. The [release guide](docs/release-process.md) covers artifact contents and install checks. macOS binaries are not claimed to be signed or notarized; use a local source build if your download policy prevents execution.

## .NET (CliWrap) contracts

The [.NET guide](dotnet/README.md) runs the same fixture against the real CliWrap 3.10.5 package with reusable test helpers. CliWrap already has extensive lifecycle tests of its own; this example is for teams that wrap CliWrap with their own cancellation or output policy. You need the .NET 8 SDK:

```sh
export LIFECASE_FIXTURE="$PWD/build/lifecase-fixture"
dotnet test dotnet/tests/Lifecase.CliWrap.Tests -c Release --logger trx
```

For a Visual Studio build on Windows, use `$env:LIFECASE_FIXTURE = (Resolve-Path ./build/Release/lifecase-fixture.exe).Path` in PowerShell.

## Explicit lifecycle policy

The verifier's contract treats these observations independently:

1. A parent exit is observable even if a descendant still holds its output pipes open.
2. EOF means a real zero-byte stream read. Closing a pipe forcibly is not EOF evidence.
3. Both output streams must drain concurrently; retention is bounded while total bytes are counted.
4. Cooperative stop and force termination are different shutdown outcomes.
5. A controlled descendant must finish its cleanup; the parent's exit alone is insufficient.

These are test policies, not a universal definition imposed on libraries. A high-level API may intentionally combine process exit with pipe completion. Instrument the underlying milestones when you need the verifier's separate observations; direct fixture tests can instead assert your application's documented policy.

## Controlled inputs

| Scenario | What it exercises |
| --- | --- |
| `exit` | Exit code 0 and complete output |
| `flood` | Simultaneous stdout/stderr, 262,144 bytes each |
| `stdin-blocked` | A child that never reads stdin |
| `cooperative` / `uncooperative` | File-based stop acceptance or force termination |
| `crash` | Abrupt exit code 23 without a core dump |
| `inherit` | One descendant holding output pipes after its parent exits |
| `startup-timeout` / `spawn-cancel` | Delayed readiness for timeout/cancellation tests |
| `signal` | Cooperative POSIX SIGTERM; unsupported on Windows |

The Go verifier also copies the executable into a Unicode/spaces path. `spawn-cancel` exercises cancellation before fixture readiness, not interception of the OS process creation syscall. `crash` is not a native access violation. See [limitations](docs/limitations.md).

## Optional developer verifier

Go 1.24+ is required only for this path. Build the native fixture first:

```sh
go run ./cmd/lifecase run --fixture ./build/lifecase-fixture --json reports/go.json --junit reports/go.xml
go install ./cmd/lifecase
lifecase run --fixture ./build/lifecase-fixture --scenario inherit --scenario flood
```

`go install` installs the CLI, not the native fixture. Put Go's binary installation directory on `PATH`, or invoke the installed executable by full path. Repeat `--scenario` to select cases. With no `--json`, JSON goes to stdout. Exit codes are `0` for no failed contracts, `1` for a contract failure, and `2` for configuration or execution errors. Unsupported results are JUnit skipped entries.

To evaluate a custom runner, implement the [JSON Lines adapter contract](docs/protocol-v1.md):

```sh
lifecase run --fixture ./build/lifecase-fixture --adapter ./my-runner-adapter --json reports/custom.json --junit reports/custom.xml
```

Use one `--adapter-arg` per argument. For a managed adapter you supply, use `--adapter dotnet --adapter-arg /absolute/path/to/Your.Adapter.dll`. The included CliWrap tests consume the fixture directly; they do not require this protocol. Adapter stdout is reserved for protocol messages. Adapters execute with your permissions and must be trusted.

In v0.1.3, named report files are exclusively reserved before the fixture starts.
The destination filesystem rejects symlink-parent and case aliases before any
scenario runs; collision cleanup leaves no report. Empty reservations may be
visible during a run. A hard kill may leave an empty reservation; choose fresh
names for a retry. Two completed report writes are still not atomic.

Starting with v0.1.2, JSON and JUnit output paths must be **new files**
and different from each other. Existing files (including fixture executables and
symlinks) are refused instead of overwritten. For repeated runs use a new report
directory, such as `reports/run-2/go.json` and `reports/run-2/go.xml`. A report
write error exits `2`; an earlier report may already have been written, so check
the process exit code before considering both artifacts complete. The native
fixture behavior and protocol v1 are unchanged.

For the shortest first test, run only `--scenario exit --scenario flood`, then
inspect `results[].status` and `results[].assertions` in the JSON. Both scenarios
should pass; `flood` should count 262,144 bytes on **each** output stream. Then
run the full suite before integrating the fixture into your application's CI.

The [Go example](examples/go/main.go) shows `kit.Run(ctx, kit.Options{Fixture: path, Scenarios: names})`. A nil `Runner` selects the reference runner; set `Runner` to a `runner.Runner` implementation or `runner.External` to change it. Empty `Scenarios` runs the standard list. `StartupTimeout` defaults to one second and accepts 50–1,500 ms. Handle both the returned error and `Report.Failed()`. `Report.WriteJUnit` serializes JUnit. Reports keep stable scenario/assertion names and status strings; PIDs and elapsed measurements vary.

## Safety and development

Each fixture uses synthetic data, a fresh temporary directory, a random ownership token, at most one descendant, bounded output, and an independent finite watchdog. Only owned parent processes are targeted for termination. The control plane releases the descendant even after parent exit. Watchdog expiry is a failure fallback, not successful cleanup evidence. No administrator privileges or system configuration changes are needed. Read [SECURITY.md](SECURITY.md).

```sh
ctest --test-dir build -C Release --output-on-failure
go test -race ./...
go vet ./...
```

CMake registers the native fixture test when Python 3 is available. The [CI workflow](.github/workflows) defines the platform/compiler checks. Verify the CI results for the exact commit being used and preserve reports as artifacts; a passing run is evidence for that tested configuration, not for arbitrary process trees.

Licensed under the [MIT License](LICENSE).
