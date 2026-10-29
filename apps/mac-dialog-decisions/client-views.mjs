import net from 'node:net';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import readline from 'node:readline';
import {WATCH_RETRY_MS} from './common.mjs';

// Uses the same inherited socket and catalog as decision watch. Nothing is
// inferred from machine labels or the daemon host's hostname.
export class ClientViews {
  constructor(env = process.env) {
    this.socketPath = env.IKESU_SOCKET_PATH || path.join(env.IKESU_STATE_DIR || path.join(os.homedir(), '.local/state/ikesu'), 'default/ikesu.sock');
    this.directory = path.dirname(this.socketPath);
    this.requests = new Set();
    this.ready = false;
    this.revision = 0;
    this.stopped = false;
  }
  sessionFor(machine) {
    if (machine === 'local') return path.basename(this.directory);
    try {
      const profiles = JSON.parse(fs.readFileSync(path.join(this.directory, 'machines.json'), 'utf8'));
      const matches = profiles.filter(profile => profile.id === machine && profile.enabled);
      return matches.length === 1 && typeof matches[0].session === 'string' ? matches[0].session : null;
    } catch { return null; }
  }
  start(changed) { this.changed = changed; this.connect(); }
  connect() {
    if (this.stopped) return;
    this.connecting = true;
    let finishConnection;
    this.connectionReady = new Promise(resolve => { finishConnection = resolve; });
    const socket = net.createConnection(this.socketPath);
    this.watch = socket;
    const lines = readline.createInterface({input: socket});
    lines.on('error', () => socket.destroy());
    socket.on('connect', () => socket.write(JSON.stringify({id:'client-views-watch', method:'events.subscribe', params:{subscriptions:[{type:'client.view_changed'}]}})+'\n'));
    lines.on('line', line => {
      try {
        const message = JSON.parse(line);
        if (message.error) { socket.destroy(); return; }
        if (message.result?.type === 'subscription_started') { this.ready = true; this.connecting = false; finishConnection(); this.revision++; this.changed?.(); }
        if (message.event === 'client_view_changed') { this.revision++; this.changed?.(); }
      } catch { socket.destroy(); }
    });
    socket.on('error', () => {});
    socket.on('close', () => {
      lines.close(); this.ready = false; this.connecting = false; finishConnection(); this.revision++; this.changed?.();
      if (!this.stopped) this.retry = setTimeout(() => this.connect(), WATCH_RETRY_MS);
    });
  }
  async list() {
    if (this.connecting) await this.connectionReady;
    const revision = this.revision;
    const views = await this.query();
    return revision === this.revision ? views : this.list();
  }
  query() {
    if (!this.ready || this.stopped) return Promise.reject(Error('client view subscription unavailable'));
    return new Promise((resolve, reject) => {
      const socket = net.createConnection(this.socketPath);
      this.requests.add(socket);
      const lines = readline.createInterface({input:socket});
      let settled = false;
      const finish = (error, views) => {
        if (settled) return;
        settled = true; this.requests.delete(socket); lines.close(); socket.destroy();
        error ? reject(error) : resolve(views);
      };
      // readline forwards input errors independently of the socket listener.
      // Settle the query instead of leaving it pending after an uncaught error.
      lines.on('error', error => finish(error));
      // Socket operations use the existing CLI request deadline.
      socket.setTimeout(15_000, () => finish(Error('client view query timed out')));
      socket.on('error', error => finish(error));
      socket.on('close', () => finish(Error('client view query disconnected')));
      socket.on('connect', () => socket.write(JSON.stringify({id:'client-views-list',method:'client.view.list',params:{}})+'\n'));
      lines.on('line', line => {
        try {
          const message = JSON.parse(line);
          if (message.error || message.result?.type !== 'client_view_list' || !Array.isArray(message.result.views)) throw Error('client view query unsupported');
          if (!this.ready) throw Error('client view subscription disconnected');
          finish(null, message.result.views);
        } catch (error) { finish(error); }
      });
    });
  }
  stop() {
    this.stopped = true; this.ready = false;
    clearTimeout(this.retry); this.watch?.destroy();
    for (const socket of this.requests) socket.destroy();
  }
}

export function displayedByFocusedClient(entry, clientViews, views) {
  const pane = entry.decision.origin?.pane_id;
  const session = clientViews?.sessionFor(entry.machineId);
  if (!pane || !session || !Array.isArray(views)) return false;
  return views.some(view => view.focused === true && view.machine_id === entry.machineId &&
    view.session === session && typeof view.workspace_id === "string" && view.workspace_id &&
    Array.isArray(view.pane_ids) && view.pane_ids.includes(pane));
}
