import fs from "node:fs";
import path from "node:path";
export const DEFAULT_LATER_MS = 10 * 60_000;
export const WATCH_RETRY_MS = 2_000;
export const RESPONDER = "mac-dialog";
export function log(...args) {
  const at = new Date().toTimeString().slice(0, 8);
  process.stderr.write(`[mac-dialog-decisions ${at}] ${args.join(" ")}\n`);
}
export function sleep(ms) { return new Promise(resolve => setTimeout(resolve, ms)); }
export function daemonPidPath(stateDir) { return path.join(stateDir, "daemon.json"); }
export function removeDialogFiles(specPath) {
  for (const file of [specPath, `${specPath}.result.jsonl`]) {
    try { fs.unlinkSync(file); } catch (error) { if (error.code !== "ENOENT") throw error; }
  }
}
