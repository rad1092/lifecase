// Launch arguments and the control channel for one Lifecase fixture run.
// The fixture is the native lifecase-fixture executable from a release archive
// or a CMake build; LIFECASE_FIXTURE holds its absolute path.
import { randomBytes } from "node:crypto";
import { existsSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { isAbsolute, join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

export function fixturePath() {
  const path = process.env.LIFECASE_FIXTURE;
  if (!path || !isAbsolute(path)) {
    throw new Error("Set LIFECASE_FIXTURE to the absolute path of lifecase-fixture.");
  }
  return path;
}

async function until(check, timeoutMs, what) {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error(`${what} did not happen within ${timeoutMs} ms`);
    await sleep(10);
  }
}

// prepare("inherit") returns the command line plus ready/go/stop controls.
// mkdtemp creates the private 0700 directory the fixture requires.
export function prepare(scenario, { leaseMs = 5000 } = {}) {
  const dir = mkdtempSync(join(tmpdir(), "lifecase-node-"));
  const token = randomBytes(16).toString("hex");
  const write = (name) => writeFileSync(join(dir, name), token);
  return {
    command: fixturePath(),
    args: ["--scenario", scenario, "--dir", dir, "--token", token, "--lease-ms", String(leaseMs)],
    // Resolves once the fixture (and, for inherit, its descendant) is ready.
    ready: () => until(() => existsSync(join(dir, "parent-ready.json")), 5000, `${scenario} readiness`),
    // Lets the scenario run: for inherit the parent then exits at once.
    go: () => write("go"),
    // Asks the descendant to release the pipes and exit.
    stop: () => write("stop"),
    descendantExited: () => existsSync(join(dir, "child-exit.json")),
    // Stop the descendant, wait for it (bounded by its lease), remove the
    // directory. Skip the wait when the test already killed the descendant.
    async cleanup({ waitForDescendant = true } = {}) {
      write("stop");
      if (waitForDescendant && existsSync(join(dir, "child-ready.json"))) {
        await until(() => existsSync(join(dir, "child-exit.json")), leaseMs + 1000, "descendant exit").catch(() => {});
      }
      rmSync(dir, { recursive: true, force: true });
    },
  };
}
