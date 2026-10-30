import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {execFile, spawn} from 'node:child_process';
import {promisify} from 'node:util';
import {once} from 'node:events';

const exec = promisify(execFile);
async function until(fn) {
  const end = Date.now() + 15000;
  let last;
  while (Date.now() < end) {
    try { return await fn(); } catch (error) { last = error; }
    await new Promise(resolve => setTimeout(resolve, 25));
  }
  throw last;
}

test('実デーモンの起動と版切替後のwatchは古い版ではなく固定入口を使う', async t => {
  const binary = process.env.MAC_DIALOG_TEST_IKESU, native = process.env.MAC_DIALOG_TEST_NATIVE_BINARY;
  assert.ok(binary && native, 'actual ikesu and native binaries required');
  const root = fs.realpathSync(fs.mkdtempSync('/tmp/ikesu-bin-cutover-'));
  const home = path.join(root, 'home'), state = path.join(root, 'panel');
  const old = path.join(root, 'old'), next = path.join(root, 'next');
  const entry = path.join(home, '.local/bin/ikesu'), stale = path.join(home, '.local/lib/ikesu/releases/old/ikesu');
  const source = path.resolve('apps/mac-dialog-decisions');
  fs.cpSync(source, old, {recursive:true}); fs.cpSync(source, next, {recursive:true});
  fs.mkdirSync(path.dirname(entry), {recursive:true}); fs.mkdirSync(path.dirname(stale), {recursive:true});
  fs.copyFileSync(binary, entry); fs.copyFileSync(binary, stale);
  fs.chmodSync(entry, 0o755); fs.chmodSync(stale, 0o755);
  fs.mkdirSync(path.join(state, 'bin'), {recursive:true});
  const hash = ['dialog.swift','main.swift','host.swift','preview.swift','unicode-width.swift']
    .reduce((h, name) => h.update(fs.readFileSync(path.join(old, name))), createHash('sha256')).digest('hex').slice(0,12);
  fs.copyFileSync(native, path.join(state, 'bin', `ikesu-decision-dialog-${hash}`));
  fs.chmodSync(path.join(state, 'bin', `ikesu-decision-dialog-${hash}`), 0o755);
  const env = {...process.env};
  for (const key of Object.keys(env)) if (/^(IKESU|MAC_DIALOG)_/.test(key)) delete env[key];
  Object.assign(env, {HOME:home, SHELL:'/bin/sh', IKESU_STATE_DIR:path.join(root,'state'),
    IKESU_SOCKET_PATH:path.join(root,'state/default/ikesu.sock'), IKESU_SOCKET_PATH_EXPLICIT:'1',
    IKESU_CONFIG:path.join(root,'config.toml'), IKESU_PLUGIN_STATE_DIR:state,
    IKESU_PLUGIN_CONFIG_DIR:path.join(root,'config'), IKESU_BIN_PATH:stale});
  fs.writeFileSync(env.IKESU_CONFIG, 'onboarding=false\n[experimental]\nallow_nested=true\n[ui.sound]\nenabled=false\n');
  const server = spawn(entry, ['server'], {env, stdio:'ignore'});
  const cli = async args => (await exec(entry, args, {env, timeout:10000})).stdout;
  t.after(async () => {
    try {
      const owner=JSON.parse(fs.readFileSync(path.join(state,'daemon.json')));
      const ids=(await exec('pgrep',['-u',String(process.getuid()),'-f',root])).stdout.trim().split('\n').map(Number);
      if ([old,next].includes(owner.root) && ids.includes(owner.pid)) process.kill(owner.pid,'SIGTERM');
    } catch {}
    try { await cli(['server','stop']); } catch {}
    if (server.exitCode === null && server.signalCode === null) await once(server,'close');
    fs.rmSync(root,{recursive:true,force:true});
  });
  await until(() => cli(['workspace','list']));
  await cli(['plugin','link',old]);
  await exec(process.execPath,[path.join(old,'mac-dialog-decisions.mjs'),'ensure'],{env,timeout:15000});
  const watch = async expectedRoot => until(async () => {
    const owner=JSON.parse(fs.readFileSync(path.join(state,'daemon.json'),'utf8'));
    assert.equal(owner.root,expectedRoot);
    const lines=(await exec('ps',['-axo','pid=,ppid=,command='])).stdout.split('\n');
    const child=lines.map(line=>line.trim().match(/^(\d+)\s+(\d+)\s+(.*)$/)).find(m=>m && Number(m[2])===owner.pid && m[3].includes('decision watch --all-machines'));
    assert.ok(child, 'watch child must be running');
    assert.ok(child[3].startsWith(entry+' '), `watch used ${child[3]}`);
    return owner.pid;
  });
  const first = await watch(old);
  await cli(['plugin','unlink','kazuph.mac-dialog-decisions']);
  await cli(['plugin','link',next]);
  await exec(process.execPath,[path.join(next,'mac-dialog-decisions.mjs'),'ensure'],{env,timeout:15000});
  const second = await watch(next);
  assert.notEqual(first,second);
});
