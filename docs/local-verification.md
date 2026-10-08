# Local verification record

The published release's source SHA and multi-OS run are recorded in its release notes and each archive's `build-info.json`. This record describes the local development evidence, not a substitute for that release gate.

On 2026-10-08, macOS ARM64 with Apple Clang 21, Go 1.27.1 and .NET SDK 8.0.425:

- Native fixture: 16 tests, 15 pass and 1 Windows-only skip; installed-binary run 5.840 seconds. Three repeated CTest runs passed.
- Official CliWrap 3.10.5: 9 tests passed in four consecutive runs. One inheritance observation was OS parent exit at 24.480 ms and command completion at 39.196 ms, after the controlled child was released. This confirms the documented completion boundary; it is not an upstream bug report or a timing guarantee.
- Go verifier: all 11 local scenarios passed, including independent parent/pipe/descendant observations. Race detector and vet passed. The runner package passed three repeated race runs.
- Adversarial contract checks deliberately made a runner report false EOF at parent exit, report an error after its normal milestones, delay the started notification, and fail Start after creating a descendant. The harness rejects false success, tolerates valid notification latency, and retains the private control directory through cleanup.
- Official NuGet dependencies restored from the lockfile; audit reported no known vulnerabilities at that time. Source preflight found no generated source artifacts or matches for its limited credential patterns. These checks are bounded, not exhaustive audits.
- Final fixture inventories for native, Go helper and .NET local suites found no running owned test processes.

The normal consumer path is a prebuilt fixture plus the .NET test project. Building the native fixture or running the optional Go verifier adds development tooling, and is not necessary just to consume the fixture. No adoption or performance claim is inferred from these local tests.
