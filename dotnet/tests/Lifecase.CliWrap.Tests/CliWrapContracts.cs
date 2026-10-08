using System.Diagnostics;
using System.Text;
using CliWrap;
using Xunit;
using Xunit.Abstractions;

[assembly: CollectionBehavior(DisableTestParallelization = true)]

namespace Lifecase.CliWrap.Tests;

public sealed class CliWrapContracts(ITestOutputHelper output)
{
    [Fact]
    public async Task CommandCompletionIncludesDescendantHeldPipeDrain()
    {
        await using var fixture = new NativeFixture("inherit");
        using var force = new CancellationTokenSource(NativeFixture.TestBound);
        using var stdout = new BoundedCapture(64);
        using var stderr = new BoundedCapture(64);
        var stopwatch = Stopwatch.StartNew();
        var command = fixture.Command()
            .WithStandardOutputPipe(PipeTarget.ToStream(stdout))
            .WithStandardErrorPipe(PipeTarget.ToStream(stderr));
        var running = command.ExecuteAsync(force.Token);
        try
        {
            await fixture.ReadyAsync(running.ProcessId);
            // Read-only observation of a PID provided by CliWrap and independently checked against
            // the private fixture token. All termination stays with CliWrap's owned lifetime.
            using var parent = Process.GetProcessById(running.ProcessId);
            fixture.Go();
            await parent.WaitForExitAsync().WaitAsync(NativeFixture.TestBound);
            var parentExitMs = stopwatch.Elapsed.TotalMilliseconds;
            Assert.True(parent.HasExited); // ExitCode is available from CliWrap after drain, not this observer on every OS.
            Assert.False(running.Task.IsCompleted);
            Assert.False(File.Exists(Path.Combine(fixture.DirectoryPath, "child-exit.json")));
            output.WriteLine($"parent_exit_ms={parentExitMs:F3}; command_complete_at_parent_exit=false; descendant_released=false");
            fixture.Stop();
            Assert.Equal(0, (await running.Task.WaitAsync(NativeFixture.TestBound)).ExitCode);
            await fixture.ChildExitedAsync();
            output.WriteLine($"command_complete_ms={stopwatch.Elapsed.TotalMilliseconds:F3}; descendant_cleanup=pass; watchdog_used=false");
        }
        finally { fixture.Stop(); force.Cancel(); await ObserveCancellationAsync(running.Task); }
    }

    [Fact]
    public async Task ConcurrentDualOutputDoesNotDeadlockAndRetentionIsBounded()
    {
        await using var fixture = new NativeFixture("flood");
        using var force = new CancellationTokenSource(NativeFixture.TestBound);
        using var stdout = new BoundedCapture(127);
        using var stderr = new BoundedCapture(127);
        var running = fixture.Command()
            .WithStandardOutputPipe(PipeTarget.ToStream(stdout))
            .WithStandardErrorPipe(PipeTarget.ToStream(stderr))
            .ExecuteAsync(force.Token);
        try
        {
            await fixture.ReadyAsync(running.ProcessId);
            fixture.Go();
            Assert.Equal(0, (await running.Task.WaitAsync(NativeFixture.TestBound)).ExitCode);
            Assert.Equal(262144, stdout.TotalBytes);
            Assert.Equal(262144, stderr.TotalBytes);
            Assert.Equal(new string('O', 127), Encoding.UTF8.GetString(stdout.Prefix));
            Assert.Equal(new string('E', 127), Encoding.UTF8.GetString(stderr.Prefix));
            output.WriteLine("stdout_bytes=262144; stderr_bytes=262144; retained_bytes=127_each; command_complete=pass");
        }
        finally { fixture.Stop(); force.Cancel(); await ObserveCancellationAsync(running.Task); }
    }

