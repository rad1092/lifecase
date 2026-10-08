# CliWrap lifecycle contracts

This small .NET 8 test asset runs the official [CliWrap 3.10.5 NuGet package](https://www.nuget.org/packages/CliWrap/3.10.5)
against the native Lifecase fixture. It tests an existing subprocess library with
real OS processes. It does not publish another process wrapper or NuGet package.

From the repository root, after building or downloading `lifecase-fixture`:

```sh
export LIFECASE_FIXTURE="$PWD/build/lifecase-fixture"
dotnet restore dotnet/tests/Lifecase.CliWrap.Tests/Lifecase.CliWrap.Tests.csproj --locked-mode
dotnet test dotnet/tests/Lifecase.CliWrap.Tests/Lifecase.CliWrap.Tests.csproj \
  -c Release --no-restore --logger trx --results-directory reports/dotnet
```

PowerShell uses `$env:LIFECASE_FIXTURE = (Resolve-Path 'build/Release/lifecase-fixture.exe').Path`.
Adjust the path if you selected another CMake build directory or downloaded a release binary.
Tests fail with a setup error if the fixture is absent; they never silently replace
it with a fake or count unexecuted OS behavior as passing. The suite is serial,
uses at most two parents and one descendant, and bounds each native process with
a 5-second lease and each expected operation with a 4-second deadline.

To isolate the pipe inheritance reproduction:

```sh
dotnet test dotnet/tests/Lifecase.CliWrap.Tests/Lifecase.CliWrap.Tests.csproj \
  -c Release --filter FullyQualifiedName~CommandCompletionIncludesDescendantHeldPipeDrain \
  --logger 'console;verbosity=detailed'
```

The test holds the fixture at a token-checked ready barrier, captures a read-only
parent process observer, and releases the parent. It asserts that the parent has
exited while CliWrap's command task remains pending because the child still owns
the output pipes. It then releases the child through `stop` and requires successful
command completion, a matching child exit marker, and an OS process-exit observation.
The marker alone is not process inventory evidence. Per-run timings appear in the
test output and TRX report; they are observations, not deterministic constants.
This passing reproduction documents a lifecycle boundary, not an allegation that
CliWrap has a current bug.

## Connect your existing CliWrap configuration

Copy [`NativeFixture.cs`](tests/Lifecase.CliWrap.Tests/NativeFixture.cs) into your xUnit
test project, or adapt its small control-plane helper. Keep your actual library
configuration at the call site:

```csharp
await using var fixture = new NativeFixture("flood");
using var stop = new CancellationTokenSource(NativeFixture.TestBound);
using var stdout = new BoundedCapture(127);
using var stderr = new BoundedCapture(127);
var command = Cli.Wrap(fixture.Executable).WithArguments(fixture.Arguments)
    .WithStandardOutputPipe(PipeTarget.ToStream(stdout))
    .WithStandardErrorPipe(PipeTarget.ToStream(stderr));
var running = command.ExecuteAsync(stop.Token);
try
{
    await fixture.ReadyAsync(running.ProcessId);
    fixture.Go();
    await running.Task.WaitAsync(NativeFixture.TestBound);
    Assert.Equal(262144, stdout.TotalBytes);
    Assert.Equal(262144, stderr.TotalBytes);
}
finally
{
    fixture.Stop();
    stop.Cancel();
    try { await running.Task.WaitAsync(NativeFixture.TestBound); }
    catch (OperationCanceledException) { }
}
```

`BoundedCapture` is a streaming sink that counts all bytes and retains at most the
specified prefix. It is intended for one pipe writer per instance. It avoids an
unbounded string builder, handles output without line breaks, and leaves command
execution, argument formatting, pipe copying and cancellation to CliWrap.

## Executable policies

| Contract | Expected result |
| --- | --- |
| Parent exits with a pipe-holding child | OS parent exit precedes CliWrap command completion; explicit child stop permits drain and independently verified child exit |
| Simultaneous stdout and stderr | 262,144 bytes on each; only 127 retained on each |
| `.NET Process.WaitForExitAsync` cancellation | Cancels that wait; the fixture and CliWrap command remain alive |
| CliWrap force cancellation | Command raises `OperationCanceledException` after observed parent exit |
| Blocked stdin | A 1 MiB source does not prevent bounded force cancellation |
| Ownership isolation | Cancelling one command leaves a separate owned fixture alive |
| Cooperative shutdown | Token-owned `stop` ends the fixture successfully with no cancellation |
| Cancel during startup | Cancellation after process creation and before readiness ends the actual parent |
| Abrupt exit | Explicit `CommandResultValidation.None` exposes code 23 |
| Unicode and spaces | The fixture runs from a copied executable path containing Korean text and spaces |

Native console graceful cancellation is **unsupported by this test asset**.
CliWrap's graceful token sends an interrupt equivalent to Ctrl+C; the fixture's
POSIX `signal` scenario handles SIGTERM. The portable `stop` test is cooperative
application control, not a test of CliWrap's graceful token. Windows console
attachment and control-event behavior are not claimed to match POSIX. OS process
creation itself is synchronous in .NET and cannot be interrupted midway; the
startup test covers the period after creation and before native readiness.

CliWrap's `PipeTarget.Null` intentionally does not open the corresponding output
pipe. Use `PipeTarget.ToStream(Stream.Null)` when discarding data but retaining pipe
semantics. These tests use real sinks for the inherited-pipe contract.

No test sends a signal to an arbitrary PID or implements its own process-tree killer.
`Process.GetProcessById` is used only for read-only observation of the current
CliWrap-owned parent or token-checked child; process termination is requested via
that command's cancellation token. Descendants are released through their private
control directory, and a native watchdog is only a last-resort bound. A watchdog
exit is never accepted as cooperative cleanup.

The lockfile pins test tooling and CliWrap; consumers may deliberately change the
CliWrap reference and regenerate the lockfile to run this contract against their
installed version. Tests have only run on an OS when the corresponding CI evidence
is green; the source's platform support alone is not execution evidence.

Official behavior references: [CliWrap cancellation and piping](https://github.com/Tyrrrz/CliWrap#timeout-and-cancellation),
[Process.WaitForExitAsync](https://learn.microsoft.com/en-us/dotnet/api/system.diagnostics.process.waitforexitasync),
[Process.Kill and descendants](https://learn.microsoft.com/en-us/dotnet/api/system.diagnostics.process.kill).
The closed [.NET runtime issue 107992](https://github.com/dotnet/runtime/issues/107992)
is historical context for a regression class, not a claim that its exact Windows
job-object topology is reproduced here or remains unresolved.
