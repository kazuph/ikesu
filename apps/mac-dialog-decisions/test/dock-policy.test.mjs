import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const exec = promisify(execFile);

// Compiling AppKit and observing two one-second deadlines takes over five seconds.
test('native host delays Dock removal and cancels it on reopen', async t => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'dialog-dock-policy-'));
  t.after(() => fs.rm(root, {recursive: true, force: true}));
  const dir = path.resolve('apps/mac-dialog-decisions');
  const helper = path.join(root, 'check');
  await exec('swiftc', [...['dialog.swift', 'host.swift', 'preview.swift', 'unicode-width.swift', 'test/dock-policy/main.swift'].map(file => path.join(dir, file)), '-o', helper], {timeout: 60000});
  const result = await exec(helper, [root], {timeout: 10000});
  assert.match(result.stdout, /EMPTY_DELAY_PASS/);
  assert.match(result.stdout, /REOPEN_CANCEL_PASS/);
  assert.match(result.stdout, /STALE_TIMER_PASS/);
});
