// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { WebSocket } from 'k6/websockets';
import { jwtFor, internalID } from './auth.js';
import {
  wsConnectLatency, wsConnectionsOpened, wsConnectionsClosed, wsConnectionDrops,
  pushReceived, wsPushE2ELatency,
} from './metrics.js';

// The async `k6/websockets` module, not the older blocking `k6/ws`.
//
// This is the difference between measuring Hermes and measuring k6. `k6/ws` blocks its VU
// inside connect() for the socket's whole life, so one socket costs one VU: 50k connections
// meant 50k VUs, and node metrics from that run showed the generator hosts at a load average
// of 10.1 against 12 cores while burning only 3.4 cores of actual CPU. That gap is runnable
// threads waiting for a scheduler slot, and it arrived at the same instant as the latency
// tails being attributed to the server -- which sat at 38% CPU throughout.
//
// Here one VU holds WS_SOCKETS_PER_VU sockets on the event loop, so connection count is
// decoupled from VU count and the generator stops competing with itself.
export function centrifugoURL() {
  return __ENV.CENTRIFUGO_URL || 'ws://localhost:8000/connection/websocket';
}

export function holdSeconds() {
  return parseInt(__ENV.WS_HOLD_SECONDS || '60', 10);
}

// openSocket connects one user, subscribes to their personal channel, and invokes
// onPush(payload) for each publication. It returns immediately; k6 keeps the iteration
// alive while the socket and its close timer are outstanding.
export function openSocket(user, organizationID, onPush) {
  const userID = internalID(user);
  const token = jwtFor(user, organizationID);
  const start = Date.now();

  const ws = new WebSocket(centrifugoURL());

  ws.addEventListener('open', function () {
    wsConnectionsOpened.add(1);
    wsConnectLatency.add(Date.now() - start);
    // Centrifugo client protocol v2: connect, then subscribe to the user-limited channel.
    ws.send(JSON.stringify({ id: 1, connect: { token: token, name: 'k6-loadtest' } }));
    ws.send(JSON.stringify({ id: 2, subscribe: { channel: `user#${userID}` } }));

    // Hold, then close. Without this the socket would live until k6 tore the iteration
    // down, and the run would measure connection churn rather than connections held.
    setTimeout(function () {
      try { ws.close(); } catch (e) { /* already closing */ }
    }, holdSeconds() * 1000);
  });

  ws.addEventListener('message', function (e) {
    let msg;
    try { msg = JSON.parse(e.data); } catch (err) { return; }
    // Server ping is an empty object and must be echoed, or the connection is dropped on
    // the ping_pong_interval timeout (~25s) and the run silently becomes a churn test.
    if (msg && Object.keys(msg).length === 0) {
      try { ws.send('{}'); } catch (err) { /* closing */ }
      return;
    }
    if (msg.push && msg.push.pub && msg.push.pub.data) {
      pushReceived.add(1);
      onPush(msg.push.pub.data);
    }
  });

  ws.addEventListener('close', function () { wsConnectionsClosed.add(1); });
  ws.addEventListener('error', function () { wsConnectionDrops.add(1); });

  return ws;
}

// recordE2EOnPush measures POST /v1/send -> WebSocket arrival from the timestamp
// buildSendBody stamped into the notification's metadata.
//
// The timestamp has to travel on the notification because the two ends are different VUs:
// a socket-holding VU never executes the send function, so anything held in process memory
// between them is written by one VU and read by another, and never matches.
export function recordE2EOnPush(payload) {
  const sent = payload && payload.metadata && payload.metadata.lt_sent_ms;
  if (typeof sent === 'number') wsPushE2ELatency.add(Date.now() - sent);
}
