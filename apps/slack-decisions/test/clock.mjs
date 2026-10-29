// Deterministic clock explicitly injected into the grace-period state machine.
export class TestClock {
 constructor(now=Date.now()){this.time=now;this.timers=new Set();}
 now=()=>this.time;
 setTimeout=(callback,ms)=>{const timer={callback,at:this.time+ms,unref(){}};this.timers.add(timer);return timer;};
 clearTimeout=timer=>{this.timers.delete(timer);};
 async advance(ms){
  const target=this.time+ms;
  while(true){const next=[...this.timers].filter(timer=>timer.at<=target).sort((a,b)=>a.at-b.at)[0];if(!next)break;this.timers.delete(next);this.time=next.at;await next.callback();}
  this.time=target;
 }
}
