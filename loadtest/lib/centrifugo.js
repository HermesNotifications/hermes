// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import ws from 'k6/ws';
import { jwtFor, internalID } from './auth.js';
import {
  wsConnectLatency, wsConnectionsOpened, wsConnectionsClosed, wsConnectionDrops,
  pushReceived, wsPushE2ELatency, wsReconnectDuration,
} from './metrics.js';

// When this VU's previous socket closed, or 0 on its first iteration. Module scope is per-VU in
// k6, so this needs no keying. It is how the churn scenario measures reconnect time: a VU whose
// pod was restarted ends its iteration early, and constant-vus immediately starts another.
let lastCloseAt = 0;

// centrifugoURL defaults to the local Centrifugo exposed by tilt.
// Override with CENTRIFUGO_URL.
export function centrifugoURL() {
  return __ENV.CENTRIFUGO_URL || 'ws://localhost:8000/connection/websocket';
}

// connect opens a Centrifugo WS connection for the given user, subscribes to
// their personal channel, and invokes onPush(notification_id) for each
// incoming publication. Blocks until the socket is closed by setTimeout.
//
// `user` is a {id, external_id} pair from the seed manifest. The channel and the token
// subject are both the INTERNAL id: the inbox worker publishes to `user#<internal id>`
// (internal/delivery/inbox.go), so subscribing with anything else yields a socket that
// connects, subscribes successfully, and then never receives a thing.
export function connect(user, organizationID, onPush) {
  const url = centrifugoURL();
  const userID = internalID(user);
  const token = jwtFor(user, organizationID);

  const start = Date.now();
  return ws.connect(url, {}, function (socket) {
    socket.on('open', function () {
      wsConnectionsOpened.add(1);
      wsConnectLatency.add(Date.now() - start);
      if (lastCloseAt > 0) {
        wsReconnectDuration.add(Date.now() - lastCloseAt);
        lastCloseAt = 0;
      }
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
        onPush(msg.push.pub.data);
      }
    });

    socket.on('close', function () {
      wsConnectionsClosed.add(1);
      lastCloseAt = Date.now();
    });
    socket.on('error', function (_e) { wsConnectionDrops.add(1); });

    socket.setTimeout(function () { socket.close(); },
      (parseInt(__ENV.WS_HOLD_SECONDS || '60', 10)) * 1000);
  });
}

// recordE2EOnPush measures POST /v1/send -> WebSocket arrival from the timestamp
// buildSendBody stamped into the notification's metadata.
//
// Both ends of the subtraction come from the same k6 process: instanceRange shards the
// user population per runner pod, so a given user's sends and that user's socket always
// live on one pod. No cross-node clock comparison is involved.
export function recordE2EOnPush(payload) {
  const sent = payload && payload.metadata && payload.metadata.lt_sent_ms;
  if (typeof sent === 'number') wsPushE2ELatency.add(Date.now() - sent);
}