    [Fact]
    public async Task CancellingDotnetWaitDoesNotCancelCliWrapButForceTokenDoes()
    {
        await using var fixture = new NativeFixture("uncooperative");
        using var force = new CancellationTokenSource(NativeFixture.TestBound);
        var running = fixture.Command().ExecuteAsync(force.Token);
        try
        {
            await fixture.ReadyAsync(running.ProcessId);
            using var parent = Process.GetProcessById(running.ProcessId);
            fixture.Go();
            using var waitCancellation = new CancellationTokenSource(TimeSpan.FromMilliseconds(70));
            await Assert.ThrowsAnyAsync<OperationCanceledException>(() => parent.WaitForExitAsync(waitCancellation.Token));
            Assert.False(parent.HasExited);
            Assert.False(running.Task.IsCompleted);
            force.Cancel();
            await Assert.ThrowsAnyAsync<OperationCanceledException>(() => running.Task.WaitAsync(NativeFixture.TestBound));
            await parent.WaitForExitAsync().WaitAsync(NativeFixture.TestBound);
            Assert.True(parent.HasExited);
            output.WriteLine("wait_cancellation_killed_parent=false; cliwrap_force_cancellation_killed_parent=true");
        }
        finally { force.Cancel(); await ObserveCancellationAsync(running.Task); }
    }

    [Fact]
    public async Task BlockedStdinStillAllowsForceCancellation()
    {
        await using var fixture = new NativeFixture("stdin-blocked");
        using var force = new CancellationTokenSource(NativeFixture.TestBound);
        var running = fixture.Command()
            .WithStandardInputPipe(PipeSource.FromBytes(new byte[1_048_576]))
            .WithStandardOutputPipe(PipeTarget.ToStream(Stream.Null))
            .WithStandardErrorPipe(PipeTarget.ToStream(Stream.Null))
            .ExecuteAsync(force.Token);
        try
        {
            await fixture.ReadyAsync(running.ProcessId);
            using var parent = Process.GetProcessById(running.ProcessId);
            fixture.Go();
            Assert.False(running.Task.IsCompleted);
            force.Cancel();
            await Assert.ThrowsAnyAsync<OperationCanceledException>(() => running.Task.WaitAsync(NativeFixture.TestBound));
            await parent.WaitForExitAsync().WaitAsync(NativeFixture.TestBound);
            output.WriteLine("stdin_source_bytes=1048576; forced_cancellation=pass; parent_exit=pass");
        }
        finally { force.Cancel(); await ObserveCancellationAsync(running.Task); }
    }

    [Fact]
    public async Task CancellingOneCommandLeavesIndependentOwnedCommandAlive()
    {
        await using var first = new NativeFixture("uncooperative");
        await using var second = new NativeFixture("cooperative");
        using var firstForce = new CancellationTokenSource(NativeFixture.TestBound);
        using var secondForce = new CancellationTokenSource(NativeFixture.TestBound);
        var a = first.Command().ExecuteAsync(firstForce.Token);
        var b = second.Command().ExecuteAsync(secondForce.Token);
        try
        {
            await first.ReadyAsync(a.ProcessId);
            await second.ReadyAsync(b.ProcessId);
            using var other = Process.GetProcessById(b.ProcessId);
            firstForce.Cancel();
            await Assert.ThrowsAnyAsync<OperationCanceledException>(() => a.Task.WaitAsync(NativeFixture.TestBound));
            Assert.False(other.HasExited);
            Assert.False(b.Task.IsCompleted);
            second.Stop();
            Assert.Equal(0, (await b.Task.WaitAsync(NativeFixture.TestBound)).ExitCode);
            output.WriteLine("canceled_command=exited; independent_owned_command=alive_until_cooperative_stop");
        }
        finally
        {
            first.Stop(); second.Stop(); firstForce.Cancel(); secondForce.Cancel();
            await ObserveCancellationAsync(a.Task); await ObserveCancellationAsync(b.Task);
        }
    }

