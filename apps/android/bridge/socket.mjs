import http from 'node:http';
import { createReadStream } from 'node:fs';
import { WebSocketServer, WebSocket } from 'ws';
import { tokenMatches } from './security.mjs';

// Four 20 MiB images expand to base64, with room for the JSON envelope.
export const MAX_MESSAGE_BYTES = Math.ceil(4 * 20 * 1024 * 1024 / 3) * 4 + 1024 * 1024;
const ONLY = (object, keys) => object && typeof object === 'object' && !Array.isArray(object) && Object.keys(object).every(key => keys.includes(key));

export function validateAnswer(message) {
  if (!ONLY(message, ['type', 'id', 'options', 'text', 'images', 'sequence']) || message.type !== 'answer' ||
      typeof message.id !== 'string' || !message.id || typeof message.sequence !== 'string' || !message.sequence ||
      !Array.isArray(message.options) || !message.options.every(value => typeof value === 'string') ||
      typeof message.text !== 'string' || !Array.isArray(message.images) || message.images.length > 4 ||
      !message.images.every(image => typeof image === 'string' && image.length <= Math.ceil(20 * 1024 * 1024 / 3) * 4 && /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(image))) {
    throw Error('回答の形式が不正です');
  }
  return message;
}

export const MAX_SHOWN_IDS = 200;
export function validateShown(message) {
  if (!ONLY(message, ['type', 'ids']) || message.type !== 'shown' || !Array.isArray(message.ids) || message.ids.length > MAX_SHOWN_IDS ||
      !message.ids.every(value => typeof value === 'string' && value.length > 0 && value.length <= 128)) {
    throw Error('通知済みの形式が不正です');
  }
  return message;
}

export async function listen({ address, port, token, snapshot, answer, later, shown = async () => {}, videoPath = () => null }) {
  const server = http.createServer((_request, response) => { response.writeHead(404); response.end(); });
  const sockets = new WebSocketServer({ noServer: true, maxPayload: MAX_MESSAGE_BYTES, perMessageDeflate: false });
  server.on('upgrade', (request, socket, head) => {
    if (request.headers.origin !== undefined || request.url !== '/') { socket.destroy(); return; }
    sockets.handleUpgrade(request, socket, head, ws => sockets.emit('connection', ws));
  });
  sockets.on('connection', ws => {
    let authenticated = false;
    const sentVideos = new Set();
    const sendVideos = async decisions => {
      for (const decision of decisions) for (const video of decision.videos || []) {
        if (!video || sentVideos.has(video.key)) continue;
        const file = videoPath(decision.id, video.key);
        if (!file) continue;
        sentVideos.add(video.key);
        try {
          let index = 0;
          for await (const bytes of createReadStream(file, { highWaterMark: 256 * 1024 })) {
            if (ws.readyState !== WebSocket.OPEN) return;
            const frame = JSON.stringify({ type: 'video_chunk', id: decision.id, key: video.key, index: index++, data: bytes.toString('base64') });
            await new Promise((resolve, reject) => ws.send(frame, error => error ? reject(error) : resolve()));
          }
          if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'video_end', id: decision.id, key: video.key }));
        } catch { sentVideos.delete(video.key); }
      }
    };
    ws.sendSnapshot = decisions => { ws.send(JSON.stringify({ type: 'snapshot', decisions })); void sendVideos(decisions); };
    ws.on('error', () => {});
    ws.on('message', async (bytes, binary) => {
      if (binary) { ws.close(1003); return; }
      let message;
      try { message = JSON.parse(bytes.toString()); } catch { ws.close(1007); return; }
      if (!authenticated) {
        if (!ONLY(message, ['type', 'token']) || message.type !== 'auth' || !tokenMatches(token, message.token)) { ws.close(1008); return; }
        authenticated = true;
        ws.authenticated = true;
        try { ws.sendSnapshot(await snapshot()); }
        catch { ws.close(1011); }
        return;
      }
      try {
        if (message.type === 'answer') {
          const response = await answer(validateAnswer(message));
          if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'answer_result', sequence: message.sequence, ...response }));
        } else if (message.type === 'shown') {
          await shown(validateShown(message).ids);
        } else if (ONLY(message, ['type', 'id']) && message.type === 'later' && typeof message.id === 'string') {
          await later(message.id);
        } else { ws.close(1008); }
      } catch (error) {
        if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'error', sequence: message?.sequence, message: error.message }));
      }
    });
  });
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(port, address, resolve); });
  return {
    address: server.address(),
    clientCount() { let count = 0; for (const ws of sockets.clients) if (ws.readyState === WebSocket.OPEN && ws.authenticated) count++; return count; },
    publish(decisions) { for (const ws of sockets.clients) if (ws.readyState === WebSocket.OPEN && ws.authenticated) ws.sendSnapshot(decisions); },
    setToken(value) { token = value; for (const ws of sockets.clients) ws.terminate(); },
    close() { for (const ws of sockets.clients) ws.terminate(); return new Promise(resolve => server.close(resolve)); },
  };
}
