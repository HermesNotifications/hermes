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
