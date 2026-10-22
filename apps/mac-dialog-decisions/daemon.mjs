import fs from "node:fs";
import path from "node:path";
import { DEFAULT_LATER_MS, WATCH_RETRY_MS, RESPONDER, log, sleep, daemonPidPath, removeDialogFiles } from "./common.mjs";
import { buildDialogSpec, decisionKey, decisionIdOf } from "./spec.mjs";

const ANSWER_RETRY_MS = 15_000;
const ANSWER_RETRY_LIMIT = 120;

export class MacDialogDecisionsDaemon {
  constructor({
    config,
    spawnWatch, // () => {lines: AsyncIterable<object>, close()}
    runAnswer, // (argv: string[]) => Promise<{code, stdout, stderr}>
    spawnDialog, // (specPath, spec) => {lines: AsyncIterable<string>, kill()}
    specFilePath, // (key) => path to write the spec JSON
    onFatal, // fatal dialog startup failures terminate the owning run
    log: logFn = log,
  }) {
    this.config = config;
    this.spawnWatch = spawnWatch;
    this.runAnswer = runAnswer;
    this.spawnDialog = spawnDialog;
    this.specFilePath = specFilePath;
    this.onFatal = onFatal;
    this.log = logFn;
    this.entries = new Map(); // key -> entry
    this.queue = []; // keys waiting for the dialog
    this.children = new Map(); // machine + decision -> displayed window
    this.resolvedKeys = new Set();
    this.machinesDown = new Set();
    this.stopped = false;
    this._pumpTimer = null;
    this._watchHandle = null;
    this._watchDone = null;
    this._dialogDone = null;
  }

  async start() {
    this._watchDone = this.watchLoop();
  }

  notify() { this.onStateChange?.(); }

  async stop() {
    this.stopped = true;
    if (this._pumpTimer) clearTimeout(this._pumpTimer);
    this._watchHandle?.close?.();
    this._closeDialog();
    for (const entry of this.entries.values()) {
      clearTimeout(entry.snoozeTimer); clearTimeout(entry.answerRetryTimer);
    }
    await Promise.allSettled([this._watchDone, this._dialogDone]);
  }

  // A short delay before taking the screen lets a burst of simultaneous
  // decisions coalesce so the first dialog reports the true queue size.
  schedulePump() {
    if (this.stopped) return;
    // reset on each new event so a burst coalesces into one presentation
    clearTimeout(this._pumpTimer);
    this._pumpTimer = setTimeout(() => {
      this._pumpTimer = null;
      this.pump();
    }, this.config.presentDelayMs ?? 0);
  }

  // ---- watch stream ---------------------------------------------------------

  async watchLoop() {
    while (!this.stopped) {
      const watch = await this.spawnWatch();
      this._watchHandle = watch;
      try {
        for await (const line of watch.lines) {
          if (this.stopped) break;
          try {
            this.onWatchLine(line);
          } catch (error) {
            this.log(`watch 行の処理に失敗: ${error.message}`);
          }
        }
      } catch (error) {
        this.log(`decision watch が切れました: ${error.message}`);
      }
      if (this.stopped) break;
      this.log("decision watch を再接続します");
      await sleep(WATCH_RETRY_MS);
    }
  }

  onWatchLine(line) {
    const event = line?.event;
    const machine = line?.machine || {};
    const mid = String(machine.id || "local");
    const decision = line?.decision || {};
    const did = decisionIdOf(decision);

    if (event === "machine_up") {
      this.machinesDown.delete(mid);
      return;
    }
    if (event === "machine_down") {
      this.onMachineDown(mid, line?.reason || "");
      return;
    }
    if (event === "created" && did) {
      this.onCreated(mid, machine, decision);
      return;
    }
    if (event === "resolved" && did) {
      this.onResolved(mid, decision);
    }
  }

  onCreated(mid, machine, decision) {
    const key = decisionKey(mid, decisionIdOf(decision));
    if (this.resolvedKeys.has(key)) return;
    const existing = this.entries.get(key);
    if (existing) {
      // Reconnect replay, or a repeat created for a decision whose dialog
      // crashed. A dialog put off with あとで comes back on its own timer.
      existing.machineDown = false;
      existing.decision = decision;
      if (
        !existing.showing &&
        !existing.queued &&
        !existing.dismissed &&
        !existing.resolved &&
        !existing.answerPending &&
        !existing.retry
      ) {
        existing.queued = true;
        this.queue.push(key);
        this.schedulePump();
        this.notify();
      }
      return;
    }
    if (decision.status && decision.status !== "pending") return;

    const entry = {
      key,
      machineId: mid,
      machine,
      decision,
      queued: false,
      showing: false,
      dismissed: false,
      resolved: false,
      machineDown: false,
      answerPending: false,
    };
    this.entries.set(key, entry);
    if (this.machinesDown.has(mid)) {
      this.log(`${mid} は切断中: ${decision.decision_id} のダイアログは出しません`);
      entry.machineDown = true;
      return;
    }
    entry.queued = true;
    this.queue.push(key);
    this.log(`判断待ちをキューに追加: ${decision.decision_id} (queue=${this.queue.length})`);
    this.schedulePump();
    this.notify();
  }

