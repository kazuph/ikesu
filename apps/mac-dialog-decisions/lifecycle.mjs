import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { daemonPidPath } from "./common.mjs";

export function releaseVersion(root) {
  try { return fs.readFileSync(path.join(root, "VERSION"), "utf8").trim(); }
  catch (error) { if (error.code !== "ENOENT") throw error; return "development"; }
}

export function readOwner(stateDir) {
  try { return JSON.parse(fs.readFileSync(daemonPidPath(stateDir), "utf8")); }
  catch (error) { if (error.code !== "ENOENT") throw error; return null; }
}

export function ownerAlive(owner) {
  if (!owner || !Number.isSafeInteger(owner.pid) || owner.pid <= 0) return false;
  try { process.kill(owner.pid, 0); return true; } catch (error) { return error.code === "EPERM"; }
}

export function ownerProven(owner, stateDir) {
  if (!ownerAlive(owner) || !owner.node || !owner.script) return false;
  try {
    const command = execFileSync("ps", ["-o", "command=", "-p", String(owner.pid)], {encoding: "utf8"}).trim();
    return owner.stateDir === fs.realpathSync(stateDir) && command === `${owner.node} ${owner.script} run`;
  } catch { return false; }
}

export function writeJSON(file, data) {
  const temporary = `${file}.${process.pid}.tmp`;
  fs.writeFileSync(temporary, JSON.stringify(data), {mode: 0o600});
  fs.renameSync(temporary, file);
}

export function requestSwitch(stateDir, root) {
  writeJSON(path.join(stateDir, "switch-request.json"), {root, requested: Date.now()});
}

export function cancelSwitch(stateDir) {
  try { fs.unlinkSync(path.join(stateDir, "switch-request.json")); }
  catch (error) { if (error.code !== "ENOENT") throw error; }
}

export function manageDaemon(daemon, config, {root, registeredRoot, onSwitch, log = () => {}}) {
  const requestPath = path.join(config.stateDir, "switch-request.json");
  let checking = false, stopped = false, switchCount = 0;
  const snapshot = () => {
    const entries = [...daemon.entries.values()];
    return {pid: process.pid, owner: config.daemonOwner, root, version: releaseVersion(root),
      windows: daemon.children.size, pending: entries.length,
      snoozed: entries.filter(e => e.dismissed).length,
      in_flight: entries.filter(e => e.answerPending).length,
      retries: entries.filter(e => e.retry).length,
      switches: switchCount, all_machines: true};
  };
  const publish = () => writeJSON(path.join(config.stateDir, "daemon-state.json"), snapshot());
  async function check() {
    if (checking || stopped) return;
    checking = true;
    try {
      let requested;
      try { requested = JSON.parse(fs.readFileSync(requestPath, "utf8")); }
      catch (error) { if (error.code === "ENOENT") return; throw error; }
      const registered = await registeredRoot();
      if (registered === root) { cancelSwitch(config.stateDir); return; }
      if (!registered || requested.root !== registered) return;
      const state = snapshot();
      if (state.windows || state.pending || state.snoozed || state.in_flight || state.retries) return;
      const last = JSON.parse(fs.readFileSync(requestPath, "utf8"));
      if (last.root !== requested.root || await registeredRoot() !== requested.root) return;
      const finalState = snapshot();
      if (finalState.windows || finalState.pending || finalState.snoozed || finalState.in_flight || finalState.retries) return;
      switchCount++;
      publish();
      stopped = true;
      await onSwitch(requested.root);
    } catch (error) { log(`版の切替確認に失敗（停止しません）: ${error.message}`); }
    finally { checking = false; }
  }
  daemon.onStateChange = () => { try { publish(); } catch (error) { log(`状態の保存に失敗: ${error.message}`); } void check(); };
  const watcher = fs.watch(config.stateDir, (_event, name) => { if (name === "switch-request.json") void check(); });
  daemon.onStateChange();
  return {snapshot, check, stop() { stopped = true; watcher.close(); daemon.onStateChange = null; }};
}
