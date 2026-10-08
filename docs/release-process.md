# Release verification

Lifecase is a testing asset: prebuilt controlled child fixtures, a versioned file
protocol, and regression tests that consume the official CliWrap NuGet package.
The Go verifier is a development and CI tool. The workflow does not publish to
NuGet, install system services, or create releases automatically.

## What the CI gate checks

The `lifecycle-contracts` workflow uses standard public GitHub hosted runners:

| Runner | Fixture compiler | Archive target | Runtime checks |
| --- | --- | --- | --- |
| Ubuntu 24.04 x64 | GCC | `linux-x64` | Python, Go 1.26, .NET 8 / CliWrap |
| macOS 15 arm64 | Apple Clang | `macos-arm64` | Python, Go 1.26, .NET 8 / CliWrap |
| Windows Server 2022 x64 | MSVC | `windows-x64` | Python, Go 1.26, .NET 8 / CliWrap |
| Ubuntu 24.04 x64 | Clang | none | Native fixture contracts |

Each main runner installs the CMake build to a staging prefix, runs the native
fixture contracts twice, runs Go tests with and without the race detector, runs
`go vet`, installs and executes the Go CLI twice against the staged fixture, and
builds and tests the real CliWrap integration. Dependencies are restored in locked
mode with NuGet's direct and transitive vulnerability auditing enabled; the
project treats warnings as errors. Windows console signaling is explicitly unsupported in protocol 1; that
result is not a successful signal test. A source preflight checks the Git source
inventory for generated files, common credential patterns, unpinned package
references, and unpinned remote Actions. It is a narrow check, not a complete
credential or vulnerability audit.

The `release-gate` job succeeds only when all platform, compiler, and source jobs
succeed. Results from an earlier commit do not qualify a later commit for release.
The workflow token is read-only and checkout does not persist Git credentials.
All Actions are pinned to full upstream commit SHAs.
Runner architecture and public-runner availability are checked against
[GitHub's hosted runner reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).

## Verify a proposed release

1. Set consistent version numbers in CMake, Go, the .NET test metadata, and the
   changelog. Keep protocol compatibility separate from product versions.
2. Review the dependency lock changes, licenses, source preflight output, and
   documented limitations. A closed upstream issue is a regression class, not
   evidence of a current upstream bug.
3. Push the proposed commit. Record its full SHA and the matching Actions run URL.
   Require all jobs, including `release-gate`, to pass for that exact SHA.
4. Download the three fixture artifacts and their reports from that run. Each
   artifact contains a ZIP and a SHA-256 sidecar. Each ZIP contains the installed
   fixture, MIT license, protocol document, file checksums, and source commit
   metadata. Check that the metadata commit matches the proposed release SHA.
5. Inspect JSON/JUnit and .NET TRX results. Report failures, unsupported scenarios,
   and unrun platforms separately. Timing values are observed evidence rather
   than guaranteed latency thresholds across arbitrary machines.
6. After review, create a version tag at that exact commit and publish the three
   ZIPs and their checksum files as release assets. Do not rebuild binaries on a
   different machine and describe them as the tested artifacts.

## Install smoke tests

The CI smoke tests execute the installed fixture and then the extracted release
ZIP through the complete Go scenario verifier:

```sh
cmake -S . -B build/native -DCMAKE_BUILD_TYPE=Release
cmake --build build/native --config Release --parallel 2
cmake --install build/native --config Release --prefix build/install
go install ./cmd/lifecase
lifecase run --fixture ./build/install/bin/lifecase-fixture \
  --json reports/contracts.json --junit reports/contracts.xml
```

On Windows use `lifecase-fixture.exe`. The CLI must be on `PATH`; alternatively,
set `GOBIN` to a temporary absolute directory and execute it from there. For a
downloaded Unix fixture, preserve ZIP executable permissions or apply `chmod +x`
to that one extracted binary. No global installation or administrator permission
is required. macOS local download quarantine policy remains in force; Lifecase
does not change security settings or claim signed/notarized distribution.

## Evidence and cleanup

Artifacts retain only bounded JSON/JUnit reports, .NET TRX, and fixture archives
for 14 days. Reports use synthetic fixtures and no user data. All scenarios have
bounded fixture lifetimes. The verifier checks token-owned descendant cleanup
using the OS inventory; tests never use broad process-name kill commands.

Keep source tests and small curated measurements in Git. Build trees, downloaded
SDKs, package caches, reports, and archives stay ignored. Only remove generated
files owned by this project after preserving the evidence needed for the release.
Local success does not establish Linux or Windows support; only the matching
real runner results do. Untested OS/architecture combinations remain unrun.