  onResolved(mid, decision) {
    const key = decisionKey(mid, decisionIdOf(decision));
    this.resolvedKeys.add(key);
    const entry = this.entries.get(key);
    if (entry) {
      entry.resolved = true;
      entry.queued = false;
      clearTimeout(entry.snoozeTimer);
      clearTimeout(entry.answerRetryTimer);
      this.queue = this.queue.filter((k) => k !== key);
      if (entry.showing) this._closeDialog(entry);
      this.entries.delete(key);
    }
    this.log(`決定済み: ${decision.decision_id} (${decision.status})`);
    this.notify();
  }

  onMachineDown(mid, reason) {
    this.machinesDown.add(mid);
    for (const entry of [...this.entries.values()]) {
      if (entry.machineId !== mid) continue;
      entry.machineDown = true;
      entry.queued = false;
      this.queue = this.queue.filter((k) => k !== entry.key);
      if (entry.showing) this._closeDialog(entry);
    }
    this.log(`マシン切断: ${mid} (${reason})`);
    this.notify();
  }

  // ---- dialog presentation ----------------------------------------------------

  pump() {
    if (this.stopped) return;
    while (this.queue.length > 0) {
      const key = this.queue.shift();
      const entry = this.entries.get(key);
      if (!entry || entry.resolved || entry.dismissed || entry.machineDown || entry.answerPending || entry.retry) {
        continue;
      }
      entry.queued = false;
      this._present(entry);
    }
  }

  _present(entry) {
    const spec = buildDialogSpec(entry.decision, entry.machine, this.children.size, {
      laterMs: this.config.laterMs,
    });
    if (entry.draft) spec.draft = entry.draft;
    let specPath = "";
    try {
      specPath = this.specFilePath(entry.key);
      spec.result_path = `${specPath}.result.jsonl`;
      spec.daemon_pid = process.pid;
      spec.daemon_owner = this.config.daemonOwner;
      spec.daemon_ownership_path = daemonPidPath(this.config.stateDir);
      fs.mkdirSync(path.dirname(specPath), { recursive: true });
      fs.writeFileSync(specPath, JSON.stringify(spec), { mode: 0o600 });
    } catch (error) {
      this.log(`spec ファイルの書き込みに失敗 (${entry.decision.decision_id}): ${error.message}`);
      return;
    }
    entry.showing = true;
    this.log(`ダイアログを表示: ${entry.decision.decision_id}`);
    const child = this.spawnDialog(specPath, spec);
    child.entry = entry;
    child.specPath = specPath;
    child.handled = false;
    this.children.set(entry.key, child);
    this._dialogDone = Promise.allSettled([this._dialogDone, this._consumeChild(child)]);
    this.refreshWindowCounts();
    this.notify();
  }

  async _consumeChild(child) {
    try {
      for await (const line of child.lines) {
        if (child.handled || this.stopped) continue;
        let message;
        try {
          message = JSON.parse(line);
        } catch {
          this.log(`ダイアログの JSON でない行を捨てました: ${String(line).slice(0, 120)}`);
          continue;
        }
        if (message?.event === "shown") continue;
        if (message?.event === "draft") { child.entry.draft = message.draft; continue; }
        if (typeof message?.result !== "string") continue;
        child.handled = true;
        await this._handleResult(child, message);
      }
    } catch (error) {
      this.log(`ダイアログプロセスの読み取りに失敗: ${error.message}`);
    }
    // exited
    const entry = child.entry;
    const outcome = await child.exited;
    removeDialogFiles(child.specPath);
    entry.showing = false;
    if (this.children.get(entry.key) === child) this.children.delete(entry.key);
    this.refreshWindowCounts();
    this.notify();
    if (outcome?.error) {
      this.log(`パネルを起動できないため停止します: ${outcome.error.code}`);
      void this.stop();
      this.onFatal?.(outcome.error);
      return;
    }
    if (!child.handled && !entry.machineDown && !entry.resolved && !this.stopped && !entry.answerPending && !entry.retry) {
      if (child.closedByDaemon) { entry.queued = true; this.queue.push(entry.key); }
      else this.snooze(entry);
    }
    this.schedulePump();
    this.notify();
  }

  async _handleResult(child, message) {
    const entry = child.entry;
    if (message.result === "answer") {
      const ids = Array.isArray(message.option_ids)
        ? message.option_ids.filter((id) => typeof id === "string" && id)
        : [];
      const text = typeof message.text === "string" ? message.text.trim() : "";
      if (!entry.resolved && (ids.length > 0 || text)) {
        await this.submitAnswer(entry, { optionIds: ids, text });
      }
      return;
    }
    if (["later", "dismiss", "closed"].includes(message.result)) {
      this.snooze(entry);
    }
  }

