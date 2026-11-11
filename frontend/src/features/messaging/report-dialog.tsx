"use client";

import { type FormEvent, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { FormError, SelectField, TextAreaField } from "@/components/ui/field";

import { type ReportReason, reportConversation } from "./api";
import { bodyLength, waitText } from "./format";

// maxDetails is the longest details, in characters after trimming, as the server counts them.
const maxDetails = 1000;

const reasons: { value: ReportReason; label: string }[] = [
  { value: "spam", label: "Spam" },
  { value: "harassment", label: "Harassment" },
  { value: "inappropriate", label: "Inappropriate" },
  { value: "other", label: "Other" },
];

type ReportDialogProps = {
  conversationId: string;
  // messageId reports one message; without it the whole conversation is reported.
  messageId?: string;
  name: string;
  // canBlock offers to block them once the report is sent, because reporting does not.
  canBlock: boolean;
  // title names what is reported when it is not a conversation with one person ("Report group").
  title?: string;
  onClose: () => void;
  onBlock: () => void;
};

// ReportDialog reports a conversation or a message to the administrators (FR-16.9).
export function ReportDialog({ conversationId, messageId, name, canBlock, title: titleOverride, onClose, onBlock }: ReportDialogProps) {
  const [reason, setReason] = useState<ReportReason | "">("");
  const [details, setDetails] = useState("");
  const [reasonError, setReasonError] = useState<string>();
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState<{ offerBlock: boolean } | null>(null);
  const title = messageId ? "Report message" : (titleOverride ?? "Report conversation");
  const length = bodyLength(details);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!reason) {
      setReasonError("Choose a reason.");
      return;
    }
    if (length > maxDetails) return;
    setReasonError(undefined);
    setError(null);
    setBusy(true);
    const res = await reportConversation(conversationId, {
      reason,
      ...(length > 0 ? { details: details.trim() } : {}),
      ...(messageId ? { messageId } : {}),
    });
    setBusy(false);
    if (res.ok) {
      setSent({ offerBlock: canBlock && !res.data.blockedByMe });
    } else if (res.code === "rate_limited") {
      setError(`You've sent the most reports allowed for today. Try again ${waitText(res.retryAfter)}.`);
    } else {
      setError(res.message);
    }
  }

  if (sent) {
    return (
      <Dialog title={title} onClose={onClose}>
        <div className="flex flex-col gap-3">
          <p role="status" className="text-rs-body text-rs-text">
            Thanks. Our team will review it.
          </p>
          <div className="flex flex-wrap justify-end gap-2 pt-1">
            {sent.offerBlock && (
              <button type="button" onClick={onBlock} className={rsButtonClass({ variant: "danger", size: "form" })}>
                Block {name}
              </button>
            )}
            <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
              Done
            </button>
          </div>
        </div>
      </Dialog>
    );
  }

  return (
    <Dialog title={title} dirty={reason !== "" || details !== ""} onClose={onClose}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-3.5">
        {error && <FormError look="rs">{error}</FormError>}
        <SelectField
          id="report-reason"
          label="Reason"
          look="rs"
          required
          error={reasonError}
          value={reason}
          onChange={(e) => setReason(e.target.value as ReportReason | "")}
        >
          <option value="">Choose a reason</option>
          {reasons.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </SelectField>
        <TextAreaField
          id="report-details"
          label="Details (optional)"
          look="rs"
          rows={4}
          counter={{ count: length, max: maxDetails }}
          value={details}
          onChange={(e) => setDetails(e.target.value)}
        />
        <div className="flex flex-wrap justify-end gap-2 pt-1">
          <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
            Cancel
          </button>
          <button type="submit" disabled={busy || length > maxDetails} className={rsButtonClass({ size: "form" })}>
            Send report
          </button>
        </div>
      </form>
    </Dialog>
  );
}
