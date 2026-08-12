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
        "This body is deliberately long so the two-line clamp, and the Show more control that " +
        "lifts it, are both visible in the panel.",
    },
  },
  {
    // Both the title's single-line ellipsis and the body's clamp, lifted by one control.
    label: "Long title and body",
    input: {
      title:
        "Your scheduled export of the full analytics warehouse has finished processing and is ready",
      body:
        "All 1.2M rows were exported without errors. The archive is available for thirty days, " +
        "after which it is deleted automatically and would need to be regenerated from scratch.",
    },
  },
  {
    label: "Error + toast",
    input: {
      title: "Payment failed",
      body: "Your card was declined. Update your billing details to avoid interruption.",
      level: "error",
      toast: true,
      actionUrl: "/billing",
      actionLabel: "Update billing",
    },
  },
  {
    label: "Success + toast",
    input: { title: "Export complete", body: "1.2M rows are ready to download.", level: "success", toast: true },
  },
  {
    label: "Warning, no toast",
    input: {
      // The two flags are independent: this one is styled as a warning in the list but is not
      // important enough to interrupt whatever the user is doing.
      title: "Approaching your plan limit",
      body: "You have used 92% of this month's events.",
      level: "warning",
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
  const [level, setLevel] = useState<"" | "info" | "success" | "warning" | "error">("");
  const [toast, setToast] = useState(false);
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

      <label className="field-label" htmlFor="send-title">
        Title
      </label>
      <input
        id="send-title"
        className="field"
        type="text"
        value={title}
        onChange={(event) => setTitle(event.target.value)}
      />

      <label className="field-label" htmlFor="send-body">
        Body
      </label>
      <input
        id="send-body"
        className="field"
        type="text"
        value={body}
        onChange={(event) => setBody(event.target.value)}
      />

      {/*
        The two reserved metadata keys, as separate controls because they are separate
        decisions: `level` is how it looks, `toast` is whether it interrupts. An error you do
        not want to interrupt someone with is a real combination, and so is an info toast.
      */}
      <label className="field-label" htmlFor="send-level">
        Level
      </label>
      <select
        id="send-level"
        className="field"
        value={level}
        onChange={(event) => setLevel(event.target.value as typeof level)}
      >
        <option value="">(none)</option>
        <option value="info">Info</option>
        <option value="success">Success</option>
        <option value="warning">Warning</option>
        <option value="error">Error</option>
      </select>

      <label className="checkbox-field">
        <input
          type="checkbox"
          checked={toast}
          data-testid="send-toast"
          onChange={(event) => setToast(event.target.checked)}
        />
        Toast it (metadata.toast)
      </label>

      <div className="button-row">
        <button
          className="primary"
          type="button"
          disabled={disabled || pending || title === ""}
          data-testid="send-transactional"
          onClick={() =>
            void send({ title, body, ...(level ? { level } : {}), ...(toast ? { toast } : {}) }, title)
          }
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