  // あとで: hide now, show again after laterMs if nobody has answered by then.
  snooze(entry) {
    entry.dismissed = true;
    const laterMs = this.config.laterMs ?? DEFAULT_LATER_MS;
    this.log(
      `あとで: ${entry.decision.decision_id} を ${Math.round(laterMs / 60_000)} 分後にもう一度出します`,
    );
    clearTimeout(entry.snoozeTimer);
    entry.snoozeTimer = setTimeout(() => {
      entry.snoozeTimer = null;
      entry.dismissed = false;
      if (this.stopped || entry.resolved || entry.machineDown) return;
      if (this.entries.get(entry.key) !== entry) return;
      if (!entry.queued && !entry.showing) {
        entry.queued = true;
        this.queue.push(entry.key);
        this.schedulePump();
        this.notify();
      }
    }, laterMs);
    entry.snoozeTimer.unref?.();
    this.notify();
  }

  _closeDialog(entry) {
    const children = entry ? [this.children.get(entry.key)].filter(Boolean) : [...this.children.values()];
    for (const child of children) {
    child.closedByDaemon = true;
    this.log(`ダイアログを閉じます: ${child.entry.decision.decision_id}`);
    // The panel watches its spec file and exits as soon as it disappears;
    // the signal is the fast path.
    try {
      fs.unlinkSync(child.specPath);
    } catch {}
    try {
      child.kill();
    } catch {}
    }
  }

  refreshWindowCounts() {
    for (const child of this.children.values()) child.respond?.({status: "windows", queue_remaining: this.children.size - 1});
  }

  settleAnswer(entry) {
    clearTimeout(entry.answerRetryTimer);
    entry.answerPending = false;
    entry.retry = null;
    this.onResolved(entry.machineId, {...entry.decision, status: "answered"});
  }

  // ---- answering ---------------------------------------------------------------

  isActive(entry) {
    return !this.stopped && !entry.resolved && !entry.machineDown &&
      !this.machinesDown.has(entry.machineId) && this.entries.get(entry.key) === entry;
  }

  async submitAnswer(entry, { optionIds = [], text = "" }) {
    if (!this.isActive(entry) || entry.answerPending) return false;
    entry.answerPending = true;
    entry.draft = {option_ids: optionIds, text};
    this.notify();
    const did = entry.decision.decision_id;
    const args = ["decision", "answer", did, ...optionIds, "--responder", RESPONDER, "--machine", entry.machineId];
    if (text) args.push("--text", text);
    this.log(`回答を送信: ${did} option=${optionIds.join(",") || "-"}${text ? " +text" : ""}`);
    let result;
    try { result = await this.runAnswer(args); }
    catch (error) { result = {code: 1, stderr: error.message}; }
    const detail = [result.stdout, result.stderr].filter(Boolean).join("\n").trim() || "unknown";
    if (result.code === 0 || /decision_already_resolved/.test(detail)) { this.settleAnswer(entry); return true; }
    if (entry.resolved || this.entries.get(entry.key) !== entry || this.stopped) return false;
    entry.answerPending = false;
    this.log(`回答の送信に失敗 (${did}): ${detail}`);
    if (/https:\/\/login\.tailscale\.com\//.test(detail)) {
      entry.retry ??= {optionIds, text, attempts: 0, deadline: Date.now() + ANSWER_RETRY_MS * ANSWER_RETRY_LIMIT};
      entry.retry.attempts++;
      this._closeDialog(entry);
      this.scheduleAnswerRetry(entry);
      this.notify();
      return false;
    }
    const child = this.children.get(entry.key);
    entry.retry = null;
    if (child?.entry === entry) {
      child.handled = false;
      child.respond({status: "error", message: `回答を送信できませんでした。再回答できます。\n${detail}`});
    } else if (!entry.machineDown && !entry.queued) {
      entry.queued = true;
      this.queue.push(entry.key);
      this.schedulePump();
      this.notify();
    }
    return false;
  }

  scheduleAnswerRetry(entry) {
    const retry = entry.retry;
    if (!retry || this.stopped || entry.resolved) return;
    if (retry.attempts >= ANSWER_RETRY_LIMIT || Date.now() >= retry.deadline) {
      entry.retry = null;
      if (!entry.machineDown && !entry.queued) { entry.queued = true; this.queue.push(entry.key); this.schedulePump(); }
      return;
    }
    clearTimeout(entry.answerRetryTimer);
    entry.answerRetryTimer = setTimeout(() => {
      entry.answerRetryTimer = null;
      if (entry.resolved || this.stopped || this.entries.get(entry.key) !== entry) return;
      if (!this.isActive(entry)) { this.scheduleAnswerRetry(entry); return; }
      void this.submitAnswer(entry, retry);
    }, this.config.answerRetryMs ?? ANSWER_RETRY_MS);
    entry.answerRetryTimer.unref?.();
  }

}
