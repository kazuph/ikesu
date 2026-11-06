import { randomUUID, createHash } from 'node:crypto';
import fs from 'node:fs/promises';
import path from 'node:path';
import { MacDialogDecisionsDaemon } from '../../mac-dialog-decisions/daemon.mjs';
import { buildDialogSpec } from '../../mac-dialog-decisions/spec.mjs';
import { displayedByFocusedClient } from '../../mac-dialog-decisions/client-views.mjs';
import { RICH_LIMITS } from '../../mac-dialog-decisions/rich.mjs';

/** Reuse the Mac daemon's watch and machine reconciliation, without opening windows. */
export class DecisionRegistry extends MacDialogDecisionsDaemon {
  constructor(options) {
    super(options);
    this.publicIds = new Map();
    this.submissions = new Map();
    this.onSnapshot = () => {};
    this.snapshotRevision = 0;
    this.onStateChange = () => {
      const revision = ++this.snapshotRevision;
      void this.snapshot().then(value => { if (revision === this.snapshotRevision) this.onSnapshot(value); }).catch(error => this.log(error.message));
    };
  }
  schedulePump() {
    for (const key of this.queue.splice(0)) {
      const entry = this.entries.get(key);
      if (entry) entry.queued = false;
    }
  }
  refreshBackgroundWindows() { this.notify(); }
  onCreated(machineId, machine, decision) {
    super.onCreated(machineId, machine, decision);
    const entry = [...this.entries.values()].find(item => item.machineId === machineId && item.decision.decision_id === decision.decision_id);
    if (entry && !this.publicIds.has(entry.key)) {
      this.publicIds.set(entry.key, createHash('sha256').update(entry.key).digest('base64url'));
      entry.media = { images: [], cards: [], videos: [] };
      entry.mediaAbort = new AbortController();
      if (this.rich) void this.rich.load(buildDialogSpec(decision, machine), machineId, entry.mediaAbort.signal, event => {
        void this.recordMedia(entry, event).catch(error => this.log(`表示素材を読めません: ${error.message}`));
      }).catch(error => this.log(`表示素材の取得に失敗: ${error.message}`));
    }
    this.notify();
  }
  async recordMedia(entry, event) {
    if (entry.mediaAbort.signal.aborted) return;
    if (event.status === 'rich_image') entry.media.images[event.index] = (await fs.readFile(event.path)).toString('base64');
    if (event.status === 'rich_card') entry.media.cards[event.index] = { title: event.title, url: event.url, site: event.site,
      image: event.path ? (await fs.readFile(event.path)).toString('base64') : '' };
    if (event.status === 'rich_video') entry.media.videos[event.index] = { key: randomUUID(), path: event.path };
    if (!entry.mediaAbort.signal.aborted) this.notify();
  }
  onResolved(machineId, decision) {
    const entry = [...this.entries.values()].find(item => item.machineId === machineId && item.decision.decision_id === decision.decision_id);
    entry?.mediaAbort?.abort();
    super.onResolved(machineId, decision);
    for (const [key] of this.publicIds) if (!this.entries.has(key)) this.publicIds.delete(key);
    this.notify();
  }
  async snapshot() {
    let views = [];
    try { if (this.clientViews) views = await this.clientViews.list(); } catch { /* Notify if lookup fails. */ }
    const entries = [...this.entries.values()];
    return entries.map(entry => ({
      ...buildDialogSpec(entry.decision, entry.machine, entries.length - 1, { laterMs: this.config.laterMs }),
      raw_title: String(entry.decision.title || ''),
      raw_body: String(entry.decision.body || ''),
      question_label: String(entry.decision.question_label || ''),
      id: this.publicIds.get(entry.key),
      machine_down: entry.machineDown,
      reauth_url: entry.retry?.url || '',
      retry_error: entry.retryError || '',
      notify: !entry.dismissed && !displayedByFocusedClient(entry, this.clientViews, views),
      images: entry.media?.images || [],
      cards: entry.media?.cards || [],
      videos: (entry.media?.videos || []).map(video => video ? { key: video.key } : null),
    })).filter(entry => entry.id);
  }
  videoPath(id, key) { return this.find(id)?.media?.videos.find(video => video?.key === key)?.path; }
  find(id) {
    const found = [...this.publicIds].find(([, publicId]) => publicId === id);
    return found && this.entries.get(found[0]);
  }
  async later(id) {
    const entry = this.find(id);
    if (!entry) throw Error('判断待ちが見つかりません');
    this.snooze(entry);
  }
  async answer(message) {
    const fingerprint = createHash('sha256').update(JSON.stringify(message)).digest('hex');
    const previous = this.submissions.get(message.sequence);
    if (previous) {
      if (previous.fingerprint !== fingerprint) throw Error('送信番号が別の回答に使われています');
      const prior = await previous.result;
      if (prior.status === 'unknown' && previous.output) {
        previous.result = this.checkSubmission(previous);
      }
      return previous.result;
    }
    const entry = this.find(message.id);
    if (!entry) throw Error('判断待ちが見つかりません');
    if (entry.machineDown) throw Error('出どころのマシンに接続できません');
    if (entry.retry) throw Error('SSH の再承認待ちです');
    entry.retryError = '';
    if (entry.answerPending) throw Error('回答を送信中です');
    const allowed = new Set((entry.decision.options || []).map(option => String(option.id)));
    if (new Set(message.options).size !== message.options.length || message.options.some(option => !allowed.has(option))) throw Error('選択肢が不正です');
    if (!entry.decision.multi_select && message.options.length > 1) throw Error('複数選択できません');
    if (!entry.decision.allow_text && (message.text || message.images.length)) throw Error('文と画像を添付できません');
    if (!message.options.length && !message.text && !message.images.length) throw Error('回答が空です');
    entry.answerPending = true;
    const submission = { fingerprint, machineId: entry.machineId, decisionId: entry.decision.decision_id, output: null };
    const work = { message, hasImages: message.images.length > 0, savedText: null };
    submission.result = this.submit(entry, work, submission).finally(() => { entry.answerPending = false; this.notify(); });
    this.submissions.set(message.sequence, submission);
    return submission.result;
  }
  async submit(entry, work, submission) {
    try { return await this.submitSteps(entry, work, submission); }
    catch (error) {
      const authorization = String(error.message).match(/https:\/\/login\.tailscale\.com\/[^\s]+/);
      if (!authorization) throw error;
      const firstRetry = !entry.retry;
      entry.retry ??= { submission, work, url: authorization[0], attempts: 0, deadline: Date.now() + 15_000 * 120 };
      entry.retry.url = authorization[0];
      if (firstRetry) this.scheduleSSHRetry(entry);
      this.notify();
      return { status: 'reauth', message: 'SSH の再承認が必要です', url: authorization[0] };
    }
  }
  async submitSteps(entry, work, submission) {
    const message = work.message;
    const id = entry.decision.decision_id;
    const args = ['decision', 'answer', id, ...message.options, '--machine', entry.machineId, '--responder', 'android'];
    let text = work.savedText ?? message.text;
    if (work.hasImages && work.savedText === null) {
      if (!this.rich) throw Error('画像の保存先を利用できません');
      const draft = await this.rich.store.draftDirectory(entry.machineId, id);
      const files = [];
      try {
        for (const encoded of message.images) {
          const bytes = Buffer.from(encoded, 'base64');
          if (bytes.length > RICH_LIMITS.attachmentBytes) throw Error('添付は1枚20MBまでです');
          const extension = bytes.subarray(0, 8).equals(Buffer.from('89504e470d0a1a0a', 'hex')) ? '.png' :
            bytes.subarray(0, 3).equals(Buffer.from('ffd8ff', 'hex')) ? '.jpg' :
            ['GIF87a', 'GIF89a'].includes(bytes.subarray(0, 6).toString('ascii')) ? '.gif' :
            bytes.subarray(0, 4).toString('ascii') === 'RIFF' && bytes.subarray(8, 12).toString('ascii') === 'WEBP' ? '.webp' : null;
          if (!extension) throw Error('画像形式を確認できません');
          const file = path.join(draft, randomUUID() + extension);
          await fs.writeFile(file, bytes, { flag: 'wx', mode: 0o600 });
          files.push({ path: file });
        }
        const saved = await this.rich.save(entry.machineId, id, files, new AbortController().signal);
        text = [text, ...saved.map(file => `添付画像: ${file}`)].filter(Boolean).join('\n');
        work.savedText = text;
        work.message = { ...message, images: [] };
      } finally { await Promise.allSettled(files.map(file => fs.rm(file.path, { force: true }))); }
    }
    if (text) args.push('--text', text);
    const before = await this.getDecision(entry.machineId, id);
    if (before.status !== 'pending') {
      if (work.hasImages && ['answered', 'cancelled', 'expired'].includes(before.status))
        await this.rich.resolve(entry.machineId, id, before.status);
      if (before.status === 'answered') return { status: 'answered_elsewhere' };
      return { status: before.status, message: '判断待ちは既に終了しています' };
    }
    let output;
    try { output = await this.runAnswer(args); } catch (error) { output = { code: 1, stderr: error.message }; }
    const authorization = `${output.stderr || ''}\n${output.stdout || ''}`.match(/https:\/\/login\.tailscale\.com\/[^\s]+/);
    if (output.code !== 0 && authorization) {
      throw Error(authorization[0]);
    }
    const detail = `${output.stderr || ''}\n${output.stdout || ''}`;
    submission.output = { code: output.code, alreadyResolved: /decision_already_resolved/.test(detail),
      failure: output.code === 0 ? '' : String(output.stderr || output.stdout || '回答を保存できませんでした') };
    return this.checkSubmission(submission);
  }
  async checkSubmission(submission) {
    const result = await this.checkOutcome(submission.machineId, submission.decisionId, submission.output);
    if (result.status !== 'unknown') delete submission.output;
    return result;
  }
  async checkOutcome(machineId, decisionId, output) {
    let saved;
    try { saved = await this.getDecision(machineId, decisionId); }
    catch (error) { return { status: 'unknown', message: `保存結果を確認できません: ${error.message}` }; }
    if (saved.status === 'answered') {
      if (output.code === 0) return { status: 'answered' };
      if (saved.answer?.responder !== 'android' || output.alreadyResolved) return { status: 'answered_elsewhere' };
      return { status: 'unknown', message: '保存済みの回答は確認できましたが、今回の送信かを特定できません' };
    }
    if (saved.status === 'cancelled' || saved.status === 'expired') return { status: saved.status };
    return { status: 'failed', message: output.failure || '回答を保存できませんでした' };
  }
  scheduleSSHRetry(entry) {
    const retry = entry.retry;
    if (!retry || this.stopped || entry.resolved) return;
    if (retry.attempts >= 120 || Date.now() >= retry.deadline) {
      entry.retry = null;
      entry.retryError = 'SSH の再承認が時間内に完了しませんでした。もう一度回答できます。';
      this.notify();
      return;
    }
    entry.answerRetryTimer = setTimeout(async () => {
      entry.answerRetryTimer = null;
      if (this.stopped || entry.resolved || this.entries.get(entry.key) !== entry) return;
      retry.attempts++;
      if (!entry.machineDown) {
        try {
          const submission = retry.submission;
          const result = await (submission.result = this.submit(entry, retry.work, submission));
          if (result.status !== 'reauth') {
            entry.retry = null;
            if (result.status === 'failed' || result.status === 'unknown') entry.retryError = result.message;
            this.notify();
            return;
          }
        } catch (error) {
          entry.retry = null;
          entry.retryError = `回答を送信できませんでした: ${error.message}`;
          this.notify();
          return;
        }
      }
      this.scheduleSSHRetry(entry);
    }, 15_000);
    entry.answerRetryTimer.unref?.();
  }
}
