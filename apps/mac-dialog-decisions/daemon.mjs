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
    getDecision, // (machine, id) => authoritative saved decision
    spawnDialog, // (specPath, spec) => {lines: AsyncIterable<string>, kill()}
    rich, // local/SSH rich content and attachment storage
    specFilePath, // (key) => path to write the spec JSON
    onFatal, // fatal dialog startup failures terminate the owning run
    log: logFn = log,
  }) {
    this.config = config;
    this.rich = rich;
    this.spawnWatch = spawnWatch;
    this.runAnswer = runAnswer;
    this.getDecision = getDecision;
    this.spawnDialog = spawnDialog;
    this.specFilePath = specFilePath;
    this.onFatal = onFatal;
    this.log = logFn;
    this.entries = new Map(); // key -> entry
    this.queue = []; // keys waiting for the dialog
    this.children = new Map(); // machine + decision -> displayed window
    this.resolvedKeys = new Set();
    this.machinesDown = new Set();
    this.machineEpochs = new Map();
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
      const epoch = (this.machineEpochs.get(mid) || 0) + 1;
      this.machineEpochs.set(mid, epoch);
      void this.reconcileMachine(mid, epoch);
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

  async reconcileMachine(mid, epoch) {
    const entries = [...this.entries.values()].filter(entry => entry.machineId === mid && entry.machineDown);
    for (const entry of entries) {
      if (this.stopped || this.machinesDown.has(mid) || this.machineEpochs.get(mid) !== epoch) return;
      try {
        if (!this.getDecision) throw Error("decision lookup is unavailable");
        const decision = await this.getDecision(mid, entry.decision.decision_id);
        if (this.stopped || this.machinesDown.has(mid) || this.machineEpochs.get(mid) !== epoch) return;
        if (this.entries.get(entry.key) !== entry) continue;
        if (decisionIdOf(decision) !== entry.decision.decision_id) throw Error("decision lookup returned a different ID");
        if (["answered", "cancelled", "expired"].includes(decision.status)) this.onResolved(mid, decision);
        else if (decision.status === "pending") this.onCreated(mid, entry.machine, decision);
        else throw Error(`unknown saved decision status: ${decision.status}`);
      } catch (error) {
        this.log(`再接続時の判断待ち照合に失敗（保持します）: ${entry.decision.decision_id}: ${error.message}`);
      }
    }
    this.notify();
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
    if (this.rich) void this.rich.resolve(mid, decisionIdOf(decision), decision.status).catch(error => this.log(`添付の整理に失敗: ${error.message}`));
    const entry = this.entries.get(key);
    if (entry) {
      entry.resolved = true;
      entry.queued = false;
      clearTimeout(entry.snoozeTimer);
      clearTimeout(entry.answerRetryTimer);
      entry.snoozeTimer = null;
      entry.answerRetryTimer = null;
      entry.answerPending = false;
      entry.retry = null;
      this.queue = this.queue.filter((k) => k !== key);
      if (entry.showing) this._closeDialog(entry);
      this.entries.delete(key);
    }
    this.log(`決定済み: ${decision.decision_id} (${decision.status})`);
    this.notify();
  }

  onMachineDown(mid, reason) {
    this.machinesDown.add(mid);
    this.machineEpochs.set(mid, (this.machineEpochs.get(mid) || 0) + 1);
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

  async _present(entry) {
    entry.showing = true;
    const spec = buildDialogSpec(entry.decision, entry.machine, this.children.size, {
      laterMs: this.config.laterMs,
    });
    if (entry.draft) spec.draft = entry.draft;
    if (this.rich && spec.allow_text) {
      try { spec.draft_directory = await this.rich.store.draftDirectory(entry.machineId, entry.decision.decision_id); }
      catch (error) { entry.showing = false; this.log(`添付の準備に失敗: ${error.message}`); return; }
      if (!this.isActive(entry)) { entry.showing = false; return; }
    }
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
    child.richAbort = new AbortController();
    if (this.rich) void this.rich.load(spec, entry.machineId, child.richAbort.signal, message => { if (!child.richAbort.signal.aborted) child.respond?.(message); }).catch(error => this.log(`画像の読み込みに失敗: ${error.message}`));
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
    child.richAbort?.abort();
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
      const attachments = entry.decision.allow_text && Array.isArray(message.attachments) ? message.attachments : [];
      if (!entry.resolved && (ids.length > 0 || text || attachments.length)) {
        await this.submitAnswer(entry, { optionIds: ids, text, attachments });
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
    child.richAbort?.abort();
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

  async submitAnswer(entry, { optionIds = [], text = "", attachments = entry.draft?.attachments || [] }) {
    if (!this.isActive(entry) || entry.answerPending) return false;
    entry.answerPending = true;
    entry.draft = {option_ids: optionIds, text, ...(attachments.length ? {attachments} : {})};
    this.notify();
    const did = entry.decision.decision_id;
    const args = ["decision", "answer", did, ...optionIds, "--responder", RESPONDER, "--machine", entry.machineId];
    let answerText = text;
    this.log(`回答を送信: ${did} option=${optionIds.join(",") || "-"}${text ? " +text" : ""}`);
    let result;
    try {
      if (attachments.length) {
        if (!this.rich || !entry.decision.allow_text) throw Error("この判断待ちは画像を添付できません");
        const files = await this.rich.save(entry.machineId, did, attachments);
        answerText = [text, ...files.map(file => `添付画像: ${file}`)].filter(Boolean).join("\n");
      }
      if (!this.isActive(entry)) { entry.answerPending = false; return false; }
      if (answerText) args.push("--text", answerText);
      result = await this.runAnswer(args);
    }
    catch (error) { result = {code: 1, stderr: error.message}; }
    const detail = [result.stdout, result.stderr].filter(Boolean).join("\n").trim() || "unknown";
    if (result.code === 0 || /decision_already_resolved/.test(detail)) { this.settleAnswer(entry); return true; }
    if (entry.resolved || this.entries.get(entry.key) !== entry || this.stopped) return false;
    entry.answerPending = false;
    this.log(`回答の送信に失敗 (${did}): ${detail}`);
    if (/https:\/\/login\.tailscale\.com\//.test(detail)) {
      entry.retry ??= {optionIds, text, attachments, attempts: 0, deadline: Date.now() + ANSWER_RETRY_MS * ANSWER_RETRY_LIMIT};
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
