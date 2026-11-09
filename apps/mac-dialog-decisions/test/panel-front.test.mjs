import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import {execFile,spawn} from 'node:child_process';
import readline from 'node:readline';
import {promisify} from 'node:util';
const exec = promisify(execFile);
const dir = path.resolve('apps/mac-dialog-decisions');

// Both tests share one compile of the probe and the product binary (the
// product binary is reused from MAC_DIALOG_TEST_NATIVE_BINARY when given).
let built;
function binaries() {
  built ??= (async () => {
    const root = await fs.realpath(await fs.mkdtemp(path.join(os.tmpdir(), 'dialog-front-bin-')));
    const sources = main => [...['dialog.swift','host.swift','preview.swift','unicode-width.swift',main].map(f=>path.join(dir,f))];
    const probe = path.join(root, 'panel-front'), dialog = process.env.MAC_DIALOG_TEST_NATIVE_BINARY || path.join(root, 'dialog');
    await Promise.all([
      exec('swiftc', [...sources('test/panel-front/main.swift'), '-o', probe], {timeout:60000}),
      process.env.MAC_DIALOG_TEST_NATIVE_BINARY ? null : exec('swiftc', [...sources('main.swift'), '-o', dialog], {timeout:60000}),
    ]);
    return {root, probe, dialog};
  })();
  return built;
}
test.after(async () => { if (built) await fs.rm((await built).root, {recursive:true, force:true}); });

// A new panel stays behind an uncovered open panel, comes to the front when
// every open panel is covered by another app. A panel placed in front takes
// the keyboard; a panel placed behind does not.
test('native panel ordering follows whether an open panel is in front', async t => {
  const root = await fs.realpath(await fs.mkdtemp(path.join(os.tmpdir(), 'dialog-panel-front-')));
  t.after(() => fs.rm(root, {recursive:true, force:true}));
  const helper = (await binaries()).probe;
  const {stdout} = await exec(helper, [root], {timeout:30000});
  console.log(stdout.split('\n').filter(line => /^(PANEL_FRONT|KEYBOARD|FAIL)/.test(line)).join('\n'));
  assert.match(stdout, /PANEL_FRONT_PASS/);
  const shown = stdout.split('\n').filter(line => line.startsWith('{')).map(line => JSON.parse(line)).filter(row => row.event === 'shown');
  assert.deepEqual(shown.map(row => [row.key, row.placement]), [['burst1.json','front'],['burst2.json','behind'],['first.json','front'],['second.json','behind'],['third.json','front']]);
  assert.deepEqual(shown.map(row => row.placement_reason), ['no_open_panel','panel_in_front','no_open_panel','panel_in_front','all_panels_covered']);
  for (const row of shown) assert.equal(row.key_window, row.placement === 'front', JSON.stringify(row));
});

// The one-dialog helper path (one process per panel, as spawnHelperDialog
// runs it) follows the same rule across sibling helper processes.
test('one-dialog helper processes follow the same front rule', async t => {
  const root = await fs.realpath(await fs.mkdtemp(path.join(os.tmpdir(), 'dialog-helper-front-')));
  const children = [];
  t.after(async () => {
    for (const child of children) child.stdin.end();
    await Promise.all(children.map(child => child.exitCode === null ? new Promise(resolve => child.once('close', resolve)) : null));
    await fs.rm(root, {recursive:true, force:true});
  });
  const {dialog, probe} = await binaries();
  const start = (command, argv, ready) => new Promise((resolve, reject) => {
    const child = spawn(command, argv, {stdio:['pipe','pipe','inherit']});
    children.push(child);
    const lines = readline.createInterface({input: child.stdout});
    lines.on('line', line => { const row = ready(line); if (row) resolve({child, row}); });
    child.once('close', code => reject(Error(`${path.basename(command)} ended early: ${code}`)));
  });
  const panel = async (name, background) => {
    const file = path.join(root, `${name}.json`);
    await fs.writeFile(file, JSON.stringify({window_title:`helper ${name}`, title:`ヘルパーの前面 ${name}`, options:[{id:'yes',label:'はい'}], allow_text:false, multi_select:false, present_in_background:background}));
    return start(dialog, [file], line => { if (!line.startsWith('{')) return; const row = JSON.parse(line); return row.event === 'shown' ? row : null; });
  };
  const order = async () => JSON.parse((await exec(probe, ['order'], {timeout:10000})).stdout);
  const position = (rows, pid) => rows.findIndex(row => row.pid === pid);
  const until = async (label, predicate) => {
    const end = Date.now() + 5000;
    for (;;) { const rows = await order(); if (predicate(rows)) return rows; if (Date.now() > end) throw Error(`${label}: ${JSON.stringify(rows.slice(0, 8))}`); await new Promise(r => setTimeout(r, 50)); }
  };
  const a = await panel('a', false);
  assert.equal(a.row.placement, 'front');
  await until('first helper panel in front', rows => position(rows, a.child.pid) === 0);
  const b = await panel('b', true);
  assert.deepEqual([b.row.placement, b.row.placement_reason], ['behind', 'panel_in_front']);
  const orderA = await until('second helper directly behind the first', rows => position(rows, b.child.pid) === position(rows, a.child.pid) + 1);
  console.log('HELPER_FRONT_A', JSON.stringify(orderA.slice(0, 3)));
  const cover = await start(probe, ['cover', String(a.row.x - 40), String(a.row.y - 400), '1600', '1400'], line => line.startsWith('COVER_READY') ? line : null);
  await until('cover over both helper panels', rows => { const c = position(rows, cover.child.pid); return c >= 0 && c < position(rows, a.child.pid) && c < position(rows, b.child.pid); });
  const c = await panel('c', true);
  assert.deepEqual([c.row.placement, c.row.placement_reason], ['front', 'all_panels_covered']);
  const orderB = await until('third helper in front of the cover', rows => position(rows, c.child.pid) >= 0 && position(rows, c.child.pid) < position(rows, cover.child.pid));
  console.log('HELPER_FRONT_B', JSON.stringify(orderB.slice(0, 4)));
  // A front panel takes the keyboard; a panel placed behind does not.
  for (const row of [a.row, c.row]) assert.equal(row.key_window, true, JSON.stringify(row));
  assert.equal(b.row.key_window, false);
});