    [Fact]
    public async Task CooperativeStopSucceedsWithoutCancellation()
    {
        await using var fixture = new NativeFixture("cooperative");
        using var force = new CancellationTokenSource(NativeFixture.TestBound);
        var running = fixture.Command().ExecuteAsync(force.Token);
        try
        {
            await fixture.ReadyAsync(running.ProcessId);
            fixture.Go();
            fixture.Stop();
            Assert.Equal(0, (await running.Task.WaitAsync(NativeFixture.TestBound)).ExitCode);
            Assert.False(force.IsCancellationRequested);
            output.WriteLine("shutdown=cooperative_control_file; forced=false; exit_code=0");
        }
        finally { fixture.Stop(); force.Cancel(); await ObserveCancellationAsync(running.Task); }
    }

    [Fact]
    public async Task CancellationBeforeNativeReadinessRemainsBounded()
    {
        await using var fixture = new NativeFixture("spawn-cancel");
        using var force = new CancellationTokenSource(NativeFixture.TestBound);
        var running = fixture.Command().ExecuteAsync(force.Token);
        try
        {
            using var parent = Process.GetProcessById(running.ProcessId);
            force.Cancel();
            await Assert.ThrowsAnyAsync<OperationCanceledException>(() => running.Task.WaitAsync(NativeFixture.TestBound));
            await parent.WaitForExitAsync().WaitAsync(NativeFixture.TestBound);
            Assert.True(parent.HasExited);
            Assert.False(File.Exists(Path.Combine(fixture.DirectoryPath, "parent-ready.json")));
            output.WriteLine("cancel_phase=after_process_start_before_ready; command_canceled=pass; parent_exit=pass");
        }
        finally { force.Cancel(); await ObserveCancellationAsync(running.Task); }
    }

    [Fact]
    public async Task AbruptExitCanBeInspectedWhenResultValidationIsExplicit()
    {
        await using var fixture = new NativeFixture("crash");
        using var force = new CancellationTokenSource(NativeFixture.TestBound);
        var running = fixture.Command().WithValidation(CommandResultValidation.None).ExecuteAsync(force.Token);
        try
        {
            await fixture.ReadyAsync(running.ProcessId);
            fixture.Go();
            Assert.Equal(23, (await running.Task.WaitAsync(NativeFixture.TestBound)).ExitCode);
            output.WriteLine("abrupt_exit_code=23; validation=None; command_complete=pass");
        }
        finally { fixture.Stop(); force.Cancel(); await ObserveCancellationAsync(running.Task); }
    }

    [Fact]
    public async Task UnicodeAndSpacesExecutablePathUsesLibraryArgumentFormatting()
    {
        await using var fixture = new NativeFixture("exit");
        var copy = Path.Combine(fixture.DirectoryPath, "fixture 한글 space" + (OperatingSystem.IsWindows() ? ".exe" : ""));
        File.Copy(fixture.Executable, copy);
        if (!OperatingSystem.IsWindows()) File.SetUnixFileMode(copy, UnixFileMode.UserRead | UnixFileMode.UserWrite | UnixFileMode.UserExecute);
        using var force = new CancellationTokenSource(NativeFixture.TestBound);
        using var stdout = new BoundedCapture(8);
        var running = Cli.Wrap(copy).WithArguments(fixture.Arguments)
            .WithStandardOutputPipe(PipeTarget.ToStream(stdout)).ExecuteAsync(force.Token);
        try
        {
            await fixture.ReadyAsync(running.ProcessId);
            fixture.Go();
            Assert.Equal(0, (await running.Task.WaitAsync(NativeFixture.TestBound)).ExitCode);
            Assert.Equal("ok\n", Encoding.UTF8.GetString(stdout.Prefix));
            output.WriteLine("unicode_executable_path=pass; spaced_directory=pass; stdout_bytes=3");
        }
        finally { fixture.Stop(); force.Cancel(); await ObserveCancellationAsync(running.Task); }
    }

    private static async Task ObserveCancellationAsync(Task task)
    {
        try { await task.WaitAsync(NativeFixture.TestBound); }
        catch (OperationCanceledException) { }
    }
}
