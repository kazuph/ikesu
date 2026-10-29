import net from 'node:net';
import readline from 'node:readline';

// Real API transport proxy: delays actual server replies to exercise delivery
// ordering. It never fabricates a view, response, or decision.
export class ViewDeliveryProxy {
 constructor(upstream,socketPath){this.upstream=upstream;this.socketPath=socketPath;this.sockets=new Set();this.subscriptions=[];this.lists=[];this.holdSubscriptions=true;this.holdLists=false;this.listCount=0;}
 async start(){
  this.server=net.createServer(client=>{
   const backend=net.createConnection(this.upstream);this.sockets.add(client);this.sockets.add(backend);
   client.pipe(backend);const lines=readline.createInterface({input:backend});
   const close=()=>{client.destroy();backend.destroy();lines.close();this.sockets.delete(client);this.sockets.delete(backend);};
   client.on('error',close);backend.on('error',close);client.on('close',close);backend.on('close',close);
   lines.on('line',line=>{
    const response=JSON.parse(line),release=()=>{if(!client.destroyed)client.write(line+'\n');};
    if(response.result?.type==='subscription_started'&&this.holdSubscriptions)this.subscriptions.push(release);
    else if(response.result?.type==='client_view_list'){this.listCount++;if(this.holdLists)this.lists.push(release);else release();}
    else release();
   });
  });
  await new Promise((resolve,reject)=>{this.server.once('error',reject);this.server.listen(this.socketPath,resolve);});
  return this;
 }
 releaseSubscriptions(){this.holdSubscriptions=false;for(const release of this.subscriptions.splice(0))release();}
 releaseLists(){this.holdLists=false;for(const release of this.lists.splice(0))release();}
 async stop(){for(const socket of this.sockets)socket.destroy();await new Promise(resolve=>this.server.close(resolve));}
}
