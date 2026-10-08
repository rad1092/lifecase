# Security policy

Lifecase is a local conformance test kit. It runs native processes and external adapters with the caller's permissions. Run only fixtures and adapters whose source or release origin you trust. It is not a sandbox for unknown executables.

## Expected safety boundaries

- Use synthetic fixture data in fresh isolated temporary directories.
- Launch executables directly, without a shell command string.
- Authenticate fixture control and evidence files with a random per-run token.
- Terminate only the exact owned parent process; use the authenticated stop control to release the fixture descendant.
- Bound output retention, input writes, scenario duration, and descendant count.
- Give each native fixture process a finite independent watchdog.
- Report cleanup failures; do not conceal them by treating watchdog expiry as normal completion.

Tokens prevent accidental cross-run control. They do not protect against malicious programs with the same filesystem access as the caller. The harness does not grant extra privileges, configure kernel containment, or restrict adapter network/filesystem access.

## Reporting a vulnerability

If the repository's Security tab offers private vulnerability reporting, use it. Otherwise open an issue asking for a private reporting channel, with no exploit, credential, or private environment details. Ordinary assertion failures and platform capability requests can be public issues.

Useful information includes the commit or release, OS and runtime versions, the smallest synthetic reproducer, and redacted JSON/JUnit evidence. Do not include user data or unrelated process inventories.

During the initial release series, fixes target the latest published version. No response-time guarantee or support for older versions is implied.
