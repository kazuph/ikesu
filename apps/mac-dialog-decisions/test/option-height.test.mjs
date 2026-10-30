import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';

// Swift compilation and real AppKit layout startup exceed five seconds.
test('actual option cards share the tallest height, keep top-aligned uncut text at normal/minimum widths in both appearances', () => {
 const root = fs.mkdtempSync(path.join(os.tmpdir(), 'ikesu-option-height-'));
 try {
  const dir = path.resolve('apps/mac-dialog-decisions');
  const helper = path.join(root, 'check');
  const build = spawnSync('swiftc', ['dialog.swift', 'host.swift', 'preview.swift', 'unicode-width.swift', 'test/option-height/main.swift'].map(p => path.join(dir, p)).concat(['-o', helper]), {encoding:'utf8', timeout:60000});
  assert.equal(build.status, 0, build.stderr);
  const run = spawnSync(helper, [root], {encoding:'utf8', timeout:60000});
  const evidence = process.env.IKESU_OPTION_EVIDENCE;
  if (evidence) { fs.mkdirSync(evidence, {recursive:true}); if (fs.existsSync(path.join(root, 'metrics.json'))) fs.copyFileSync(path.join(root, 'metrics.json'), path.join(evidence, 'metrics.json')); fs.writeFileSync(path.join(evidence,'native.log'), run.stdout + run.stderr); }
  assert.equal(run.status, 0, run.stdout + run.stderr);
  const rows = JSON.parse(fs.readFileSync(path.join(root, 'metrics.json')));
  assert.equal(rows.length, 12);
  assert.ok(rows.every(row => row.heightSpread <= 0.5 && row.clipped.length === 0));
  for (const count of [2,3,4]) for (const theme of ['NSAppearanceNameAqua','NSAppearanceNameDarkAqua']) {
   const narrow = rows.find(row => row.count === count && row.width === 720 && row.theme === theme);
   const wide = rows.find(row => row.count === count && row.width === 1080 && row.theme === theme);
   assert.ok(narrow && wide);
   assert.ok(narrow.descriptionLines[0] >= 4);
   assert.ok(narrow.descriptionLines[0] > wide.descriptionLines[0], 'narrow width increases wrapping');
  }
  console.log(run.stdout.trim());
 } finally { fs.rmSync(root, {recursive:true,force:true}); }
});
