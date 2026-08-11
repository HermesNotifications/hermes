// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { Trend, Counter } from 'k6/metrics';

export const sendAckLatency       = new Trend('send_ack_latency', true);
export const wsConnectLatency     = new Trend('ws_connect_latency', true);
export const wsPushE2ELatency     = new Trend('ws_push_e2e_latency', true);
export const inboxListLatency     = new Trend('inbox_list_latency', true);
export const wsConnectionsOpened  = new Counter('ws_connections_opened');
export const wsConnectionsClosed  = new Counter('ws_connections_closed');
export const wsConnectionDrops    = new Counter('ws_connection_drops');
export const sendErrors           = new Counter('send_errors');
export const pushReceived         = new Counter('ws_push_received');
// Gap between a socket closing and the same VU having a live one again. Only meaningful in the
// churn scenario, where pods are restarted underneath the run: in a steady-state scenario
// sockets close once, at the end, and nothing reconnects.
export const wsReconnectDuration  = new Trend('ws_reconnect_duration', true);
