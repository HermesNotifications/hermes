import ws from 'k6/ws';
import { jwtFor } from './auth.js';
import {
  wsConnectLatency, wsConnectionsOpened, wsConnectionsClosed, wsConnectionDrops,
  pushReceived, wsPushE2ELatency,
} from './metrics.js';
import { takeSent } from './shared.js';

// centrifugoURL defaults to the local Centrifugo exposed by tilt.
// Override with CENTRIFUGO_URL.
export function centrifugoURL() {
  return __ENV.CENTRIFUGO_URL || 'ws://localhost:8000/connection/websocket';
}

// connect opens a Centrifugo WS connection for the given user, subscribes to
// their personal channel, and invokes onPush(notification_id) for each
// incoming publication. Blocks until the socket is closed by setTimeout.
export function connect(userID, tenantID, onPush) {
  const url = centrifugoURL();
  const token = jwtFor(userID, tenantID);

  const start = Date.now();
  return ws.connect(url, {}, function (socket) {
    socket.on('open', function () {
      wsConnectionsOpened.add(1);
      wsConnectLatency.add(Date.now() - start);
      // Centrifugo v5 client protocol: connect + subscribe.
      socket.send(JSON.stringify({ id: 1, connect: { token: token, name: 'k6-loadtest' } }));
      socket.send(JSON.stringify({ id: 2, subscribe: { channel: `user#${userID}` } }));
    });

    socket.on('message', function (data) {
      let msg;
      try { msg = JSON.parse(data); } catch (e) { return; }
      // Centrifugo v5 app-level ping: server sends `{}`, client must echo `{}`
      // or the connection is dropped on ping_pong_interval timeout (~25s default).
      if (msg && Object.keys(msg).length === 0) {
        socket.send('{}');
        return;
      }
      // Publications arrive as { push: { channel, pub: { data: {...} } } }.
      if (msg.push && msg.push.pub && msg.push.pub.data) {
        pushReceived.add(1);
        const payload = msg.push.pub.data;
        if (payload.notification_id) onPush(payload.notification_id);
      }
    });

    socket.on('close', function () { wsConnectionsClosed.add(1); });
    socket.on('error', function (_e) { wsConnectionDrops.add(1); });

    socket.setTimeout(function () { socket.close(); },
      (parseInt(__ENV.WS_HOLD_SECONDS || '60', 10)) * 1000);
  });
}

// recordE2EOnPush looks up the send timestamp in the shared map and, if
// present, records the end-to-end latency trend.
export function recordE2EOnPush(notificationID) {
  const t = takeSent(notificationID);
  if (t !== undefined) wsPushE2ELatency.add(Date.now() - t);
}
