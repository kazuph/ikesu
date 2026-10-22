import { spawn } from "node:child_process";
import path from "node:path";
import readline from "node:readline";

// A single AppKit process owns all windows and therefore one Dock identity.
export class NativeDialogHost {
  constructor(binary, directory, log = () => {}) {
    this.binary = binary;
    this.directory = directory;
    this.log = log;
    this.windows = new Map();
    this.child = null;
  }

  start() {
    if (this.child) return;
    const child = spawn(this.binary, ["--host", this.directory], {stdio: ["pipe", "pipe", "pipe"]});
    this.child = child;
    let error;
    child.on("error", e => { error = e; this.log(`native host: ${e.message}`); });
    child.stdin.on("error", e => this.log(`native host input: ${e.message}`));
    child.stderr.on("data", bytes => this.log(String(bytes).trim()));
    const lines = readline.createInterface({input: child.stdout});
    lines.on("line", line => {
      let event;
      try { event = JSON.parse(line); } catch { this.log(`invalid native host event: ${line}`); return; }
      const window = this.windows.get(event.key);
      if (!window) return;
      if (event.event === "closed") {
        window.finish({code: event.error ? 1 : 0, error: event.error ? Error(event.error) : undefined});
        this.windows.delete(event.key);
      } else window.push(line);
    });
    this.exited = new Promise(resolve => child.once("close", (code, signal) => {
      lines.close();
      for (const window of this.windows.values()) window.finish({code, signal, error: error || (code ? Error(`native host exited ${code}`) : undefined)});
      this.windows.clear();
      if (this.child === child) this.child = null;
      resolve({code, signal, error});
    }));
  }

  send(message) {
    if (this.child && !this.child.stdin.destroyed) this.child.stdin.write(JSON.stringify(message)+"\n");
  }

  open(specPath) {
    const key = path.basename(specPath);
    if (this.windows.has(key)) throw Error(`duplicate native window ${key}`);
    const queue = [];
    let wake, ended = false, resolveExit;
    const exited = new Promise(resolve => { resolveExit = resolve; });
    const handle = {
      exited,
      push(line) { queue.push(line); wake?.(); wake = null; },
      finish(result) { ended = true; resolveExit(result); wake?.(); wake = null; },
      lines: (async function* () {
        while (!ended || queue.length) {
          if (queue.length) { yield queue.shift(); continue; }
          await new Promise(resolve => { wake = resolve; });
        }
      })(),
      respond: message => this.send({...message, key}),
      kill: () => this.send({op: "close", key}),
    };
    this.windows.set(key, handle);
    this.start();
    this.send({op: "open", path: specPath, key});
    return handle;
  }

  async stop() {
    this.send({op: "stop"});
    this.child?.stdin.end();
    await this.exited;
  }
}
