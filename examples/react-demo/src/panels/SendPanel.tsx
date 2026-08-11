// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { useState } from "react";
import { testSend, type TestSendInput } from "../session.js";

interface SendPanelProps {
  disabled: boolean;
  onSent: (description: string) => void;
}

/** Ready-made shapes worth seeing the widget handle. */
const PRESETS: Array<{ label: string; input: TestSendInput }> = [
  { label: "Plain", input: { title: "Weekly report is ready", body: "Your Acme Analytics summary for this week." } },
  {
    label: "With action",
    input: {
      title: "Invoice #1041 is ready",
      body: "Payment is due in 14 days.",
      actionUrl: "/invoices/1041",
      actionLabel: "View invoice",
    },
  },
  {
    label: "Long body",
    input: {
      title: "Data export finished",
      body:
        "Your export of 1.2M rows completed successfully and will be retained for thirty days. " +
        "This body is deliberately long so the two-line clamp in the widget is visible.",
    },
  },
  { label: "Burst of 5", input: { title: "Threshold crossed", body: "Sessions exceeded target.", count: 5 } },
  // 25 is chosen to exceed one page of 20, so "Load more" appears.
  { label: "25 (fills 2 pages)", input: { title: "Backfill event", body: "Historical record.", count: 25 } },
];

/**
 * The operator's way to drive the demo.
 *
 * ## Why this is labelled "transactional"
 *
 * These sends supply `content` directly rather than naming a template, and a direct-content send
 * **bypasses preference resolution entirely** — `FilterChannelsForTemplate` returns the requested
 * channels verbatim. So a preference toggle sitting next to this button would have no effect on it.
 * Rather than imply otherwise, the button says what it does.
 */
export function SendPanel({ disabled, onSent }: SendPanelProps) {
  const [title, setTitle] = useState("Weekly report is ready");
  const [body, setBody] = useState("Your Acme Analytics summary for this week.");
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

  async function send(input: TestSendInput, description: string) {
    setPending(true);
    setFailure(null);
    try {
      const { notificationIds } = await testSend(input);
      // Deliberately worded as accepted, not delivered: /v1/send returns 202 and the notification
      // is created later by dispatch, so arrival is something to watch for rather than assume.
      onSent(`${description} — accepted ${notificationIds.length}, awaiting delivery`);
    } catch (cause) {
      setFailure(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setPending(false);
    }
  }

  return (
    <section className="card">
      <h2>Send a test notification</h2>
      <p className="hint">
        Goes through the real pipeline: Send → NATS → Dispatch → inbox worker → Centrifugo. Expect a
        moment before it arrives.
      </p>

      <label htmlFor="send-title">Title</label>
      <input
        id="send-title"
        type="text"
        value={title}
        onChange={(event) => setTitle(event.target.value)}
      />

      <label htmlFor="send-body">Body</label>
      <input
        id="send-body"
        type="text"
        value={body}
        onChange={(event) => setBody(event.target.value)}
      />

      <div className="button-row">
        <button
          className="primary"
          type="button"
          disabled={disabled || pending || title === ""}
          data-testid="send-transactional"
          onClick={() => void send({ title, body }, title)}
        >
          {pending ? "Sending…" : "Send transactional"}
        </button>
      </div>
      <p className="hint" style={{ marginTop: 8, marginBottom: 12 }}>
        Labelled transactional because a direct-content send bypasses the preference centre — the
        channels asked for are the channels used.
      </p>

      <div className="button-row">
        {PRESETS.map((preset) => (
          <button
            key={preset.label}
            type="button"
            disabled={disabled || pending}
            data-testid={`send-preset-${preset.label.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`}
            onClick={() => void send(preset.input, preset.label)}
          >
            {preset.label}
          </button>
        ))}
      </div>

      {failure ? <p className="error">{failure}</p> : null}
    </section>
  );
}
