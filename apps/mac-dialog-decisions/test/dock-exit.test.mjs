import test from 'node:test';
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {once} from 'node:events';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import readline from 'node:readline';
import {performance} from 'node:perf_hooks';

const binary = process.env.MAC_DIALOG_TEST_NATIVE_BINARY;
for (const scenario of ['empty-stop', 'window-stop', 'pending-stop', 'eof', 'single-close']) {
  test(`native host exits promptly: ${scenario}`, async t => {
    assert.ok(binary, 'MAC_DIALOG_TEST_NATIVE_BINARY is required');
    const root = await fs.mkdtemp(path.join(os.tmpdir(), 'dock-exit-'));
    t.after(() => fs.rm(root, {recursive:true, force:true}));
    const spec = path.join(root, 'one.json');
    await fs.writeFile(spec, JSON.stringify({window_title:'exit', title:'exit', options:[], multi_select:false, allow_text:false}));
    const child = spawn(binary, scenario === 'single-close' ? [spec] : ['--host', root], {stdio:['pipe','pipe','pipe']});
    const exited = once(child, 'exit');
    const closed = once(child, 'close');
    t.after(() => { if (child.exitCode === null && child.signalCode === null) child.kill('SIGTERM'); });
    let stderr = ''; child.stderr.on('data', chunk => { stderr += chunk; });
    const events = []; const listeners = new Set();
    readline.createInterface({input:child.stdout}).on('line', line => {
      try { events.push(JSON.parse(line)); for (const listener of listeners) listener(); } catch {}
    });
    const wait = predicate => new Promise((resolve, reject) => {
      const timer = setTimeout(() => { listeners.delete(check); reject(Error(`event timeout: ${JSON.stringify(events)} ${stderr}`)); }, 5000);
      const check = () => { if (predicate()) { clearTimeout(timer); listeners.delete(check); resolve(); } };
      listeners.add(check); check();
    });
    const send = object => child.stdin.write(JSON.stringify(object)+'\n');
    if (!['empty-stop', 'single-close'].includes(scenario)) send({op:'open', path:spec});
    if (scenario !== 'empty-stop') await wait(() => events.some(e => e.event === 'shown'));
    if (scenario === 'pending-stop') {
      send({op:'close', key:'one.json'});
      await wait(() => events.some(e => e.event === 'closed') && events.some(e => e.event === 'windows' && e.count === 0));
    }
    const start = performance.now();
    if (scenario === 'eof') child.stdin.end();
    else if (scenario === 'single-close') send({op:'close', key:'one.json'});
    else send({op:'stop'});
    const [code, signal] = await Promise.race([exited, new Promise((_, reject) => setTimeout(() => reject(Error('process exit timeout')), 3000))]);
    const elapsed = performance.now() - start;
    await closed;
    assert.equal(signal, null); assert.equal(code, 0, stderr);
    if (['window-stop','pending-stop','eof','single-close'].includes(scenario)) assert.equal(events.filter(e => e.event === 'closed').length, 1);
    assert.ok(elapsed < 1000, `exit must not wait for Dock timer: ${elapsed.toFixed(1)}ms`);
    console.log(`${scenario}: exit=${elapsed.toFixed(1)}ms closed=${events.filter(e => e.event === 'closed').length}`);
  });
}
