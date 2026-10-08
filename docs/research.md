# Why a lifecycle conformance kit?

Research checked on 2026-10-08. These are primary-source reports and documented semantics, not a survey of user demand or a claim that every current runtime has the reported defects.

## The boundary worth testing

Subprocess APIs expose several different completion events. Treating one of them as a universal "done" is a source of hangs, truncated output, and incomplete cleanup. The useful contract is explicit: observe the parent exit, finish reading both streams, and establish what happened to descendants and pending writes.

| Primary evidence | What it establishes | Lifecase implication |
| --- | --- | --- |
| [Go issue #22485](https://github.com/golang/go/issues/22485), opened in 2017, **closed** | Historical `CommandContext` examples with shell descendants exceeded the apparent timeout in older Go versions. | Keep a repeatable inherited-pipe regression class. Do not label current Go as having this unresolved bug. |
| [Current Go `Cmd.WaitDelay` documentation](https://pkg.go.dev/os/exec#Cmd) | `Wait` may be delayed by a child that does not exit or by I/O pipes that remain open. `WaitDelay` can terminate the child and close pipes to bound this delay. | Report forced pipe closure separately from actual EOF. A bounded `Wait` alone is not descendant cleanup evidence. |
| [.NET `Process.Kill` documentation](https://learn.microsoft.com/en-us/dotnet/api/system.diagnostics.process.kill?view=net-10.0) | `Kill` is asynchronous. `WaitForExit` and `HasExited` concern the associated process even when tree termination is requested. | Assert parent and descendant milestones independently. |
| [.NET issue #107992](https://github.com/dotnet/runtime/issues/107992), opened in 2024, **closed** | A reported Windows Job Object topology caused a descendant to survive tree termination. The issue links [fix #128598](https://github.com/dotnet/runtime/pull/128598). | Preserve the broader descendant-cleanup regression class. Lifecase v1 does not recreate that Job Object topology or claim the historical defect remains. |
| [Watchexec command documentation](https://github.com/watchexec/watchexec/blob/main/doc/watchexec.1.md#command) | Stop signals, timeouts, and process wrapping vary by platform; its documented Windows stop path supports force termination rather than Unix-style graceful signaling. | Expose capabilities and mark native Windows console signaling unsupported in v1. |

The need follows from these documented distinctions: authors of build runners, test executors, local development tools, and job workers need to verify their own interpretation of completion after changing a runtime, platform, or process library. This is a technical fit assessment. It does not establish market size or commercial demand.

## Existing tools and direct overlap

| Tool | Existing responsibility | Relationship to Lifecase |
| --- | --- | --- |
| [CliWrap](https://github.com/Tyrrrz/CliWrap) | A .NET command library with async piping, cancellation, ownership of process lifetime, and tests on Windows/Linux/macOS. | The first package integration example. Its substantial existing tests make a second generic .NET process wrapper difficult to justify. |
| [process-wrap](https://docs.rs/process-wrap/latest/process_wrap/) | Composable wrappers around Rust `Command` for std/Tokio, including Unix groups/sessions and Windows Job Objects. | An implementation a user can test through an adapter. Lifecase does not replace its wrappers or claim superior process control. |
| [Watchexec](https://github.com/watchexec/watchexec/blob/main/doc/watchexec.1.md) | Execute/restart commands on events; configure stop signals, timeout, and group/session/Job Object wrapping. | Provides supervision policy. Lifecase supplies controlled fixtures and assertions for evaluating such policies. |
| [libuv process API](https://docs.libuv.org/en/v1.x/process.html) | Spawn processes, configure stdio inheritance, receive exit callbacks, and signal process handles. | Supplies runtime building blocks. An adapter can translate their observations into the shared contract. |
| [pytest-subprocess](https://pytest-subprocess.readthedocs.io/en/latest/) | Register fake subprocess behavior without launching the real process. | Useful for deterministic application unit tests. Lifecase instead exercises actual OS pipe inheritance, process exit, and backpressure. Both layers can be useful. |

This comparison concerns documented purpose and selected source tests, not an exhaustive inventory. No claim is made that Lifecase is the first or only lifecycle test tool.

## Negative evidence: CliWrap already tests this area

CliWrap's [execution tests](https://github.com/Tyrrrz/CliWrap/blob/prime/CliWrap.Tests/ExecutionSpecs.cs) cover immediate, delayed, and graceful cancellation, owned-process termination, and large simultaneous output. Its [piping tests](https://github.com/Tyrrrz/CliWrap/blob/prime/CliWrap.Tests/PipingSpecs.cs) cover partial or absent stdin consumption, failed pipe sources/targets, and merged-target EOF behavior. The [project workflow](https://github.com/Tyrrrz/CliWrap/blob/prime/.github/workflows/main.yml) uses a [shared workflow](https://github.com/Tyrrrz/.github/blob/prime/.github/workflows/nuget.yml) with Linux, Windows, and macOS test jobs. Lifecase cannot justify itself by saying these contracts go untested upstream.

CliWrap [3.10.4](https://github.com/Tyrrrz/CliWrap/releases/tag/3.10.4) shipped process-lifetime ownership changes; [3.10.5](https://github.com/Tyrrrz/CliWrap/releases/tag/3.10.5) fixed a merged pipe target deadlock when a target read past EOF. Those are released fixes, not known-unfixed defects or claims that this fixture reproduces their exact regressions. They support keeping package-upgrade tests, but do not establish demand for a new framework.

## Narrow product decision and install reason

The public asset is a small controlled native fixture with a .NET/CliWrap integration example and an explicit lifecycle policy. Go remains the kit's optional developer verifier and report generator. The example uses the real CliWrap NuGet package, with reusable fixture and bounded-capture test helpers rather than a new process wrapper.

The plausible installation reason is specific: a team has its own completion/cancellation policy around a pinned process library, needs an inherited-pipe or blocked-I/O case in its application CI, and wants to reuse the same bounded native input without copying another project's entire dummy-program test infrastructure. The fixture's ready/stop side channel lets the test coordinate a descendant independently of the parent's exit. This is a hypothesis about integration convenience, not evidence of external adoption.

There is a real setup cost: obtain a native executable for each CI architecture, maintain the fixture path and version, add the .NET test package dependencies, and decide what cleanup and completion mean in the application. A custom JSON Lines adapter adds further work and should only be used when the shared report contracts are useful. The CLI and generic adapter are optional for the direct .NET fixture tests.

Skip Lifecase when upstream defaults and existing integration tests meet your needs, when a fake process is sufficient, or when the requirement is production supervision. Use the upstream library for execution. Broader ecosystem support should wait for an actual consumer and a concrete missing scenario; the source review does not justify a large universal conformance framework.

## Deliberate exclusions

Lifecase v1 does not provide a daemon, shell, scheduler, arbitrary-PID cleanup tool, process-tree containment system, or sandbox. It does not reproduce every historical issue or promise identical platform semantics. Small fixed fixtures, independent watchdogs, and explicit unsupported results keep the scope reviewable. See [limitations](limitations.md) and [protocol v1](protocol-v1.md).
