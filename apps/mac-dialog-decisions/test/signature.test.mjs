import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {verifyNativeDialogSignature} from '../mac-dialog-decisions.mjs';

test('実Developer IDアプリは正しい要件で通り、違うチームIDでは拒否する', () => {
  const app = process.env.IKESU_SIGNED_TEST_APP;
  assert.ok(app, 'IKESU_SIGNED_TEST_APP must name an existing signed app');
  assert.ok(fs.statSync(app).isDirectory());
  assert.doesNotThrow(() => verifyNativeDialogSignature(app));
  const wrong = '=anchor apple generic and certificate leaf[subject.OU] = "0000000000" and certificate leaf[subject.CN] = "Developer ID Application: KAZUHIRO HOMMA (67M8FDFZ28)"';
  assert.throws(() => verifyNativeDialogSignature(app, wrong), error => {
    const stderr = String(error.stderr);
    assert.ok(!stderr.includes('invalid requirement specification'), stderr);
    assert.match(stderr, /failed to satisfy specified code requirement/);
    return true;
  });
});
