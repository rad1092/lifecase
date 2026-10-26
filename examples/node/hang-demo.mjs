// A CI step in miniature: start a tool, wait for it to exit, print "done".
// Run with LIFECASE_FIXTURE set. The tool exits at once, yet this Node process
// only ends when the tool's descendant releases the inherited pipes.
import { spawn } from "node:child_process";
import { prepare } from "./lifecase.mjs";

const started = Date.now();
const ms = () => `${String(Date.now() - started).padStart(5)} ms`;
const run = prepare("inherit", { leaseMs: 5000 });
const child = spawn(run.command, run.args);
child.stdout.pipe(process.stdout);
child.stderr.pipe(process.stderr);
child.on("exit", (code) => console.log(`${ms()}  tool exited with ${code}; the step looks finished`));
child.on("close", () => console.log(`${ms()}  pipes closed; only now can Node exit`));
process.on("exit", () => console.log(`${ms()}  node process ends`));
await run.ready();
run.go();
// Nothing writes stop: the descendant keeps the pipes until its 5 s watchdog,
// standing in for a background server or watcher a build script left running.
