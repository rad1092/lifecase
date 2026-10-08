# Native fixture

The C++17 fixture creates real OS process and pipe behavior for runner tests. It
does not supervise arbitrary programs or terminate any other process. Build and
install with:

```sh
cmake -S . -B build/native -DCMAKE_BUILD_TYPE=Release
cmake --build build/native --config Release
ctest --test-dir build/native -C Release --output-on-failure
cmake --install build/native --config Release --prefix build/install
```

CTest runs `fixture/tests.py` when Python 3 is available. These tests start the
actual compiled executable, including from a path containing spaces and Korean
characters. They exercise independent stdout/stderr drains, blocked writes,
readiness cancellation, inherited pipes, signal handling, and hard watchdogs.
An exact child PID inventory checks that no owned child remains live after each
test. An exited POSIX zombie awaiting the OS orphan reaper is classified as
exited; no PID-based descendant termination is used.

Use the [versioned protocol](../docs/protocol-v1.md) to integrate a runner. The
fixture requires a fresh absolute control directory. On POSIX it must be owned
by the invoking user with no group or world permissions (normally mode 0700).
Do not reuse a control directory between runs. Control files must contain the
matching 32-character lowercase hexadecimal token; an optional final newline
is accepted. Symlink control files and non-regular files are ignored.

The fixture returns `64` for invalid input or control-plane errors, `74` for a
failed output write, `78` for the unsupported Windows signal scenario, and `124`
when its hard lease expires. The `crash` scenario intentionally uses immediate
exit code `23` without a crash dump. A hard lease is a cleanup fallback and must
never count as successful graceful cleanup.

`child-exit.json` is written just before the child exits. It is a cooperative
cleanup milestone, not proof that the OS process has already disappeared. Keep
parent exit, pipe EOF, this milestone, and any OS inventory observations separate.

POSIX descendants use `posix_spawn`, so an active watchdog never creates a
post-fork C++ execution path. Windows descendants use `CreateProcessW` with an
explicit executable path, Unicode arguments, runtime-compatible argument
quoting, and a restricted list of duplicated standard handles. The Windows
console signal scenario is explicitly unsupported in protocol 1.

Native API references:

- [Apple `posix_spawn` documentation](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/posix_spawn.2.html)
- [Microsoft `CreateProcessW` documentation](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-createprocessw)
- [Microsoft C command-line parsing rules](https://learn.microsoft.com/en-us/cpp/c-language/parsing-c-command-line-arguments)
