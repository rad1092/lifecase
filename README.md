# Lifecase

**Small native fixtures for testing subprocess completion policies.**

Use a controlled child process to check what your application means by "finished": parent exit, actual stdout/stderr EOF, descendant cleanup, and graceful or forced shutdown. The first integration example targets .NET and the real CliWrap package. A Go verifier produces separate lifecycle assertions and JSON/JUnit evidence for fixture development or custom adapters.

Lifecase is useful when your application adds its own cancellation or output policy and you want repeatable native inputs in its CI. CliWrap already has extensive lifecycle tests; this project does not replace those tests or its process implementation. [Research and fit](docs/research.md) explains the narrow use case, existing alternatives, and adoption uncertainty.

[한국어 시작 가이드](docs/quickstart.ko.md) · [Protocol v1](docs/protocol-v1.md) · [Limits and capabilities](docs/limitations.md) · [.NET guide](dotnet/README.md)

## Run the .NET contracts

You need the .NET 8 SDK and a native `lifecase-fixture` executable for your OS/architecture. Use the matching asset from [Releases](https://github.com/rad1092/lifecase/releases), or build it from this checkout with CMake 3.16+ and a C++17 compiler:

```sh
cmake -S . -B build
cmake --build build --config Release
```

From the repository root on macOS/Linux:

```sh
export LIFECASE_FIXTURE="$PWD/build/lifecase-fixture"
dotnet test dotnet/tests/Lifecase.CliWrap.Tests -c Release --logger trx
```

For a Visual Studio build on Windows, use PowerShell:

```powershell
$env:LIFECASE_FIXTURE = (Resolve-Path ./build/Release/lifecase-fixture.exe).Path
dotnet test dotnet/tests/Lifecase.CliWrap.Tests -c Release --logger trx
```

For a downloaded executable, set `LIFECASE_FIXTURE` to its absolute path instead. Other CMake generators may place the binary in a different build subdirectory. The native executable and .NET contracts are enough for this workflow; Go is optional.

Release targets are `linux-x64`, `macos-arm64`, and `windows-x64`, named `lifecase-fixture-TARGET.zip` with a SHA-256 sidecar. Verify the archive checksum before extracting it. Source builds on other configurations remain unverified until their tests run. The [release guide](docs/release-process.md) covers artifact contents and install checks. macOS binaries are not claimed to be signed or notarized; use a local source build if your download policy prevents execution.

The [.NET guide](dotnet/README.md) describes the CliWrap 3.10.5 package integration, reusable test helpers, and individual contracts. Copy the relevant test policy into your application's integration suite, pin the fixture/package versions, and run on each platform you ship. `--logger trx` stores .NET test results; the optional Go verifier below produces JSON/JUnit.

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
