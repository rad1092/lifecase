// What Node's child_process reports when the process you started exits but a
// process it started still holds stdout and stderr. This is the usual reason a
// CI step prints its last line and then never finishes.
import assert from "node:assert/strict";
import { execFile, spawn } from "node:child_process";
import { once } from "node:events";
import test from "node:test";
import { setTimeout as sleep } from "node:timers/promises";
import { prepare } from "./lifecase.mjs";

const posix = process.platform !== "win32";

function started(run) {
  const child = spawn(run.command, run.args);
  child.stdout.resume();
  child.stderr.resume();
  return child;
}

test("'exit' fires at once, 'close' waits for the inherited pipes", async () => {
  const run = prepare("inherit");
  try {
    const child = started(run);
    let closed = false;
    const close = once(child, "close").then(() => { closed = true; });
    await run.ready();
    run.go();
    const [code] = await once(child, "exit");
    assert.equal(code, 0);
    await sleep(500);
    assert.equal(closed, false, "close must not fire while the descendant holds the pipes");
    run.stop();
    await close;
  } finally {
    await run.cleanup();
  }
});

test("execFile's callback waits for the descendant, not the process it started", async () => {
  const run = prepare("inherit");
  try {
    let stopWritten = 0;
    const done = new Promise((resolve) => {
      const child = execFile(run.command, run.args, (error) => resolve({ error, at: Date.now() }));
      child.once("exit", () => setTimeout(() => { stopWritten = Date.now(); run.stop(); }, 500));
    });
    await run.ready();
    run.go();
    const { error, at } = await done;
    assert.equal(error, null);
    assert.ok(stopWritten > 0 && at >= stopWritten, "the callback only ran after the descendant was told to stop");
  } finally {
    await run.cleanup();
  }
});

test("execFile's timeout still reports success, but only when it fires", async () => {
  const run = prepare("inherit");
  try {
    const started = Date.now();
    const done = new Promise((resolve) => {
      execFile(run.command, run.args, { timeout: 700 }, (error) => resolve({ error, ms: Date.now() - started }));
    });
    await run.ready();
    run.go();
    const { error, ms } = await done;
    // The process exited with 0 long before; Node closed the pipes on timeout
    // and reports a normal success. Nothing says the output may be incomplete.
    assert.equal(error, null);
    assert.ok(ms >= 700, `callback after ${ms} ms`);
  } finally {
    await run.cleanup();
  }
});

test("a bounded policy: drain briefly after exit, then end the process group", { skip: !posix && "process groups are POSIX-only" }, async () => {
  const run = prepare("inherit");
  let killed = false;
  try {
    const child = spawn(run.command, run.args, { detached: true });
    child.stdout.resume();
    child.stderr.resume();
    const close = once(child, "close");
    await run.ready();
    run.go();
    await once(child, "exit");
    const grace = setTimeout(() => { killed = true; process.kill(-child.pid, "SIGKILL"); }, 200);
    const startedWaiting = Date.now();
    await close;
    clearTimeout(grace);
    assert.ok(Date.now() - startedWaiting < 2000, "close arrived within the grace period");
    assert.equal(run.descendantExited(), false, "the descendant was ended, it did not finish its own cleanup");
  } finally {
    await run.cleanup({ waitForDescendant: !killed });
  }
});
