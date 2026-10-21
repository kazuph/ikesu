import fs from 'node:fs';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const exec = promisify(execFile);

// Resolve every external destination before allocating or contacting anything.
export function requireLiveConfig(env = process.env, {screen = false} = {}) {
 const pick = name => env['IKESU_LIVE_TEST_'+name] || env['GTX_LIVE_TEST_'+name];
 const required = ['BIN','REMOTE_BIN','TARGET','EVIDENCE','REPORT_BIN','PARENT_PANE','REPORT_SOCKET'];
 if(screen) required.push('REMOTE_HOME','CLAUDE_BIN','NATIVE_BINARY');
 const config = {};
 for(const name of required) {
  const value=pick(name);
  if(!value) throw Error('required: IKESU_LIVE_TEST_'+name+' (GTX alias accepted)');
  config[name]=value;
 }
 for(const name of ['BIN','REMOTE_BIN','REPORT_BIN','REPORT_SOCKET','EVIDENCE',...(screen?['REMOTE_HOME','CLAUDE_BIN','NATIVE_BINARY']:[])]) {
  if(!config[name].startsWith('/')) throw Error('required absolute path: '+name);
 }
 if(pick('NATIVE_BINARY')) config.NATIVE_BINARY=pick('NATIVE_BINARY');
 return config;
}
// Only a fully bounded, recognizable Claude input area is evidence of an
// unsent report. Unknown/truncated layouts fail closed; history never counts.
export function hasUnsentReport(screen, message) {
 if (!message || /[\r\n]/.test(message)) return false;
 const lines=screen.replace(/\x1b\[[0-?]*[ -/]*[@-~]/g,'').split('\n');
 let prompt=-1;
 for(let i=0;i<lines.length;i++) if(/^❯[ \u00a0]/u.test(lines[i])) prompt=i;
 if(prompt<1 || !/^─+$/u.test(lines[prompt-1].trim())) return false;
 let end=prompt+1;
 while(end<lines.length && !/^─+$/u.test(lines[end].trim())) end++;
 if(end===lines.length) return false;
 const input=[lines[prompt].replace(/^❯[ \u00a0]/u,''),...lines.slice(prompt+1,end).map(line=>line.startsWith('  ')?line.slice(2):line)].join('');
 return input===message;
}
export async function reportToParent(config, message) {
 const env={...process.env,IKESU_SOCKET_PATH:config.REPORT_SOCKET,GTX_SOCKET_PATH:config.REPORT_SOCKET,IKESU_SOCKET_EXPLICIT:'1',GTX_SOCKET_EXPLICIT:'1'};
 const run=args=>exec(config.REPORT_BIN,args,{env,timeout:15_000});
 const result=JSON.parse((await run(['pane','get',config.PARENT_PANE])).stdout);
 if(result?.result?.pane?.pane_id!==config.PARENT_PANE) throw Error('parent pane validation failed');
 await run(['send',config.PARENT_PANE,message,'--from','machines']);
 const check=await run(['pane','read',config.PARENT_PANE,'--lines','15']);
 if(hasUnsentReport(check.stdout,message)) await run(['pane','send-keys',config.PARENT_PANE,'Enter']);
}
export async function cleanupSteps(steps) {
 const errors=[];
 for(const step of steps) {try {await step();} catch(error) {errors.push(error);}}
 if(errors.length) throw new AggregateError(errors,'owned test resource cleanup failed');
}
export function isolatedEnv(env) {
 const result={...env};
 for(const prefix of ['GTX_','IKESU_']) for(const key of Object.keys(result)) if(key.startsWith(prefix)) delete result[key];
 for(const key of ['CODEX_THREAD_ID','CLAUDECODE','HERDR_PANE_ID']) delete result[key];
 return result;
}
