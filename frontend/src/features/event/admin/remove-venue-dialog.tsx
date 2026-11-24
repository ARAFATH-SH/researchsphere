"use client";

import { type FormEvent, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { FormError, TextAreaField } from "@/components/ui/field";

import { removeVenue, type VenueSummary } from "../api";

export const minReason = 10;
export const maxReason = 1000;

type Props = {
  venue: Pick<VenueSummary, "id" | "name">;
  onClose: () => void;
  onRemoved: () => void;
  returnFocusId?: string;
};

// RemoveVenueDialog removes a venue for good (FR-15.1): users no longer see it and there is no
// restore. The reason, 10 to 1,000 characters, is kept in the audit trail; Remove stays off
// until it is long enough.
export function RemoveVenueDialog({ venue, onClose, onRemoved, returnFocusId }: Props) {
  const [reason, setReason] = useState("");
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const length = reason.trim().length;
  const valid = length >= minReason && length <= maxReason;

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!valid) return;
    setBusy(true);
    setFailure("");
    const res = await removeVenue(venue.id, reason.trim());
    setBusy(false);
    if (res.ok) onRemoved();
    else setFailure(res.code === "not_found" ? "This venue was already removed." : res.message);
  }

  return (
    <Dialog title="Remove venue" dirty={reason.trim() !== ""} onClose={onClose} returnFocusId={returnFocusId}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-4">
        <p className="text-rs-body text-rs-text">
          Remove <strong className="font-medium">{venue.name}</strong>? Nobody will see it again, and it cannot be restored.
        </p>
        <TextAreaField
          look="rs"
          id="venue-remove-reason"
          label="Reason"
          required
          rows={3}
          counter={{ count: reason.length, max: maxReason }}
          error={length > 0 && length < minReason ? `Give a reason of at least ${minReason} characters.` : undefined}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
        {failure && <FormError look="rs">{failure}</FormError>}
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
            Cancel
          </button>
          <button type="submit" disabled={!valid || busy} className={rsButtonClass({ variant: "destructive", size: "form" })}>
            {busy ? "Removing…" : "Remove venue"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
