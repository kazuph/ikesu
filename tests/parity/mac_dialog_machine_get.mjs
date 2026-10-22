import test from 'node:test';import fs from 'node:fs';import path from 'node:path';import readline from 'node:readline';import {spawn,execFile} from 'node:child_process';import {promisify} from 'node:util';import assert from 'node:assert/strict';
test('保存済みSSH先のdecision getは同じ出力を返し接続失敗を明示する',async()=>{
const host=process.env.IKESU_TEST_SSH_HOST,remoteBinary=process.env.IKESU_TEST_REMOTE_BIN,binary=process.env.IKESU_TEST_LOCAL_BIN;
assert.ok(host&&remoteBinary&&binary,'IKESU_TEST_SSH_HOST, IKESU_TEST_REMOTE_BIN, IKESU_TEST_LOCAL_BIN are required');
const exec=promisify(execFile),root=fs.mkdtempSync('/tmp/ikesu-get-'),remote='/tmp/'+path.basename(root);
const quote=s=>"'"+s.replaceAll("'","'\\''")+"'";
const helper=fs.readFileSync(new URL('./mac_dialog_machine_server.py',import.meta.url),'utf8');
const server=spawn('ssh',[host,['python3','-c',helper,remote,remoteBinary].map(quote).join(' ')],{stdio:['pipe','pipe','inherit']}),lines=[];readline.createInterface({input:server.stdout}).on('line',l=>lines.push(l));const exited=new Promise(r=>server.once('close',r));
const endpoint=path.join(root,'ikesu.sock'),env={...process.env,HOME:root,IKESU_SOCKET_PATH:endpoint,IKESU_SOCKET_PATH_EXPLICIT:'1'};
const remoteEnv=['HOME='+remote,'IKESU_CONFIG='+remote+'/config.toml','IKESU_STATE_DIR='+remote+'/state','IKESU_SOCKET_PATH='+remote+'/state/default/ikesu.sock','IKESU_SOCKET_PATH_EXPLICIT=1'];

// A real executable on the SSH host preserves this isolated server environment.
const wrapper=remote+"/ikesu";
fs.writeFileSync(path.join(root,'machines.json'),JSON.stringify([{id:'remote-test',label:'Remote fixture',target:host,session:'default',enabled:true},{id:'unreachable',label:'Unavailable fixture',target:'127.0.0.1',session:'default',enabled:true}]));env.IKESU_MACHINE_REMOTE_BINARY=wrapper;
async function remoteCLI(...args){return exec('ssh',[host,[wrapper,...args].map(quote).join(' ')],{timeout:20000})}
let ask;
try{
 const deadline=Date.now()+20000;while(!lines.includes('UP')){if(Date.now()>deadline)throw Error('server deadline');await new Promise(r=>setTimeout(r,20))}
await exec('ssh',[host,'python3 -c '+quote(`import pathlib; p=pathlib.Path(${JSON.stringify(wrapper)});p.parent.mkdir(exist_ok=True);p.write_text(${JSON.stringify('#!/bin/sh\nexec env '+remoteEnv.map(quote).join(' ')+' '+quote(remoteBinary)+' "$@"\n')});p.chmod(0o700)`)]);
 ask=remoteCLI('decision','ask','--title','SSH lookup','--option','yes:Yes:approve');ask.catch(()=>{});
 let decision;for(let i=0;i<100;i++){const result=JSON.parse((await remoteCLI('decision','list')).stdout);decision=result.result.decisions[0];if(decision)break;await new Promise(r=>setTimeout(r,20))}assert.ok(decision);
 const direct=JSON.parse((await remoteCLI('decision','get',decision.decision_id)).stdout);
 for(const ref of ['remote-test','Remote fixture']){const forwarded=JSON.parse((await exec(binary,['decision','get',decision.decision_id,'--machine',ref],{env,timeout:20000})).stdout);assert.deepEqual(forwarded,direct)}
 await assert.rejects(exec(binary,['decision','get',decision.decision_id,'--machine','unreachable'],{env,timeout:20000}),e=>e.code!==0&&/SSH|connect|verification failed/.test(e.stderr));
 await remoteCLI('decision','cancel',decision.decision_id);await ask.catch(e=>{assert.equal(e.code,4)});console.log('PASS actual SSH: saved ID and label return same decision; unreachable errors; isolated HOME only');
}finally{server.stdin.end('stop\n');await exited;fs.rmSync(root,{recursive:true,force:true})}

});
