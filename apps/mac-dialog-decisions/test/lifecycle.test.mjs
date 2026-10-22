import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { MacDialogDecisionsDaemon } from "../daemon.mjs";
import { manageDaemon, requestSwitch } from "../lifecycle.mjs";

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(),"ikesu-cutover-"));
  const stateDir=path.join(root,"state"), running=path.join(root,"old"), next=path.join(root,"new");
  fs.mkdirSync(stateDir);fs.mkdirSync(running);fs.mkdirSync(next);
  fs.writeFileSync(path.join(running,"VERSION"),"0.1.0\n");
  fs.writeFileSync(path.join(next,"VERSION"),"0.2.0\n");
  const registry=path.join(root,"registry.json"), switches=path.join(root,"switches.jsonl");
  const register=value=>fs.writeFileSync(registry,JSON.stringify({root:value}));
  register(next);
  const daemon=new MacDialogDecisionsDaemon({config:{stateDir},log:()=>{}});
  const control=manageDaemon(daemon,{stateDir,daemonOwner:"test-owner"},{root:running,
    registeredRoot:async()=>JSON.parse(await fs.promises.readFile(registry,"utf8")).root,
    onSwitch:async root=>fs.appendFileSync(switches,JSON.stringify({root})+"\n")});
  t.after(()=>{control.stop();fs.rmSync(root,{recursive:true,force:true});});
  return {stateDir,running,next,daemon,control,register,switches};
}

async function until(predicate) {
  const deadline=Date.now()+2_000;
  while(!predicate()) { if(Date.now()>deadline)throw Error("cutover condition did not settle");await new Promise(resolve=>setTimeout(resolve,5)); }
}

for(const field of ["pending","dismissed","answerPending","retry"]) {
  test(`${field}が残る間は切り替えず、通知を待たない回答回収で1回だけ切り替える`,async t=>{
    const f=fixture(t);
    const entry={key:"mini dec-one",machineId:"mini",decision:{decision_id:"dec-one"}};
    if(field!=="pending")entry[field]=field==="retry"?{text:"kept"}:true;
    f.daemon.entries.set(entry.key,entry);
    f.daemon.notify();
    requestSwitch(f.stateDir,f.next);
    await f.control.check();
    assert.equal(fs.existsSync(f.switches),false);
    const state=JSON.parse(fs.readFileSync(path.join(f.stateDir,"daemon-state.json")));
    assert.equal(state.pending,1);
    assert.equal(state.all_machines,true);
    f.daemon.settleAnswer(entry);
    await until(()=>fs.existsSync(f.switches));
    const switches=fs.readFileSync(f.switches,"utf8").trim().split("\n");
    assert.equal(switches.length,1);
    assert.equal(JSON.parse(switches[0]).root,f.next);
    f.daemon.onResolved("mini",entry.decision);
    assert.equal(fs.readFileSync(f.switches,"utf8").trim().split("\n").length,1);
  });
}

test("登録を稼働版へ戻すと古い切替要求を取り消す",async t=>{
  const f=fixture(t);f.register(f.running);requestSwitch(f.stateDir,f.next);
  await until(()=>!fs.existsSync(path.join(f.stateDir,"switch-request.json")));
  assert.equal(fs.existsSync(f.switches),false);
});

test("切替要求と登録先が一致しなければ古い先へ切り替えない",async t=>{
  const f=fixture(t);f.register(path.join(f.running,"different"));requestSwitch(f.stateDir,f.next);
  await f.control.check();
  assert.equal(fs.existsSync(f.switches),false);
});
