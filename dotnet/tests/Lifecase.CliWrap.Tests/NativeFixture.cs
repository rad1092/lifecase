using System.Diagnostics;
using System.Security.Cryptography;
using System.Text.Json;
using CliWrap;
using Xunit;

namespace Lifecase.CliWrap.Tests;

/// <summary>Fixture control-plane glue. Command execution stays entirely in the user's CliWrap library.</summary>
public sealed class NativeFixture : IAsyncDisposable
{
    public static readonly TimeSpan TestBound = TimeSpan.FromSeconds(4);
    public string DirectoryPath { get; } = Path.Combine(Path.GetTempPath(), "lifecase dotnet 한글 " + Guid.NewGuid().ToString("N"));
    public string Executable { get; }
    public string[] Arguments { get; }
    private readonly string token = Convert.ToHexString(RandomNumberGenerator.GetBytes(16)).ToLowerInvariant();
    private readonly string scenario;
    private Process? childObserver;
    private int? childPid;

    public NativeFixture(string scenario)
    {
        this.scenario = scenario;
        Executable = Environment.GetEnvironmentVariable("LIFECASE_FIXTURE")
            ?? throw new InvalidOperationException("Set LIFECASE_FIXTURE to the absolute native fixture path; these tests require real processes.");
        if (!Path.IsPathFullyQualified(Executable) || !File.Exists(Executable))
            throw new InvalidOperationException("LIFECASE_FIXTURE must name an existing absolute executable.");
        Directory.CreateDirectory(DirectoryPath);
        if (!OperatingSystem.IsWindows()) File.SetUnixFileMode(DirectoryPath, UnixFileMode.UserRead | UnixFileMode.UserWrite | UnixFileMode.UserExecute);
        Arguments = ["--scenario", scenario, "--dir", DirectoryPath, "--token", token, "--lease-ms", "5000"];
    }

    public Command Command() => Cli.Wrap(Executable).WithArguments(Arguments);
    public void Go() => File.WriteAllText(Path.Combine(DirectoryPath, "go"), token);
    public void Stop() => File.WriteAllText(Path.Combine(DirectoryPath, "stop"), token);

    /// <summary>Verifies an independently written token and PID before tests observe that process.</summary>
    public async Task ReadyAsync(int expectedPid)
    {
        var ready = await WaitControlAsync("parent-ready.json");
        Assert.Equal("parent", ready.GetProperty("role").GetString());
        Assert.Equal("ready", ready.GetProperty("event").GetString());
        Assert.Equal(expectedPid, ready.GetProperty("pid").GetInt32());
        if (scenario == "inherit")
        {
            var child = await WaitControlAsync("child-ready.json");
            Assert.Equal("child", child.GetProperty("role").GetString());
            Assert.Equal("ready", child.GetProperty("event").GetString());
            childPid = child.GetProperty("pid").GetInt32();
            childObserver = Process.GetProcessById(childPid.Value);
            // Open and retain the Windows process handle before the parent exits. This is read-only
            // and avoids treating a reused PID as the original child on later inventory checks.
            _ = childObserver.SafeHandle;
        }
    }

    public async Task ChildExitedAsync()
    {
        var exited = await WaitControlAsync("child-exit.json");
        Assert.Equal("child", exited.GetProperty("role").GetString());
        Assert.Equal("exit", exited.GetProperty("event").GetString());
        Assert.Equal(childPid, exited.GetProperty("pid").GetInt32());
        if (childObserver is null) throw new InvalidOperationException("Child readiness must be observed before cleanup.");
        await childObserver.WaitForExitAsync().WaitAsync(TestBound);
        Assert.True(childObserver.HasExited);
    }

    private async Task<JsonElement> WaitControlAsync(string name)
    {
        using var deadline = new CancellationTokenSource(TestBound);
        var path = Path.Combine(DirectoryPath, name);
        while (!File.Exists(path)) await Task.Delay(10, deadline.Token);
        using var document = JsonDocument.Parse(await File.ReadAllTextAsync(path, deadline.Token));
        Assert.Equal(1, document.RootElement.GetProperty("v").GetInt32());
        Assert.Equal(token, document.RootElement.GetProperty("token").GetString());
        return document.RootElement.Clone();
    }

    public async ValueTask DisposeAsync()
    {
        Stop();
        if (scenario == "inherit" && File.Exists(Path.Combine(DirectoryPath, "child-ready.json")))
            await ChildExitedAsync();
        childObserver?.Dispose();
        Directory.Delete(DirectoryPath, recursive: true);
    }
}

/// <summary>Streaming output sink for byte totals with a fixed retained prefix; not a growing text buffer.</summary>
public sealed class BoundedCapture(int limit) : Stream
{
    private readonly byte[] prefix = limit is >= 0 and <= 65536 ? new byte[limit]
        : throw new ArgumentOutOfRangeException(nameof(limit));
    private int used;
    public long TotalBytes { get; private set; }
    public byte[] Prefix => prefix[..used];
    public override bool CanRead => false;
    public override bool CanSeek => false;
    public override bool CanWrite => true;
    public override long Length => TotalBytes;
    public override long Position { get => TotalBytes; set => throw new NotSupportedException(); }
    public override void Flush() { }
    public override int Read(byte[] buffer, int offset, int count) => throw new NotSupportedException();
    public override long Seek(long offset, SeekOrigin origin) => throw new NotSupportedException();
    public override void SetLength(long value) => throw new NotSupportedException();
    public override void Write(byte[] buffer, int offset, int count)
    {
        var keep = Math.Min(count, prefix.Length - used);
        buffer.AsSpan(offset, keep).CopyTo(prefix.AsSpan(used));
        used += keep;
        TotalBytes = checked(TotalBytes + count);
    }
    public override ValueTask WriteAsync(ReadOnlyMemory<byte> buffer, CancellationToken cancellationToken = default)
    {
        cancellationToken.ThrowIfCancellationRequested();
        var keep = Math.Min(buffer.Length, prefix.Length - used);
        buffer.Span[..keep].CopyTo(prefix.AsSpan(used));
        used += keep;
        TotalBytes = checked(TotalBytes + buffer.Length);
        return ValueTask.CompletedTask;
    }
}
