# Lifecase 0.1.2 portable fixture and verifier

Verify this archive's SHA-256 sidecar before extracting. It contains the native
`lifecase-fixture` and the optional `lifecase` verifier for the named platform.
Both use `.exe` on Windows. No Go or .NET runtime is needed for these executables.

From the extracted directory on macOS/Linux:

```sh
./lifecase version
./lifecase run --fixture ./lifecase-fixture --scenario exit --scenario flood --json reports/first.json --junit reports/first.xml
```

Expect version `0.1.2`, exit `0` and two passing results. PowerShell uses
`.\lifecase.exe` and `.\lifecase-fixture.exe`. Remove both `--scenario` selections
to run the full suite. Windows reports POSIX `signal` as unsupported/skipped.
Reports must be new, distinct files; choose another directory for each run.
Exit `1` is a failed lifecycle assertion; exit `2` is invalid configuration or
an execution/report error. Do not treat partially written reports as success.

To test your own application, use the
[CliWrap integration guide](https://github.com/rad1092/lifecase/blob/v0.1.2/dotnet/README.md)
or the included `protocol-v1.md`. The fixture is controlled synthetic input,
not a replacement for your application's runner or cancellation policy.
See [limitations](https://github.com/rad1092/lifecase/blob/v0.1.2/docs/limitations.md).
The binaries are not claimed to be signed or notarized; follow your normal
local execution policy. No service or global installation is required.
