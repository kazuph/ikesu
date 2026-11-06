import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { loadToken, rotateToken, tokenMatches } from '../security.mjs';

test('合言葉は永続化され、入替後は古い値を拒否する', async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'android-decisions-token-'));
  try {
    const first = await loadToken(dir);
    assert.equal(first, await loadToken(dir));
    assert.equal((await fs.stat(path.join(dir, 'token'))).mode & 0o777, 0o600);
    assert.equal(tokenMatches(first, first), true);
    assert.equal(tokenMatches(first, ''), false);
    const second = await rotateToken(dir);
    assert.notEqual(first, second);
    assert.equal(tokenMatches(second, first), false);
    assert.equal(tokenMatches(second, second), true);
  } finally { await fs.rm(dir, { recursive: true, force: true }); }
});

test('権限が広い合言葉ファイルを拒否する', async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'android-decisions-token-'));
  try {
    await loadToken(dir);
    await fs.chmod(path.join(dir, 'token'), 0o644);
    await assert.rejects(loadToken(dir), /権限/);
  } finally { await fs.rm(dir, { recursive: true, force: true }); }
});
